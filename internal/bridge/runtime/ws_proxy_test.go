package runtime

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A one-shot CONNECT proxy that records the request and then echoes bytes.
func startConnectProxy(t *testing.T, status string) (addr string, gotRequest chan *http.Request) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	gotRequest = make(chan *http.Request, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			return
		}
		gotRequest <- req
		_, _ = io.WriteString(conn, "HTTP/1.1 "+status+"\r\n\r\n")
		_, _ = io.Copy(conn, conn)
	}()
	return ln.Addr().String(), gotRequest
}

func useProxy(t *testing.T, raw string) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse proxy: %v", err)
	}
	old := wsProxyFor
	wsProxyFor = func(string) (*url.URL, error) { return u, nil }
	t.Cleanup(func() { wsProxyFor = old })
}

func TestDialViaEnvProxy_TunnelsThroughConnect(t *testing.T) {
	addr, gotRequest := startConnectProxy(t, "200 Connection Established")
	useProxy(t, "http://user:pw@"+addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := dialViaEnvProxy(ctx, "tcp", "b1.cdp.example.com:443")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	req := <-gotRequest
	if req.Method != http.MethodConnect || req.Host != "b1.cdp.example.com:443" {
		t.Fatalf("proxy got %s %s", req.Method, req.Host)
	}
	if !strings.HasPrefix(req.Header.Get("Proxy-Authorization"), "Basic ") {
		t.Fatalf("missing proxy credentials: %v", req.Header)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write through tunnel: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("read through tunnel: %q %v", buf, err)
	}
}

func TestDialViaEnvProxy_RefusedConnectIsAnError(t *testing.T) {
	addr, _ := startConnectProxy(t, "403 Forbidden")
	useProxy(t, "http://"+addr)

	_, err := dialViaEnvProxy(context.Background(), "tcp", "b1.cdp.example.com:443")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("want a 403 error, got %v", err)
	}
}
