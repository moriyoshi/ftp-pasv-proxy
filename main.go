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
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/pkg/errors"
)

type ReaderWithDeadline interface {
	io.Reader
	SetReadDeadline(t time.Time) error
}

type WriterWithDeadline interface {
	io.Writer
	SetWriteDeadline(t time.Time) error
}

type NowGetter func() time.Time

type FTPSession struct {
	srcConn              *net.TCPConn
	destConn             *net.TCPConn
	destAddr             IPAddrPortPair
	srcContext           *FTPReaderContext
	destContext          *FTPReaderContext
	remoteDataConn       net.Conn
	localDataSubmAddr    IPAddrPortPair
	srcHandler           func() error
	err                  error
	doneChan             chan struct{}
	finishChan           chan struct{}
	nowGetter            NowGetter
	dialer               net.Dialer
	outboundCommandSpool [][]byte
	canceled             bool
}

type FTPReply struct {
	Code    int
	Message []byte
	Image   []byte
}

type ConnGenFunc func(ctx context.Context) (*net.TCPConn, error)

var portPasvOctetsRegex = regexp.MustCompile("\\s*([0-9]+),([0-9]+),([0-9]+),([0-9]+),([0-9]+),([0-9]+)\\s*")
var statusRespRegex = regexp.MustCompile("(?s)^([0-9]{3})(\\s|-)\\s*(.+)\\z")
var crlf = []byte{13, 10}
var emptyBytes = []byte{}
var pasvReqLine = []byte{'P', 'A', 'S', 'V', 13, 10}
var epsvReqLine = []byte{'E', 'P', 'S', 'V', 13, 10}
var abrtReqLine = []byte{'A', 'B', 'O', 'R', 13, 10}
var writeTimeout = time.Duration(1000000000 * 60)
var readTimeout = time.Duration(1000000000 * 60)

func (fc *FTPSession) sendReply(rep FTPReply) error {
	buf := make([]byte, 4, 4+len(rep.Message)+2)
	buf[0] = "0123456789"[(rep.Code/100)%10]
	buf[1] = "0123456789"[(rep.Code/10)%10]
	buf[2] = "0123456789"[rep.Code%10]
	buf[3] = ' '
	buf = append(buf, rep.Message...)
	buf = append(buf, crlf...)
	_, err := writeWithDeadline(fc.srcConn, buf, fc.Now().Add(writeTimeout))
	return err
}

func parsePortPasvOctets(l []byte) (pair IPAddrPortPair, err error) {
	matches := portPasvOctetsRegex.FindAllSubmatch(l, 1)
	if matches == nil {
		err = fmt.Errorf("invalid PORT/PASV octets: %s", l)
		return
	}

	host := make([]byte, 0, len(l))
	host = append(host, matches[0][1]...)
	host = append(host, '.')
	host = append(host, matches[0][2]...)
	host = append(host, '.')
	host = append(host, matches[0][3]...)
	host = append(host, '.')
	host = append(host, matches[0][4]...)
	addr, err := net.ResolveIPAddr("ip4", string(host))
	if err != nil {
		err = errors.Wrap(err, fmt.Sprintf("invalid PORT/PASV octets: %s", l))
		return
	}

	portHi, err := strconv.Atoi(string(matches[0][5]))
	if err != nil {
		err = errors.Wrap(err, fmt.Sprintf("invalid PORT/PASV octets: %s", l))
		return
	}
	portLo, err := strconv.Atoi(string(matches[0][6]))
	if err != nil {
		err = errors.Wrap(err, fmt.Sprintf("invalid PORT/PASV octets: %s", l))
		return
	}
	pair = IPAddrPortPair{
		Addr: *addr,
		Port: portHi*256 + portLo,
	}
	return
}

