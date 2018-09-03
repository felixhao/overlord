package redis

import (
	"bytes"
	errs "errors"
	"overlord/proto"
	"strings"
	"sync"
)

var (
	emptyBytes = []byte("")
	crlfBytes  = []byte("\r\n")

	blankByte = byte(' ')

	arrayLenTwo   = []byte("2")
	arrayLenThree = []byte("3")

	cmdQuitBytes   = []byte("4\r\nQUIT")
	cmdPingBytes   = []byte("4\r\nPING")
	cmdMSetBytes   = []byte("4\r\nMSET")
	cmdMGetBytes   = []byte("4\r\nMGET")
	cmdGetBytes    = []byte("3\r\nGET")
	cmdDelBytes    = []byte("3\r\nDEL")
	cmdExistsBytes = []byte("6\r\nEXISTS")

	reqReadCmdsBytes = []byte("" +
		"4\r\nDUMP" +
		"6\r\nEXISTS" +
		"4\r\nPTTL" +
		"3\r\nTTL" +
		"4\r\nTYPE" +
		"8\r\nBITCOUNT" +
		"6\r\nBITPOS" +
		"3\r\nGET" +
		"6\r\nGETBIT" +
		"8\r\nGETRANGE" +
		"4\r\nMGET" +
		"6\r\nSTRLEN" +
		"7\r\nHEXISTS" +
		"4\r\nHGET" +
		"7\r\nHGETALL" +
		"5\r\nHKEYS" +
		"4\r\nHLEN" +
		"5\r\nHMGET" +
		"7\r\nHSTRLEN" +
		"5\r\nHVALS" +
		"5\r\nHSCAN" +
		"5\r\nSCARD" +
		"5\r\nSDIFF" +
		"6\r\nSINTER" +
		"9\r\nSISMEMBER" +
		"8\r\nSMEMBERS" +
		"11\r\nSRANDMEMBER" +
		"6\r\nSUNION" +
		"5\r\nSSCAN" +
		"5\r\nZCARD" +
		"6\r\nZCOUNT" +
		"9\r\nZLEXCOUNT" +
		"6\r\nZRANGE" +
		"11\r\nZRANGEBYLEX" +
		"13\r\nZRANGEBYSCORE" +
		"5\r\nZRANK" +
		"9\r\nZREVRANGE" +
		"14\r\nZREVRANGEBYLEX" +
		"16\r\nZREVRANGEBYSCORE" +
		"8\r\nZREVRANK" +
		"6\r\nZSCORE" +
		"5\r\nZSCAN" +
		"6\r\nLINDEX" +
		"4\r\nLLEN" +
		"6\r\nLRANGE" +
		"7\r\nPFCOUNT")

	reqWriteCmdsBytes = []byte("" +
		"3\r\nDEL" +
		"6\r\nEXPIRE" +
		"8\r\nEXPIREAT" +
		"7\r\nPERSIST" +
		"7\r\nPEXPIRE" +
		"9\r\nPEXPIREAT" +
		"7\r\nRESTORE" +
		"4\r\nSORT" +
		"6\r\nAPPEND" +
		"4\r\nDECR" +
		"6\r\nDECRBY" +
		"6\r\nGETSET" +
		"4\r\nINCR" +
		"6\r\nINCRBY" +
		"11\r\nINCRBYFLOAT" +
		"4\r\nMSET" +
		"6\r\nPSETEX" +
		"3\r\nSET" +
		"6\r\nSETBIT" +
		"5\r\nSETEX" +
		"5\r\nSETNX" +
		"8\r\nSETRANGE" +
		"4\r\nHDEL" +
		"7\r\nHINCRBY" +
		"12\r\nHINCRBYFLOAT" +
		"5\r\nHMSET" +
		"4\r\nHSET" +
		"6\r\nHSETNX" +
		"7\r\nLINSERT" +
		"4\r\nLPOP" +
		"5\r\nLPUSH" +
		"6\r\nLPUSHX" +
		"4\r\nLREM" +
		"4\r\nLSET" +
		"5\r\nLTRIM" +
		"4\r\nRPOP" +
		"9\r\nRPOPLPUSH" +
		"5\r\nRPUSH" +
		"6\r\nRPUSHX" +
		"4\r\nSADD" +
		"5\r\nSMOVE" +
		"4\r\nSPOP" +
		"4\r\nSREM" +
		"4\r\nZADD" +
		"7\r\nZINCRBY" +
		"11\r\nZINTERSTORE" +
		"4\r\nZREM" +
		"14\r\nZREMRANGEBYLEX" +
		"15\r\nZREMRANGEBYRANK" +
		"16\r\nZREMRANGEBYSCORE" +
		"5\r\nPFADD" +
		"7\r\nPFMERGE")

	reqNotSupportCmdsBytes = []byte("" +
		"6\r\nMSETNX" +
		"10\r\nSDIFFSTORE" +
		"11\r\nSINTERSTORE" +
		"11\r\nSUNIONSTORE" +
		"11\r\nZUNIONSTORE" +
		"5\r\nBLPOP" +
		"5\r\nBRPOP" +
		"10\r\nBRPOPLPUSH" +
		"4\r\nKEYS" +
		"7\r\nMIGRATE" +
		"4\r\nMOVE" +
		"6\r\nOBJECT" +
		"9\r\nRANDOMKEY" +
		"6\r\nRENAME" +
		"8\r\nRENAMENX" +
		"4\r\nSCAN" +
		"4\r\nWAIT" +
		"5\r\nBITOP" +
		"4\r\nEVAL" +
		"7\r\nEVALSHA" +
		"4\r\nAUTH" +
		"4\r\nECHO" +
		"4\r\nINFO" +
		"5\r\nPROXY" +
		"7\r\nSLOWLOG" +
		"6\r\nSELECT" +
		"4\r\nTIME" +
		"6\r\nCONFIG" +
		"8\r\nCOMMANDS")

	reqCtlCmdsBytes = []byte("4\r\nQUIT" + "4\r\nPING")
)

