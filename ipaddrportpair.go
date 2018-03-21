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
