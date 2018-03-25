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
	"time"
)

type NowgettableContext interface {
	context.Context
	Now() time.Time
}

type DefaultNowgettableContext struct {
	context.Context
	now func() time.Time
}

func (ctx *DefaultNowgettableContext) Now() time.Time {
	return ctx.now()
}

func WithCancel(parent NowgettableContext) (NowgettableContext, context.CancelFunc) {
	cctx, cancelFunc := context.WithCancel(parent)
	return &DefaultNowgettableContext{cctx, parent.Now}, cancelFunc
}

func WithTimeout(parent NowgettableContext, timeout time.Duration) (NowgettableContext, context.CancelFunc) {
	cctx, cancelFunc := context.WithDeadline(parent, parent.Now().Add(timeout))
	return &DefaultNowgettableContext{cctx, parent.Now}, cancelFunc
}

func Background() NowgettableContext {
	return &DefaultNowgettableContext{context.Background(), time.Now}
}