func parsePort(l []byte) (pair IPAddrPortPair, ok bool) {
	if len(l) < 5 {
		return
	}
	if l[0] != byte('P') || l[1] != byte('O') || l[2] != byte('R') || l[3] != byte('T') || (l[4] != 0x20 && l[4] != 0x09) {
		return
	}
	pair, err := parsePortPasvOctets(l[5:])
	ok = err == nil
	return
}

func afIntToString(af int) string {
	switch af {
	case 1:
		return "ip4"
	case 2:
		return "ip6"
	default:
		return ""
	}
}

func parseEprtEpsvHostPort(l []byte, allowEmptyAfAddr bool) (pair IPAddrPortPair, err error) {
	i := 0
	for {
		if i >= len(l) {
			err = fmt.Errorf("invalid EPRT/EPSV address: %s", string(l))
			return
		}
		if l[i] != 0x20 && l[i] != 0x09 {
			break
		}
		i++
	}
	d := l[i]
	if d >= 0x7f {
		err = fmt.Errorf("invalid EPRT/EPSV address: %s", string(l))
		return
	}
	i++
	var af int
	var netAddr net.IPAddr
	var tcpPort int
	{
		s := i
		for {
			if i >= len(l) {
				err = fmt.Errorf("invalid EPRT/EPSV address: %s", string(l))
				return
			}
			if l[i] == d {
				break
			}
			i++
		}
		afStr := l[s:i]
		if len(afStr) != 0 || !allowEmptyAfAddr {
			af, err = strconv.Atoi(string(afStr))
			if err != nil {
				err = errors.Wrap(err, fmt.Sprintf("invalid EPRT/EPSV address: %s", string(l)))
				return
			}
			if af != 1 && af != 2 {
				// invalid address family
				err = fmt.Errorf("invalid EPRT/EPSV address: %s", string(l))
				return
			}
		}
	}
	i++
	{
		s := i
		for {
			if i >= len(l) {
				err = fmt.Errorf("invalid EPRT/EPSV address: %s", string(l))
				return
			}
			if l[i] == d {
				break
			}
			i++
		}
		netAddrStr := l[s:i]
		if len(netAddrStr) != 0 || !allowEmptyAfAddr {
			var _netAddr *net.IPAddr
			_netAddr, err = net.ResolveIPAddr(
				afIntToString(af),
				string(netAddrStr),
			)
			if err != nil {
				err = errors.Wrap(err, fmt.Sprintf("invalid EPRT/EPSV address: %s", string(l)))
				return
			}
			netAddr = *_netAddr
		}
	}
	i++
	{
		s := i
		for {
			if i >= len(l) {
				err = fmt.Errorf("invalid EPRT/EPSV address: %s", string(l))
				return
			}
			if l[i] == d {
				break
			}
			i++
		}
		tcpPortStr := l[s:i]
		tcpPort, err = strconv.Atoi(string(tcpPortStr))
		if err != nil {
			err = errors.Wrap(err, fmt.Sprintf("invalid EPRT/EPSV address: %s", string(l)))
			return
		}
	}
	i++
	for {
		if i >= len(l) {
			break
		}
		if l[i] != 0x20 && l[i] != 0x09 && l[i] != 0x0d && l[i] != 0x0a {
			break
		}
		i++
	}
	if i != len(l) {
		// trailing characters
		err = fmt.Errorf("invalid EPRT/EPSV address: %s", string(l))
		return
	}
	pair = IPAddrPortPair{
		Addr: netAddr,
		Port: tcpPort,
	}
	return
}

func parseEprt(l []byte) (pair IPAddrPortPair, ok bool) {
	if len(l) < 5 {
		return
	}
	if l[0] != byte('E') || l[1] != byte('P') || l[2] != byte('R') || l[3] != byte('T') || (l[4] != 0x20 && l[4] != 0x09) {
		return
	}
	pair, err := parseEprtEpsvHostPort(l[5:], false)
	ok = err == nil
	return
}

