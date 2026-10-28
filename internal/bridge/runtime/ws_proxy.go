package runtime

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gobwas/ws"
)

var installProxyDialOnce sync.Once

// wsProxyFor picks the proxy for a CDP websocket target; tests swap it because
// http.ProxyFromEnvironment reads the environment only once per process.
var wsProxyFor = func(addr string) (*url.URL, error) {
	return http.ProxyFromEnvironment(&http.Request{URL: &url.URL{Scheme: "https", Host: addr}})
}

// installProxyAwareWSDial makes chromedp's CDP websocket honour HTTPS_PROXY.
// chromedp dials through gobwas/ws's DefaultDialer, which always connects
// directly, so a sandbox whose only way out is an egress proxy could never
// reach a hosted browser. Loopback and NO_PROXY targets still dial directly.
func installProxyAwareWSDial() {
	installProxyDialOnce.Do(func() { ws.DefaultDialer.NetDial = dialViaEnvProxy })
}

func dialViaEnvProxy(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	proxyURL, err := wsProxyFor(addr)
	if err != nil || proxyURL == nil {
		return d.DialContext(ctx, network, addr)
	}
	if proxyURL.Scheme != "http" {
		return nil, fmt.Errorf("CDP websocket: unsupported proxy scheme %q (only http:// CONNECT proxies)", proxyURL.Scheme)
	}
	proxyAddr := proxyURL.Host
	if proxyURL.Port() == "" {
		proxyAddr = net.JoinHostPort(proxyURL.Hostname(), "80")
	}
	conn, err := d.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("CDP websocket: dial proxy %s: %w", proxyURL.Redacted(), err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: addr}, Host: addr, Header: http.Header{}}
	if u := proxyURL.User; u != nil {
		pass, _ := u.Password()
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(u.Username()+":"+pass)))
	}
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("CDP websocket: CONNECT %s via proxy: %w", addr, err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("CDP websocket: CONNECT %s via proxy: %w", addr, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("CDP websocket: CONNECT %s via proxy: %s", addr, resp.Status)
	}
	_ = conn.SetDeadline(time.Time{})
	if br.Buffered() > 0 {
		return &bufferedConn{Conn: conn, r: br}, nil
	}
	return conn, nil
}

// bufferedConn keeps bytes the proxy sent right after its CONNECT reply.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }
