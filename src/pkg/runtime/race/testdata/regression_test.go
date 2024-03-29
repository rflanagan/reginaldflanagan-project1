// Copyright 2012 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Code patterns that caused problems in the past.

package race_test

import (
	"testing"
)

type LogImpl struct {
	x int
}

func NewLog() (l LogImpl) {
	go func() {
		_ = l
	}()
	l = LogImpl{}
	return
}

var _ LogImpl = NewLog()

func MakeMap() map[int]int {
	return make(map[int]int)
}

func InstrumentMapLen() {
	_ = len(MakeMap())
}

func InstrumentMapLen2() {
	m := make(map[int]map[int]int)
	_ = len(m[0])
}

func InstrumentMapLen3() {
	m := make(map[int]*map[int]int)
	_ = len(*m[0])
}

type Rect struct {
	x, y int
}

type Image struct {
	min, max Rect
}

func NewImage() Image {
	var pleaseDoNotInlineMe stack
	pleaseDoNotInlineMe.push(1)
	_ = pleaseDoNotInlineMe.pop()
	return Image{}
}

func AddrOfTemp() {
	_ = NewImage().min
}

type TypeID int

func (t *TypeID) encodeType(x int) (tt TypeID, err error) {
	switch x {
	case 0:
		return t.encodeType(x * x)
	}
	return 0, nil
}

type stack []int

func (s *stack) push(x int) {
	*s = append(*s, x)
}

func (s *stack) pop() int {
	i := len(*s)
	n := (*s)[i-1]
	*s = (*s)[:i-1]
	return n
}

func TestNoRaceStackPushPop(t *testing.T) {
	var s stack
	go func(s *stack) {}(&s)
	s.push(1)
	x := s.pop()
	_ = x
}

type RpcChan struct {
	c chan bool
}

var makeChanCalls int

func makeChan() *RpcChan {
	var pleaseDoNotInlineMe stack
	pleaseDoNotInlineMe.push(1)
	_ = pleaseDoNotInlineMe.pop()

	makeChanCalls++
	c := &RpcChan{make(chan bool, 1)}
	c.c <- true
	return c
}

func call() bool {
	x := <-makeChan().c
	return x
}

func TestNoRaceRpcChan(t *testing.T) {
	makeChanCalls = 0
	_ = call()
	if makeChanCalls != 1 {
		t.Fatalf("makeChanCalls %d, expected 1\n", makeChanCalls)
	}
}