func parsePasvReply(m []byte) (pair IPAddrPortPair, err error) {
	s := bytes.IndexByte(m, '(')
	if s < 0 {
		err = fmt.Errorf("invalid PASV reply: %s", string(m))
		return
	}
	s++
	e := bytes.IndexByte(m[s:], ')')
	if e < 0 {
		err = fmt.Errorf("invalid PASV reply: %s", string(m))
		return
	}
	e += s
	return parsePortPasvOctets(m[s:e])
}

func parseEpsvReply(m []byte) (pair IPAddrPortPair, err error) {
	s := bytes.IndexByte(m, '(')
	if s < 0 {
		err = fmt.Errorf("invalid EPSV reply: %s", string(m))
		return
	}
	s++
	e := bytes.IndexByte(m[s:], ')')
	if e < 0 {
		err = fmt.Errorf("invalid EPSV reply: %s", string(m))
		return
	}
	e += s
	return parseEprtEpsvHostPort(m[s:e], true)
}

type FTPReplyParser struct {
	Reply        FTPReply
	continuation bool
}

func (frp *FTPReplyParser) Feed(lr *LineReader) (ok bool, err error) {
	l := lr.Get()
	matches := statusRespRegex.FindAllSubmatch(l, 1)
	if matches == nil {
		if frp.continuation {
			frp.Reply.Message = append(frp.Reply.Message, l...)
		} else {
			frp.Reply.Code = -1
			frp.Reply.Message = l
			frp.Reply.Image = lr.GetFromMarker()
			ok = true
			lr.Mark()
		}
	} else {
		code := int(matches[0][1][0]-'0')*100 + int(matches[0][1][1]-'0')*10 + int(matches[0][1][2]-'0')
		continuation := matches[0][2][0] == '-'
		message := matches[0][3]
		if frp.continuation {
			if continuation || code != frp.Reply.Code {
				frp.Reply.Message = append(frp.Reply.Message, l...)
			} else {
				frp.Reply.Message = append(frp.Reply.Message, message...)
				frp.Reply.Image = lr.GetFromMarker()
				lr.Mark()
				ok = true
			}
		} else {
			frp.Reply.Code = code
			if !continuation {
				frp.Reply.Message = message
				frp.Reply.Image = lr.GetFromMarker()
				lr.Mark()
				ok = true
			} else {
				buf := make([]byte, len(message))
				copy(buf, message)
				frp.Reply.Message = message
			}
		}
	}
	return
}

func (fc *FTPSession) Err() error {
	return fc.err
}

func (fc *FTPSession) Done() <-chan struct{} {
	return fc.doneChan
}
func (fc *FTPSession) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (fc *FTPSession) Value(interface{}) interface{} {
	return nil
}

func (fc *FTPSession) Now() time.Time {
	return fc.nowGetter()
}

func (fc *FTPSession) Cancel() {
	if fc.canceled {
		return
	}
	fc.canceled = true
	close(fc.doneChan)
	log.Println("Session canceled")
}

func (fc *FTPSession) flushSpooledCommands() error {
	for len(fc.outboundCommandSpool) > 0 {
		_, err := fc.destConn.Write(fc.outboundCommandSpool[0])
		if err != nil {
			return err
		}
		fc.outboundCommandSpool = fc.outboundCommandSpool[1:]
	}
	return nil
}

type handlerFuncs struct {
	inboundFuncChangeChan  chan inboundHandlerFunc
	outboundFuncChangeChan chan outboundHandlerFunc
	inbound                inboundHandlerFunc
	outbound               outboundHandlerFunc
}

func (fns *handlerFuncs) setInbound(fn inboundHandlerFunc) {
	fns.inboundFuncChangeChan <- fn
}

func (fns *handlerFuncs) setOutbound(fn outboundHandlerFunc) {
	fns.outboundFuncChangeChan <- fn
}

type inboundHandlerFunc func(*FTPSession, *handlerFuncs, FTPReply) error
type outboundHandlerFunc func(*FTPSession, *handlerFuncs, []byte) error

