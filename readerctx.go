package main

import (
	"io"
	"net"
	"time"
)

type FTPReaderContext struct {
	parent   NowgettableContext
	r        ReaderWithDeadline
	lr       LineReader
	doneChan chan struct{}
	dataChan chan *LineReader
	err      error
	running  bool
}

func (rctx *FTPReaderContext) Done() <-chan struct{} {
	return rctx.doneChan
}

func (rctx *FTPReaderContext) Data() <-chan *LineReader {
	return rctx.dataChan
}

func (rctx *FTPReaderContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (rctx *FTPReaderContext) Value(interface{}) interface{} {
	return nil
}

func (rctx *FTPReaderContext) Cancel() {
	rctx.running = false
}

func NewFTPReaderContext(parent NowgettableContext, r ReaderWithDeadline) *FTPReaderContext {
	lr := LineReader{
		r:      r,
		buf:    make([]byte, 0, 1024),
		offset: 0,
		mark:   0,
	}
	rctx := &FTPReaderContext{
		parent:   parent,
		r:        r,
		lr:       lr,
		doneChan: make(chan struct{}),
		dataChan: make(chan *LineReader),
		err:      nil,
		running:  true,
	}
	go func() {
	outer:
		for rctx.running {
			select {
			case <-parent.Done():
				break outer
			default:
			}
			_err := r.SetReadDeadline(parent.Now().Add(readTimeout))
			if _err != nil {
				rctx.err = _err
				break
			}
			_err = lr.Next()
			if _err == nil {
				select {
				case <-parent.Done():
					break outer
				case rctx.dataChan <- &lr:
					break
				}
			} else if _err == io.EOF {
				break
			} else {
				if err, ok := (_err).(*net.OpError); !ok || (!err.Timeout() && !err.Temporary()) {
					rctx.err = _err
					break
				}
			}
		}
		close(rctx.doneChan)
	}()
	return rctx
}
