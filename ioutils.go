//
// Copyright 2018 Moriyoshi Koizumi
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.
//
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"
)

type ReaderWithDeadline interface {
	io.Reader
	SetReadDeadline(t time.Time) error
}

type WriterWithDeadline interface {
	io.Writer
	SetWriteDeadline(t time.Time) error
}

const maxTemporaryFailure = 5

var timeoutError = &TimeoutError{}
var canceled = fmt.Errorf("canceled I/O operation")

type TimeoutError struct{}

func (*TimeoutError) Error() string {
	return "context timeout"
}

func (*TimeoutError) Timeout() bool {
	return true
}

func isTemporary(err error) bool {
	_err, ok := err.(interface{ Temporary() bool })
	return ok && _err.Temporary()
}

func isTimeout(err error) bool {
	_err, ok := err.(interface{ Timeout() bool })
	return ok && _err.Timeout()
}

func writeWithDeadline(w WriterWithDeadline, buf []byte, dl time.Time) (int, error) {
	err := w.SetWriteDeadline(dl)
	if err != nil {
		return 0, err
	}

	tfail := 0
	tWait := 100000000
	o := 0
	for o < len(buf) {
		var n int
		if o > 0 {
			n, err = w.Write(buf[o:])
		} else {
			n, err = w.Write(buf)
		}
		o += n
		if err != nil {
			if isTemporary(err) && !isTimeout(err) {
				tfail++
				if tfail < maxTemporaryFailure {
					time.Sleep(time.Duration(tWait))
					tWait *= 2
					continue
				} else {
					err = fmt.Errorf("too many temporary failures; assuming it to be a permanent failure")
					break
				}
			}
			break
		}
	}
	return o, err
}

func readWithDeadline(r ReaderWithDeadline, buf []byte, dl time.Time) (n int, err error) {
	err = r.SetReadDeadline(dl)
	if err != nil {
		return
	}
	tFail := 0
	tWait := 100000000
	for {
		n, err = r.Read(buf)
		if err != nil {
			if isTemporary(err) && !isTimeout(err) {
				log.Println(err.Error())
				tFail++
				if tFail < maxTemporaryFailure {
					time.Sleep(time.Duration(tWait))
					tWait *= 2
					continue
				} else {
					err = fmt.Errorf("too many temporary failures; assuming it to be a permanent failure")
				}
			}
		}
		break
	}
	return
}

func cancelablReadInner(ctx context.Context, r ReaderWithDeadline, buf []byte) (n int, err error) {
	type readResult struct {
		n   int
		err error
	}
	resultAvailableChan := make(chan readResult)
	go func() {
		n, err := r.Read(buf)
		resultAvailableChan <- readResult{n, err}
	}()
	select {
	case <-ctx.Done():
		r.SetReadDeadline(time.Unix(1, 0))
		err = canceled
		break
	case result := <-resultAvailableChan:
		n = result.n
		err = result.err
	}
	return
}

func cancelableRead(ctx NowgettableContext, r ReaderWithDeadline, buf []byte) (int, error) {
	return cancelablReadInner(ctx, r, buf)
}

func cancelableReadWithDeadline(ctx NowgettableContext, r ReaderWithDeadline, buf []byte, dl time.Time) (int, error) {
	r.SetReadDeadline(dl)
	return cancelablReadInner(ctx, r, buf)
}

func cancelableWriteWithDeadline(ctx NowgettableContext, w WriterWithDeadline, buf []byte, dl time.Time) (n int, err error) {
	type writeResult struct {
		n   int
		err error
	}
	w.SetWriteDeadline(dl)
	resultAvailableChan := make(chan writeResult)
	go func() {
		n, err := w.Write(buf)
		resultAvailableChan <- writeResult{n, err}
	}()
	select {
	case <-ctx.Done():
		w.SetWriteDeadline(time.Unix(1, 0))
		err = canceled
		break
	case result := <-resultAvailableChan:
		n = result.n
		err = result.err
	}
	return
}