// errors
var (
	ErrBadAssert  = errs.New("bad assert for redis")
	ErrBadCount   = errs.New("bad count number")
	ErrBadRequest = errs.New("bad request")
)

// mergeType is used to decript the merge operation.
type mergeType = uint8

// merge types
const (
	mergeTypeNo mergeType = iota
	mergeTypeCount
	mergeTypeOK
	mergeTypeJoin
)

// Request is the type of a complete redis command
type Request struct {
	resp  *resp
	reply *resp
	mType mergeType
}

var reqPool = &sync.Pool{
	New: func() interface{} {
		return newReq()
	},
}

// getReq get the msg from pool
func getReq() *Request {
	return reqPool.Get().(*Request)
}

func newReq() *Request {
	r := &Request{}
	r.resp = &resp{}
	r.reply = &resp{}
	return r
}

// AsSlowlog will convert it self as slowlog string
func (r *Request) AsSlowlog() string {
	var sb strings.Builder
	_, _ = sb.Write(r.Cmd())
	for i := 1; i < r.resp.arrayn-1; i++ {
		_, _ = sb.Write(r.resp.array[i].data)
		_ = sb.WriteByte(blankByte)
	}
	_, _ = sb.Write(r.resp.array[r.resp.arrayn-1].data)
	return sb.String()
}

// Clone will copy the data and anything.
func (r *Request) Clone() proto.Request {
	nr := &Request{
		resp:  r.resp.Clone(),
		reply: nil,
		mType: r.mType,
	}
	return nr
}

// CmdString get the cmd
func (r *Request) CmdString() string {
	return string(r.Cmd())
}

// Cmd get the cmd
func (r *Request) Cmd() []byte {
	if r.resp.arrayn < 1 {
		return emptyBytes
	}
	cmd := r.resp.array[0]
	var pos int
	if cmd.rTp == respBulk {
		pos = bytes.Index(cmd.data, crlfBytes) + 2
	}
	return cmd.data[pos:]
}

// Key impl the proto.protoRequest and get the Key of redis
func (r *Request) Key() []byte {
	if r.resp.arrayn < 1 {
		return emptyBytes
	}
	if r.resp.arrayn == 1 {
		return r.resp.array[0].data
	}
	k := r.resp.array[1]
	var pos int
	if k.rTp == respBulk {
		pos = bytes.Index(k.data, crlfBytes) + 2
	}
	return k.data[pos:]
}

// Put the resource back to pool
func (r *Request) Put() {
	r.resp.reset()
	r.reply.reset()
	r.mType = mergeTypeNo
	reqPool.Put(r)
}

// RESP return request resp.
func (r *Request) RESP() *RESP {
	return r.resp
}

// Reply return request reply.
func (r *Request) Reply() *RESP {
	return r.reply
}

// IsSupport check command support.
func (r *Request) IsSupport() bool {
	if r.resp.arrayn < 1 {
		return false
	}

	return bytes.Contains(reqReadCmdsBytes, r.resp.array[0].data) ||
		bytes.Contains(reqWriteCmdsBytes, r.resp.array[0].data) ||
		bytes.Contains(reqCtlCmdsBytes, r.resp.array[0].data)
}

// IsCtl is control command.
func (r *Request) IsCtl() bool {
	if r.resp.arrayn < 1 {
		return false
	}

	return bytes.Contains(reqCtlCmdsBytes, r.resp.array[0].data)
}
