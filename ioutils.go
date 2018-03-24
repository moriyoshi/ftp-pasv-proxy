package main

import (
	"context"
	"fmt"
	"log"
	"time"
)

const maxTemporaryFailure = 5
const tick = 100000000

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