func handleInboundPassthru(fc *FTPSession, _ *handlerFuncs, rep FTPReply) error {
	_, err := writeWithDeadline(fc.srcConn, rep.Image, fc.Now().Add(writeTimeout))
	if err != nil {
		return err
	}
	return nil
}

func handleInboundPreAuth(fc *FTPSession, fns *handlerFuncs, rep FTPReply) error {
	_, err := writeWithDeadline(fc.srcConn, rep.Image, fc.Now().Add(writeTimeout))
	if err != nil {
		return err
	}
	if rep.Code == 230 || rep.Code == 530 {
		fns.setInbound(handleInboundPassthru)
		fns.setOutbound(handleOutboundPostAuth)
		return nil
	}
	return nil
}

func handleInboundPasv(fc *FTPSession, fns *handlerFuncs, rep FTPReply) error {
	switch rep.Code {
	case 227:
		pair, err := parsePasvReply(rep.Message)
		if err != nil {
			return err
		}
		log.Printf("PASV: %s\n", pair.String())
		err = fc.connectRemoteData(pair)
		if err != nil {
			return err
		}
		fns.setInbound(handleInboundAwaitOneAndStartProxy)
		fns.setOutbound(handleOutboundPassthru)
		fc.flushSpooledCommands()
		return nil
	case 229:
		pair, err := parseEpsvReply(rep.Message)
		if err != nil {
			return err
		}
		if pair.Addr.IP.IsUnspecified() {
			pair.Addr = fc.destAddr.Addr
		}
		log.Printf("EPSV: %s, %s\n", fc.destAddr.Addr.String(), pair.String())
		err = fc.connectRemoteData(pair)
		if err != nil {
			return err
		}
		fns.setInbound(handleInboundAwaitOneAndStartProxy)
		fns.setOutbound(handleOutboundPassthru)
		fc.flushSpooledCommands()
		return nil
	default:
		_, err := writeWithDeadline(fc.srcConn, rep.Image, fc.Now().Add(writeTimeout))
		if err != nil {
			return err
		}
	}
	return nil
}

func handleInboundAwaitOneAndStartProxy(fc *FTPSession, fns *handlerFuncs, rep FTPReply) error {
	_, err := writeWithDeadline(fc.srcConn, rep.Image, fc.Now().Add(writeTimeout))
	if err != nil {
		return err
	}
	fns.setInbound(handleInboundPassthru)
	fns.setOutbound(handleOutboundPostAuth)
	return fc.startLocalRemoteProxy()
}

func handleOutboundPassthru(fc *FTPSession, _ *handlerFuncs, l []byte) error {
	_, ok := parsePort(l)
	if !ok {
		_, ok = parseEprt(l)
	}
	if ok {
		fc.sendReply(
			FTPReply{
				Code:    503,
				Message: []byte("unsupported command sequence"),
			},
		)
		return nil
	}
	_, err := writeWithDeadline(fc.destConn, l, fc.Now().Add(writeTimeout))
	if err != nil {
		return err
	}
	return nil
}

func handleOutboundSpool(fc *FTPSession, fns *handlerFuncs, l []byte) error {
	fc.outboundCommandSpool = append(fc.outboundCommandSpool, l)
	return nil
}

func handleOutboundPostAuth(fc *FTPSession, fns *handlerFuncs, l []byte) error {
	cmd := "PORT"
	pair, ok := parsePort(l)
	if !ok {
		pair, ok = parseEprt(l)
		cmd = "EPRT"
	}
	if ok {
		err := fc.sendReply(
			FTPReply{
				Code:    200,
				Message: []byte(fmt.Sprintf("%s command successful", cmd)),
			},
		)
		if err != nil {
			return err
		}
		fns.setOutbound(handleOutboundSpool)
		fc.localDataSubmAddr = pair
		var reqLine []byte
		if fc.destAddr.Addr.IP.To4() != nil {
			reqLine = pasvReqLine
			fns.setInbound(handleInboundPasv)
		} else {
			reqLine = epsvReqLine
			fns.setInbound(handleInboundPasv)
		}
		log.Printf("*>> %s", reqLine)
		_, err = fc.destConn.Write(reqLine)
		if err != nil {
			return err
		}
		log.Print(pair.String())
	} else {
		_, err := writeWithDeadline(fc.destConn, l, fc.Now().Add(writeTimeout))
		if err != nil {
			return err
		}
	}
	return nil
}

