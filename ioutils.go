package main

import (
	"context"
	"fmt"
	"log"
	"net"
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

func cancelablReadInner(ctx context.Context, r ReaderWithDeadline, buf []byte, nowGetter func() time.Time) (n int, err error) {
	for {
		now := nowGetter()
		select {
		case <-ctx.Done():
			dl, ok := ctx.Deadline()
			if !ok || dl.IsZero() || dl.Sub(now) > time.Duration(0) {
				err = canceled
				return
			} else {
				err = &net.OpError{Op: "read", Err: timeoutError}
			}
			return
		default:
		}
		n, err = readWithDeadline(r, buf, now.Add(tick))
		if _err, ok := err.(interface{ Timeout() bool }); ok && _err.Timeout() {
			continue
		}
		break
	}
	return
}

func cancelableRead(ctx NowgettableContext, r ReaderWithDeadline, buf []byte) (int, error) {
	return cancelablReadInner(ctx, r, buf, ctx.Now)
}

func cancelableReadWithDeadline(ctx NowgettableContext, r ReaderWithDeadline, buf []byte, dl time.Time) (int, error) {
	_ctx, cancel := context.WithDeadline(ctx, dl)
	defer cancel()
	return cancelablReadInner(_ctx, r, buf, ctx.Now)
}

func cancelableWriteWithDeadline(ctx NowgettableContext, w WriterWithDeadline, buf []byte, dl time.Time) (n int, err error) {
	_ctx, cancel := context.WithDeadline(ctx, dl)
	defer cancel()
	for {
		select {
		case <-_ctx.Done():
			dl, ok := _ctx.Deadline()
			if !ok || dl.IsZero() || dl.Sub(ctx.Now()) > time.Duration(0) {
				err = canceled
				return
			} else {
				err = &net.OpError{Op: "read", Err: timeoutError}
			}
			return
		default:
		}
		n, err = writeWithDeadline(w, buf, ctx.Now().Add(tick))
		if _err, ok := err.(interface{ Timeout() bool }); ok && _err.Timeout() {
			continue
		}
		break
	}
	return
}
