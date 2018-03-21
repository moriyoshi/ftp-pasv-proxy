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
