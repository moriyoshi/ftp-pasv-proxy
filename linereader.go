package main

import (
	"bytes"
	"io"
)

type LineReader struct {
	r          io.Reader
	buf        []byte
	offset     int
	eof        bool
	mark       int
	prevOffset int
}

func (lr *LineReader) Next() (err error) {
	if lr.eof && lr.offset == len(lr.buf) {
		return io.EOF
	}
	for {
		if i := bytes.IndexByte(lr.buf[lr.offset:], '\n'); i >= 0 {
			i += lr.offset
			lr.prevOffset = lr.offset
			lr.offset = i + 1
			return
		} else if lr.eof {
			lr.prevOffset = lr.offset
			lr.offset = len(lr.buf)
			return
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
