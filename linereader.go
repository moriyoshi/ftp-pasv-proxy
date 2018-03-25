package main

import (
	"io"
)

const (
	tescStateNone         = 0
	tescStateExpectSecond = 1
	tescStateExpectThird  = 2
)

type LineReader struct {
	r          io.Reader
	ctx        NowgettableContext
	buf        []byte
	offset     int
	scanOffset int
	eof        bool
	mark       int
	prevOffset int
	tesc       func([]byte) error
}

func (lr *LineReader) Next() (err error) {
	if lr.eof && lr.offset == len(lr.buf) {
		return io.EOF
	}
	for {
		i := lr.offset
		lineAvail := false
		tescBufState := tescStateNone
		tescOffset := -1
	inner:
		for i < len(lr.buf) {
			switch tescBufState {
			case tescStateNone:
				for i < len(lr.buf) {
					c := lr.buf[i]
					i++
					if c == 255 {
						// telnet escape
						tescOffset = i - 1
						tescBufState = tescStateExpectSecond
						break
					} else if c == '\n' {
						lineAvail = true
						break inner
					}
				}
			case tescStateExpectSecond:
				if i < len(lr.buf) {
					c := lr.buf[i]
					i++
					switch c {
					case 251, 252, 253, 254:
						tescBufState = tescStateExpectThird
					default:
						tescBuf := make([]byte, i-tescOffset)
						copy(tescBuf, lr.buf[tescOffset:i])
						copy(lr.buf[tescOffset:], lr.buf[i:])
						lr.buf = lr.buf[:len(lr.buf)-(i-tescOffset)]
						i = tescOffset
						tescBufState = tescStateNone
						err = lr.tesc(tescBuf)
						if err != nil {
							return
						}
					}
				}
			case tescStateExpectThird:
				if i < len(lr.buf) {
					i++
					tescBuf := make([]byte, i-tescOffset)
					copy(tescBuf, lr.buf[tescOffset:i])
					copy(lr.buf[tescOffset:], lr.buf[i:])
					lr.buf = lr.buf[:len(lr.buf)-(i-tescOffset)]
					i = tescOffset
					tescBufState = tescStateNone
					err = lr.tesc(tescBuf)
					if err != nil {
						return
					}
				}
			}
		}
		if tescBufState == tescStateNone {
			if lineAvail {
				lr.prevOffset = lr.offset
				lr.offset = i
				return
			} else if lr.eof {
				lr.prevOffset = lr.offset
				lr.offset = len(lr.buf)
				return
			}
		} else {
			if lr.eof {
				// simply discard the incomplete escape sequences
				lr.buf = lr.buf[:tescOffset]
				lr.prevOffset = lr.offset
				lr.offset = tescOffset
				tescBufState = tescStateNone
				return
			}
		}
		o := len(lr.buf)
		if o == cap(lr.buf) {
			var cp int
			if lr.mark >= 0 {
				cp = lr.mark
			} else {
				cp = lr.offset
			}
			newLen := o - cp
			newCap := cap(lr.buf)
			if newLen > newCap/2 {
				newCap += cap(lr.buf) / 2
			}
			newBuf := make([]byte, newLen, newCap)
			copy(newBuf, lr.buf[cp:])
			lr.buf = newBuf
			lr.offset -= cp
			lr.prevOffset -= cp
			lr.mark -= cp
		}
		n, _err := lr.r.Read(lr.buf[len(lr.buf):cap(lr.buf)])
		if _err == io.EOF {
			lr.eof = true
		} else if _err != nil {
			err = _err
			return
		}
		lr.buf = lr.buf[:len(lr.buf)+n]
	}
}

func (lr *LineReader) Mark() {
	lr.mark = lr.offset
}

func (lr *LineReader) Unmark() {
	lr.mark = -1
}

func (lr *LineReader) Get() []byte {
	return lr.buf[lr.prevOffset:lr.offset]
}

func (lr *LineReader) GetFromMarker() []byte {
	return lr.buf[lr.mark:lr.offset]
}
