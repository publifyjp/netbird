//go:build !js

package net

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestDialContextWithProxyUsesHTTPConnect(t *testing.T) {
	clearProxyEnv(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen proxy: %v", err)
	}
	defer listener.Close()

	reqCh := make(chan *http.Request, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()

		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			errCh <- err
			return
		}
		reqCh <- req

		if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
			errCh <- err
			return
		}
		time.Sleep(50 * time.Millisecond)
	}()

	t.Setenv("HTTPS_PROXY", "http://"+listener.Addr().String())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	conn, proxyURL, err := DialContextWithProxy(ctx, &net.Dialer{}, "https", "management.example.com:443", "netbird-test")
	if err != nil {
		t.Fatalf("DialContextWithProxy() error = %v", err)
	}
	defer conn.Close()
	if proxyURL == nil {
		t.Fatal("DialContextWithProxy() proxyURL = nil")
	}

	select {
	case req := <-reqCh:
		if req.Method != http.MethodConnect {
			t.Fatalf("proxy request method = %s, want CONNECT", req.Method)
		}
		if req.URL.Host != "management.example.com:443" {
			t.Fatalf("proxy request URL host = %q, want management.example.com:443", req.URL.Host)
		}
		if got := req.Header.Get("User-Agent"); got != "netbird-test" {
			t.Fatalf("proxy request user-agent = %q, want netbird-test", got)
		}
	case err := <-errCh:
		t.Fatalf("proxy server error = %v", err)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for proxy CONNECT request")
	}
}

func TestProxyURLForAddressHonorsNoProxy(t *testing.T) {
	clearProxyEnv(t)

	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:8080")
	t.Setenv("NO_PROXY", "management.example.com")

	proxyURL, err := ProxyURLForAddress("https", "management.example.com:443")
	if err != nil {
		t.Fatalf("ProxyURLForAddress() error = %v", err)
	}
	if proxyURL != nil {
		t.Fatalf("ProxyURLForAddress() = %v, want nil", proxyURL)
	}
}

func clearProxyEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"HTTP_PROXY",
		"http_proxy",
		"HTTPS_PROXY",
		"https_proxy",
		"NO_PROXY",
		"no_proxy",
		"REQUEST_METHOD",
	} {
		t.Setenv(key, "")
	}
}
