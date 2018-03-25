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
	"fmt"
	"net"
)

type IPAddrPortPair struct {
	Addr net.IPAddr
	Port int
}

func (pair *IPAddrPortPair) Valid() bool {
	return len(pair.Addr.IP) != 0
}

func (pair *IPAddrPortPair) String() string {
	if pair.Addr.IP.To4() == nil {
		return fmt.Sprintf("[%s]:%d", pair.Addr.String(), pair.Port)
	} else {
		return fmt.Sprintf("%s:%d", pair.Addr.String(), pair.Port)
	}
}
