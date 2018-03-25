package main

import (
	"io"
	"log"
	"net"
	"sync"
	"time"
)

type FTPReaderContext struct {
	parent   NowgettableContext
	r        *net.TCPConn
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

func NewFTPReaderContext(parent NowgettableContext, r *net.TCPConn) *FTPReaderContext {
	oobChan := make(chan []byte)
	lr := LineReader{
		r:      r,
		buf:    make([]byte, 0, 1024),
		offset: 0,
		mark:   0,
		tesc: func(b []byte) error {
			oobChan <- b
			return nil
		},
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
		defer func() {
			log.Printf("FTPReaderContext (%p) is closing...", rctx)
			close(rctx.doneChan)
		}()
		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			availChan := make(chan error)
		outer:
			for rctx.running {
				go func() {
					err := r.SetReadDeadline(parent.Now().Add(readTimeout))
					if err != nil {
						availChan <- err
						return
					}
					availChan <- lr.Next()
				}()
				select {
				case <-parent.Done():
					r.SetReadDeadline(time.Unix(1, 0))
					break outer
				case _err := <-availChan:
					if _err == io.EOF {
						break outer
					} else if _err != nil {
						rctx.err = _err
						break outer
					}
				}
				select {
				case <-parent.Done():
					break outer
				case rctx.dataChan <- &lr:
				}
			}
		}()
		wg.Wait()
	}()
	return rctx
}
