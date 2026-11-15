package egress

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Proxy is the transparent egress proxy. The firewall redirects outbound
// TCP 80 and 443 here; the proxy reads the hostname the client asked for
// (HTTP Host header / TLS SNI — never decrypting anything), checks it
// against the current policy, and connects to that hostname itself. Dialing
// the name rather than the original destination IP means a client cannot
// claim an allowed name while connecting somewhere else.
type Proxy struct {
	// Dial connects upstream (tests swap it; the worker's dialer marks its
	// sockets so the firewall lets them out).
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)

	mu       sync.RWMutex
	matcher  *Matcher // nil = allow all
	onBlock  func(host string, port int)
	accepted atomic.Int64
}

// NewProxy returns a proxy that allows everything until SetPolicy.
func NewProxy(dial func(ctx context.Context, network, addr string) (net.Conn, error)) *Proxy {
	return &Proxy{Dial: dial}
}

// SetPolicy swaps the active policy: hosts are the full allowlist (required
// hosts included); allowAll disables checking.
func (p *Proxy) SetPolicy(allowAll bool, hosts []string, onBlock func(host string, port int)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if allowAll {
		p.matcher = nil
	} else {
		p.matcher = NewMatcher(hosts)
	}
	p.onBlock = onBlock
}

func (p *Proxy) check(host string, port int) bool {
	p.mu.RLock()
	m, cb := p.matcher, p.onBlock
	p.mu.RUnlock()
	if m == nil || m.Allowed(host) {
		return true
	}
	if cb != nil {
		cb(host, port)
	}
	return false
}

// ServeTLS accepts redirected port-443 connections until l closes.
func (p *Proxy) ServeTLS(l net.Listener) error { return p.serve(l, 443, peekSNI) }

// ServeHTTP accepts redirected port-80 connections until l closes.
func (p *Proxy) ServeHTTP(l net.Listener) error { return p.serve(l, 80, peekHost) }

func (p *Proxy) serve(l net.Listener, port int, peek func(io.Reader) (string, error)) error {
	for {
		c, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		p.accepted.Add(1)
		go p.handle(c, port, peek)
	}
}

func (p *Proxy) handle(c net.Conn, port int, peek func(io.Reader) (string, error)) {
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(15 * time.Second))
	var seen bytes.Buffer
	host, err := peek(io.TeeReader(c, &seen))
	if err != nil || host == "" {
		// No hostname (IP-literal TLS, garbage, ECH without outer SNI…):
		// nothing to match an allowlist against.
		p.check("", port)
		return
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h // "Host: example.com:80"
	}
	if !p.check(host, port) {
		if port == 80 {
			fmt.Fprintf(c, "HTTP/1.1 403 Forbidden\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\nquill egress: %s is not on this repository's allowlist\n", host)
		}
		return
	}
	_ = c.SetReadDeadline(time.Time{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	up, err := p.Dial(ctx, "tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	cancel()
	if err != nil {
		return
	}
	defer up.Close()
	if _, err := up.Write(seen.Bytes()); err != nil {
		return
	}
	pipe(c, up)
}

// pipe copies both ways until both directions are done.
func pipe(a, b net.Conn) {
	var wg sync.WaitGroup
	cp := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			_ = dst.Close()
		}
	}
	wg.Add(2)
	go cp(a, b)
	go cp(b, a)
	wg.Wait()
}

var errPeeked = errors.New("peeked")

// peekSNI reads a TLS ClientHello and returns its server name.
func peekSNI(r io.Reader) (string, error) {
	var name string
	err := tls.Server(readOnlyConn{r}, &tls.Config{
		GetConfigForClient: func(h *tls.ClientHelloInfo) (*tls.Config, error) {
			name = h.ServerName
			return nil, errPeeked
		},
	}).Handshake()
	if name != "" {
		return name, nil
	}
	if err == nil {
		err = errors.New("no server name")
	}
	return "", err
}

// peekHost reads an HTTP/1.x request head and returns its Host.
func peekHost(r io.Reader) (string, error) {
	req, err := http.ReadRequest(bufio.NewReader(r))
	if err != nil {
		return "", err
	}
	return req.Host, nil
}

// readOnlyConn feeds the TLS server the client's bytes and swallows its
// replies (the handshake is aborted before any are sent).
type readOnlyConn struct{ r io.Reader }

func (c readOnlyConn) Read(b []byte) (int, error)       { return c.r.Read(b) }
func (readOnlyConn) Write(b []byte) (int, error)        { return 0, io.ErrClosedPipe }
func (readOnlyConn) Close() error                       { return nil }
func (readOnlyConn) LocalAddr() net.Addr                { return nil }
func (readOnlyConn) RemoteAddr() net.Addr               { return nil }
func (readOnlyConn) SetDeadline(t time.Time) error      { return nil }
func (readOnlyConn) SetReadDeadline(t time.Time) error  { return nil }
func (readOnlyConn) SetWriteDeadline(t time.Time) error { return nil }

// PublicDialer returns a dialer that refuses non-public destinations (the
// metadata service, loopback, private and link-local ranges: an allowed
// name must not resolve its way into the instance's neighbourhood) and
// applies control (e.g. the firewall mark) to every upstream socket.
func PublicDialer(control func(fd uintptr) error) func(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{
		Timeout: 15 * time.Second,
		Control: func(network, address string, rc syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || !PublicIP(ip) {
				return fmt.Errorf("egress: %s is not a public address", host)
			}
			if control == nil {
				return nil
			}
			var cerr error
			if err := rc.Control(func(fd uintptr) { cerr = control(fd) }); err != nil {
				return err
			}
			return cerr
		},
	}
	return d.DialContext
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// PublicIP reports whether ip is a globally routable unicast address.
func PublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() || cgnat.Contains(ip) {
		return false
	}
	return ip.IsGlobalUnicast()
}