func copyStream(ctx NowgettableContext, dest WriterWithDeadline, src ReaderWithDeadline) error {
	buf := make([]byte, 4096)
	for {
		n, err := cancelableRead(ctx, src, buf)
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		wn, err := cancelableWriteWithDeadline(ctx, dest, buf[:n], ctx.Now().Add(writeTimeout))
		if err != nil {
			return err
		}
		if wn < n {
			return fmt.Errorf("Could not fully write data out of buffer")
		}
	}
	return nil
}

func (fc *FTPSession) sendAbort() error {
	var errs []error
	errs = append(errs, fc.sendReply(
		FTPReply{
			Code:    426,
			Message: []byte("Connection closed; transfer aborted."),
		},
	))
	_, err := fc.destConn.Write(abrtReqLine)
	errs = append(errs, err)
	if len(errs) > 0 {
		return fmt.Errorf("something went wrong")
	}
	return nil
}

func (fc *FTPSession) connectRemoteData(pair IPAddrPortPair) error {
	log.Printf("Connecting to %s...\n", pair.String())
	var err error
	fc.remoteDataConn, err = fc.dialer.DialContext(fc, "tcp", pair.String())
	if err != nil {
		_, _ = fc.destConn.Write(abrtReqLine)
		return err
	}
	log.Printf("Connected to %s\n", pair.String())
	return nil
}

func (fc *FTPSession) startLocalRemoteProxy() error {
	log.Printf("Connecting to %s...\n", fc.localDataSubmAddr.String())
	localConn, err := fc.dialer.DialContext(fc, "tcp", fc.localDataSubmAddr.String())
	if err != nil {
		fc.remoteDataConn.Close()
		fc.sendAbort()
		return err
	}
	go func() {
		ctx, cancel := WithCancel(fc)
		defer cancel()
		defer fc.remoteDataConn.Close()
		defer localConn.Close()
		log.Printf("Connected to %s\n", fc.localDataSubmAddr.String())
		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			err := copyStream(ctx, fc.remoteDataConn, localConn)
			if err != nil && err != canceled {
				log.Printf("%v\n", err)
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			err := copyStream(ctx, localConn, fc.remoteDataConn)
			if err != nil && err != canceled {
				log.Printf("%v\n", err)
			}
		}()
		wg.Wait()
		log.Println("local-remote proxy completed")
	}()
	return nil
}

