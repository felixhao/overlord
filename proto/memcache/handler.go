package memcache

import (
	"bytes"
	"net"
	"sync/atomic"
	"time"

	"github.com/felixhao/overlord/lib/bufio"
	"github.com/felixhao/overlord/lib/conv"
	"github.com/felixhao/overlord/lib/pool"
	"github.com/felixhao/overlord/lib/stat"
	"github.com/felixhao/overlord/proto"
	"github.com/pkg/errors"
)

const (
	handlerOpening = int32(0)
	handlerClosed  = int32(1)

	handlerWriteBufferSize = 8 * 1024   // NOTE: write command, so relatively small
	handlerReadBufferSize  = 128 * 1024 // NOTE: read data, so relatively large
)

type handler struct {
	cluster string
	addr    string
	conn    net.Conn
	br      *bufio.Reader
	bw      *bufio.Writer
	bss     [][]byte
	buf     []byte

	readTimeout  time.Duration
	writeTimeout time.Duration

	closed int32
}

// Dial returns pool Dial func.
func Dial(cluster, addr string, dialTimeout, readTimeout, writeTimeout time.Duration) (dial func() (pool.Conn, error)) {
	dial = func() (pool.Conn, error) {
		conn, err := net.DialTimeout("tcp", addr, dialTimeout)
		if err != nil {
			return nil, err
		}
		h := &handler{
			cluster:      cluster,
			addr:         addr,
			conn:         conn,
			bw:           bufio.NewWriterSize(conn, handlerWriteBufferSize),
			br:           bufio.NewReaderSize(conn, handlerReadBufferSize),
			bss:          make([][]byte, 2), // NOTE: like: 'VALUE a_11 0 0 3\r\naaa\r\nEND\r\n', and not copy 'END\r\n'
			readTimeout:  readTimeout,
			writeTimeout: writeTimeout,
		}
		return h, nil
	}
	return
}

// Handle call server node by request and read response returned.
func (h *handler) Handle(reqs *proto.Request) (resps []*proto.Response, err error) {
	if h.Closed() {
		err = errors.Wrap(ErrClosed, "MC Handler handle request")
		return
	}
	var mcrs []*MCRequest
	var i int
	for req := reqs; req != nil; req = req.Next {
		i++
		mcr, ok := req.Proto().(*MCRequest)
		if !ok {
			err = errors.Wrap(ErrAssertRequest, "MC Handler handle assert MCRequest")
			return
		}
		mcrs = append(mcrs, mcr)
		if h.writeTimeout > 0 {
			h.conn.SetWriteDeadline(time.Now().Add(h.writeTimeout))
		}
		h.bw.WriteString(mcr.rTp.String())
		h.bw.WriteByte(spaceByte)
		if mcr.rTp == RequestTypeGat || mcr.rTp == RequestTypeGats {
			h.bw.Write(mcr.data) // NOTE: exptime
			h.bw.WriteByte(spaceByte)
			h.bw.Write(mcr.key)
			h.bw.Write(crlfBytes)
		} else {
			h.bw.Write(mcr.key)
			h.bw.Write(mcr.data)
		}
	}
	if err = h.bw.Flush(); err != nil {
		err = errors.Wrap(err, "MC Handler handle flush request bytes")
		return
	}
	if h.readTimeout > 0 {
		h.conn.SetReadDeadline(time.Now().Add(h.readTimeout))
	}
	for i := 0; i < len(mcrs); i++ {
		mcr := mcrs[i]
		var bs []byte
		bs, err = h.br.ReadBytes(delim)
		if err != nil {
			err = errors.Wrap(err, "MC Handler handle read response bytes")
			return
		}
		if mcr.rTp == RequestTypeGet || mcr.rTp == RequestTypeGets || mcr.rTp == RequestTypeGat || mcr.rTp == RequestTypeGats {
			if !bytes.Equal(bs, endBytes) {
				stat.Hit(h.cluster, h.addr)
				bss := bytes.Split(bs, spaceBytes)
				if len(bss) < 4 {
					err = errors.Wrap(ErrBadResponse, "MC Handler handle read response bytes split")
					return
				}
				var length int64
				if len(bss) == 4 { // NOTE: if len==4, means gets|gats
					if len(bss[3]) < 2 {
						err = errors.Wrap(ErrBadResponse, "MC Handler handle read response bytes check")
						return
					}
					bss[3] = bss[3][:len(bss[3])-2] // NOTE: gets|gats contains '\r\n'
				}
				if length, err = conv.Btoi(bss[3]); err != nil {
					err = errors.Wrap(ErrBadResponse, "MC Handler handle read response bytes length")
					return
				}
				var bs2 []byte
				if bs2, err = h.br.ReadFull(int(length + 2)); err != nil { // NOTE: +2 read contains '\r\n'
					err = errors.Wrap(ErrBadResponse, "MC Handler handle read response bytes read")
					return
				}
				h.bss = h.bss[:2]
				h.bss[0] = bs
				h.bss[1] = bs2
				tl := len(bs) + len(bs2)
				var bs3 []byte
				for !bytes.Equal(bs3, endBytes) {
					if bs3 != nil { // NOTE: here, avoid copy 'END\r\n'
						h.bss = append(h.bss, bs3)
						tl += len(bs3)
					}
					if h.readTimeout > 0 {
						h.conn.SetReadDeadline(time.Now().Add(h.readTimeout))
					}
					if bs3, err = h.br.ReadBytes(delim); err != nil {
						err = errors.Wrap(err, "MC Handler handle reread response bytes")
						return
					}
				}
				const endBytesLen = 5 // NOTE: endBytes length
				tmp := h.makeBytes(tl + endBytesLen)
				off := 0
				for i := range h.bss {
					copy(tmp[off:], h.bss[i])
					off += len(h.bss[i])
				}
				copy(tmp[off:], endBytes)
				bs = tmp
			} else {
				stat.Miss(h.cluster, h.addr)
			}
		}
		resp := &proto.Response{Type: proto.CacheTypeMemcache}
		pr := &MCResponse{rTp: mcr.rTp, data: bs}
		resp.WithProto(pr)
		resps = append(resps, resp)
	}
	//fmt.Println(i, len(mcrs), len(resps))
	return
}

func (h *handler) Close() error {
	if atomic.CompareAndSwapInt32(&h.closed, handlerOpening, handlerClosed) {
		return h.conn.Close()
	}
	return nil
}

func (h *handler) Closed() bool {
	return atomic.LoadInt32(&h.closed) == handlerClosed
}

func (h *handler) makeBytes(n int) (ss []byte) {
	switch {
	case n == 0:
		return []byte{}
	case n >= handlerWriteBufferSize:
		return make([]byte, n)
	default:
		if len(h.buf) < n {
			h.buf = make([]byte, handlerReadBufferSize)
		}
		ss, h.buf = h.buf[:n:n], h.buf[n:]
		return ss
	}
}
