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
	assert.Equal(t, lr.GetFromMarker(), []byte("def\nghi"))
}