func (fc *FTPSession) start() {
	wg := sync.WaitGroup{}
	handlerFuncs := handlerFuncs{
		make(chan inboundHandlerFunc, 1),
		make(chan outboundHandlerFunc, 1),
		handleInboundPreAuth,
		handleOutboundPassthru,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		replyParser := FTPReplyParser{}
	outer:
		for {
			select {
			case <-fc.destContext.Done():
				fc.Cancel()
				break outer
			case fn := <-handlerFuncs.inboundFuncChangeChan:
				log.Printf("Inbound func changed to %s\n", runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name())
				handlerFuncs.inbound = fn
				break
			case lr := <-fc.destContext.Data():
				if lr == nil {
					break
				}
				ok, err := replyParser.Feed(lr)
				if err != nil {
					fc.err = err
					fc.Cancel()
					continue
				}
				if !ok {
					continue
				}
				if handlerFuncs.inbound != nil {
					log.Printf("<<< %s", replyParser.Reply.Image)
					err := handlerFuncs.inbound(fc, &handlerFuncs, replyParser.Reply)
					if err != nil {
						fc.err = err
						log.Printf("--- %v\n", err)
						fc.Cancel()
					}
				}
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
	outer:
		for {
			select {
			case <-fc.srcContext.Done():
				fc.Cancel()
				break outer
			case fn := <-handlerFuncs.outboundFuncChangeChan:
				log.Printf("Outbound func changed to %s\n", runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name())
				handlerFuncs.outbound = fn
				break
			case lr := <-fc.srcContext.Data():
				if lr == nil {
					break
				}
				lr.Mark()
				if handlerFuncs.outbound != nil {
					l := lr.Get()
					log.Printf(">>> %s", l)
					err := handlerFuncs.outbound(fc, &handlerFuncs, l)
					if err != nil {
						log.Printf("--- %v\n", err)
						fc.err = err
						fc.Cancel()
					}
				}
			}
		}
	}()

	go func() {
		wg.Wait()
		fc.finishChan <- struct{}{}
	}()
}

func newFTPSession(srcConn *net.TCPConn, newDestFunc ConnGenFunc, nowGetter NowGetter) (fc *FTPSession) {
	fc = &FTPSession{
		srcConn:    srcConn,
		err:        nil,
		doneChan:   make(chan struct{}),
		finishChan: make(chan struct{}),
		nowGetter:  nowGetter,
		dialer:     net.Dialer{},
	}
	fc.srcContext = NewFTPReaderContext(fc, srcConn)
	log.Printf("FTPReaderContext (%p) created for requests\n", fc.srcContext)
	destConn, err := newDestFunc(fc)
	if err != nil {
		_err := fc.sendReply(FTPReply{Code: 421, Message: []byte("Service Not Available")})
		if _err != nil {
			log.Printf("%v\n", _err)
		}
		fc.err = err
		fc.Cancel()
		return
	}
	addrStr, portStr, err := net.SplitHostPort(destConn.RemoteAddr().String())
	if err != nil {
		fc.err = err
		fc.Cancel()
		return
	}
	addr, err := net.ResolveIPAddr("ip", addrStr)
	if err != nil {
		fc.err = err
		fc.Cancel()
		return
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		fc.err = err
		fc.Cancel()
		return
	}
	log.Printf("Connection to %v established\n", destConn.RemoteAddr())
	fc.destConn = destConn
	fc.destAddr.Addr = *addr
	fc.destAddr.Port = port
	fc.destContext = NewFTPReaderContext(fc, destConn)
	log.Printf("FTPReaderContext (%p) created for replies\n", fc.destContext)
	fc.start()
	return
}

func startServing(l *net.TCPListener, connGen ConnGenFunc) error {
	for {
		cn, err := l.AcceptTCP()
		if err != nil {
			return err
		}
		go func(n *net.TCPConn) {
			log.Printf("Accepted new connection from %v\n", n.RemoteAddr())
			fc := newFTPSession(n, connGen, time.Now)
			<-fc.finishChan
			log.Printf("FTP session with %v ended\n", n.RemoteAddr())
			err = fc.Err()
			if err != nil {
				log.Printf("%v\n", err)
			}
		}(cn)
	}
}

func putError(message string) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", os.Args[0], message)
}

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) < 2 {
		putError("too few arguments")
		os.Exit(255)
	}
	l, err := net.Listen("tcp", args[0])
	if err != nil {
		putError(err.Error())
	}
	dest := args[1]
	dialer := net.Dialer{
		Timeout: time.Duration(1000000000),
	}
	err = startServing(l.(*net.TCPListener), func(ctx context.Context) (*net.TCPConn, error) {
		log.Printf("Trying to connect to %v...\n", dest)
		conn, err := dialer.DialContext(ctx, "tcp", dest)
		if err != nil {
			return nil, err
		}
		return conn.(*net.TCPConn), err
	})
	if err != nil {
		putError(err.Error())
	}
}
