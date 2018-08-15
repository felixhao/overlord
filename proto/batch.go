package proto

import (
	"sync"
	"time"

	"overlord/lib/bufio"
	"overlord/lib/log"
	"overlord/lib/prom"

	"sync/atomic"

	"github.com/pkg/errors"
)

const (
	defaultRespBufSize  = 1024
	defaultMsgBatchSize = 2
)

const maxRetryTimes = 8

const (
	msgBatchStateNotReady = uint32(0)
	msgBatchStateDone     = uint32(1)
)

// errors
var (
	ErrTimeout         = errors.New("timeout reached")
	ErrMaxRetryReached = errors.New("too many times weak up")
)

var msgBatchPool = &sync.Pool{
	New: func() interface{} {
		return &MsgBatch{
			msgs:  make([]*Message, defaultMsgBatchSize),
			buf:   bufio.Get(defaultRespBufSize),
			state: msgBatchStateNotReady,
		}
	},
}

// NewMsgBatchSlice returns new slice of msgs
func NewMsgBatchSlice(n int) []*MsgBatch {
	m := make([]*MsgBatch, n)
	for i := 0; i < n; i++ {
		m[i] = NewMsgBatch()
	}
	return m
}

// NewMsgBatch will get msg from pool
func NewMsgBatch() *MsgBatch {
	return msgBatchPool.Get().(*MsgBatch)
}

// MsgBatch is a single execute unit
type MsgBatch struct {
	buf   *bufio.Buffer
	msgs  []*Message
	count int

	dc    chan struct{}
	state uint32

	parent *MsgBatch
}

// Fork for the MsgBatch's done channel.
func (m *MsgBatch) Fork() *MsgBatch {
	mb := NewMsgBatch()
	mb.dc = m.dc
	// 溯源回到 root 并设置
	var p = m
	for {
		if p.parent == nil {
			mb.parent = p
			return mb
		}
		p = p.parent
	}

}

// AddMsg will add new message reference to the buffer
func (m *MsgBatch) AddMsg(msg *Message) {
	if len(m.msgs) <= m.count {
		m.msgs = append(m.msgs, msg)
	} else {
		m.msgs[m.count] = msg
	}
	m.count++
}

// Count returns the count of the batch size
func (m *MsgBatch) Count() int {
	return m.count
}

// Nth will get the given positon, if not , nil will be return
func (m *MsgBatch) Nth(i int) *Message {
	if i < m.count {
		return m.msgs[i]
	}
	return nil
}

// Buffer will send back buffer to executor
func (m *MsgBatch) Buffer() *bufio.Buffer {
	return m.buf
}

// Done will set the total batch to done and notify the handler to check it.
func (m *MsgBatch) Done() {
	// 溯源回到 root 节点设置 done
	var p = m
	for {
		if p.parent == nil {
			atomic.StoreUint32(&p.state, msgBatchStateDone)
			break
		}
		p = p.parent
	}

	select {
	case m.dc <- struct{}{}:
	default:
	}
}

// Reset will reset all the field as initial value but msgs
func (m *MsgBatch) Reset() {
	// never reset sub mb. let them fuck
	// atomic.StoreUint32(&m.state, msgBatchStateNotReady)
	atomic.StoreUint32(&m.state, msgBatchStateNotReady)
	m.count = 0
	m.buf.Reset()
	m.parent = nil
}

// Msgs returns a slice of Msg
func (m *MsgBatch) Msgs() []*Message {
	return m.msgs[:m.count]
}

// IsDone check if MsgBatch is done.
func (m *MsgBatch) IsDone() bool {
	return atomic.LoadUint32(&m.state) == msgBatchStateDone
}

// BatchDone will set done and report prom HandleTime.
func (m *MsgBatch) BatchDone(cluster, addr string) {
	if prom.On {
		for _, msg := range m.Msgs() {
			prom.HandleTime(cluster, addr, msg.Request().CmdString(), int64(msg.RemoteDur()/time.Microsecond))
		}
	}
	m.Done()
}

// BatchDoneWithError will set done with error and report prom ErrIncr.
func (m *MsgBatch) BatchDoneWithError(cluster, addr string, err error) {
	for _, msg := range m.Msgs() {
		msg.SetError(err)
		if log.V(1) {
			log.Errorf("cluster(%s) Msg(%s) cluster process handle error:%+v", cluster, msg.Request().Key(), err)
		}
		if prom.On {
			prom.ErrIncr(cluster, addr, msg.Request().CmdString(), errors.Cause(err).Error())
		}
	}
	m.Done()
}

// MsgBatchAllocator will manage and allocate the msg batches
type MsgBatchAllocator struct {
	mbMap   map[string]*MsgBatch
	dc      chan struct{}
	timeout time.Duration
	// TODO: impl quick search for iterator
	quickSearch map[string]struct{}
}

// NewMsgBatchAllocator create mb batch from servers and dc
func NewMsgBatchAllocator(dc chan struct{}, timeout time.Duration) *MsgBatchAllocator {
	mba := &MsgBatchAllocator{
		mbMap: make(map[string]*MsgBatch),
		dc:    dc, quickSearch: make(map[string]struct{}),
		timeout: timeout,
	}
	return mba
}

// AddMsg will add new msg and create a new batch if node not exists.
func (m *MsgBatchAllocator) AddMsg(node string, msg *Message) {
	if mb, ok := m.mbMap[node]; ok {
		mb.AddMsg(msg)
	} else {
		mb := NewMsgBatch()
		mb.AddMsg(msg)
		mb.dc = m.dc
		m.mbMap[node] = mb
	}
}

// MsgBatchs will return the self mbMap for iterator
func (m *MsgBatchAllocator) MsgBatchs() map[string]*MsgBatch {
	return m.mbMap
}

// Wait until timeout reached or all msgbatch is done.
// if timeout, ErrTimeout will be return.
func (m *MsgBatchAllocator) Wait() error {
	mbLen := len(m.mbMap)
	to := time.After(m.timeout)
	if mbLen == 0 {
		select {
		case <-m.dc:
			if m.checkAllDone() {
				return nil
			}
			return ErrMaxRetryReached
		case <-to:
			return ErrTimeout
		}
	}

	for i := 0; i < mbLen+maxRetryTimes; i++ {
		select {
		case <-m.dc:
			if m.checkAllDone() {
				return nil
			}
		case <-to:
			return ErrTimeout
		}
	}
	return ErrMaxRetryReached
}

// Reset inner MsgBatchs
func (m *MsgBatchAllocator) Reset() {
	for _, mb := range m.MsgBatchs() {
		mb.Reset()
	}
}

// Put all the resource back into pool
func (m *MsgBatchAllocator) Put() {
	m.Reset()
	for _, mb := range m.MsgBatchs() {
		msgBatchPool.Put(mb)
	}
}

// Done will knock the done channel for errors command.
func (m *MsgBatchAllocator) Done() {
	select {
	case m.dc <- struct{}{}:
	default:
	}
}

func (m *MsgBatchAllocator) checkAllDone() bool {
	for _, mb := range m.MsgBatchs() {
		if mb.Count() > 0 {
			if !mb.IsDone() {
				return false
			}
		}
	}
	return true
}
