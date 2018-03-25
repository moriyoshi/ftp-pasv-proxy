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
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLineReader(t *testing.T) {
	lr := LineReader{
		r:    bytes.NewReader([]byte("abc\ndef\nghi")),
		buf:  make([]byte, 0, 3),
		mark: -1,
	}
	{
		err := lr.Next()
		assert.Nil(t, err)
		assert.Equal(t, []byte("abc\n"), lr.Get())
		assert.Equal(t, 4, cap(lr.buf))
	}
	lr.Mark()
	{
		err := lr.Next()
		assert.Nil(t, err)
		assert.Equal(t, []byte("def\n"), lr.Get())
	}
	{
		err := lr.Next()
		assert.Nil(t, err)
		assert.Equal(t, []byte("ghi"), lr.Get())
	}
	{
		err := lr.Next()
		assert.Equal(t, io.EOF, err)
	}
	assert.Equal(t, []byte("def\nghi"), lr.GetFromMarker())
}

func TestLineReaderTelnetEscape(t *testing.T) {
	tescs := make([][]byte, 0, 5)
	lr := LineReader{
		r:    bytes.NewReader([]byte("abc\nd\xff\xffef\xff\xfe\x01\ngh\xffi\xff")),
		buf:  make([]byte, 0, 3),
		mark: -1,
		tesc: func(tesc []byte) error {
			tescs = append(tescs, tesc)
			return nil
		},
	}
	{
		err := lr.Next()
		assert.Nil(t, err)
		assert.Equal(t, []byte("abc\n"), lr.Get())
		assert.Equal(t, 4, cap(lr.buf))
	}
	lr.Mark()
	{
		err := lr.Next()
		assert.Nil(t, err)
		assert.Equal(t, []byte("def\n"), lr.Get())
		assert.Equal(t, 2, len(tescs))
		assert.Equal(t, []byte{0xff, 0xff}, tescs[0])
		assert.Equal(t, []byte{0xff, 0xfe, 0x01}, tescs[1])
	}
	{
		err := lr.Next()
		assert.Nil(t, err)
		assert.Equal(t, []byte("gh"), lr.Get())
		assert.Equal(t, 3, len(tescs))
		assert.Equal(t, []byte{0xff, 0xff}, tescs[0])
		assert.Equal(t, []byte{0xff, 0xfe, 0x01}, tescs[1])
		assert.Equal(t, []byte{0xff, 'i'}, tescs[2])
	}
	{
		err := lr.Next()
		assert.Equal(t, io.EOF, err)
	}
	assert.Equal(t, []byte("def\ngh"), lr.GetFromMarker())
}
