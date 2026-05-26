//go:build !js

package net

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/http/httpproxy"
)

const proxyAuthHeaderKey = "Proxy-Authorization"

// ContextDialer is the subset implemented by net.Dialer and NetBird's Dialer.
type ContextDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// ProxyFromEnvironment resolves a proxy dynamically for each request.
//
// The standard http.ProxyFromEnvironment caches environment variables on first
// use. Android passes proxy settings at runtime before starting the Go engine,
// so keep this dynamic.
func ProxyFromEnvironment(req *http.Request) (*url.URL, error) {
	return httpproxy.FromEnvironment().ProxyFunc()(req.URL)
}

// ProxyURLForAddress returns the configured proxy for a target scheme/address.
func ProxyURLForAddress(targetScheme, targetAddr string) (*url.URL, error) {
	if targetScheme == "" {
		targetScheme = "https"
	}
	reqURL := &url.URL{
		Scheme: targetScheme,
		Host:   targetAddr,
	}
	return httpproxy.FromEnvironment().ProxyFunc()(reqURL)
}

// ProxyURLForRawURL returns the configured HTTP proxy for HTTP(S), WS(S), or
// NetBird relay URL schemes.
func ProxyURLForRawURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("missing host in url %q", rawURL)
	}

	scheme := parsed.Scheme
	switch scheme {
	case "rel", "ws", "http":
		scheme = "http"
	case "rels", "wss", "https":
		scheme = "https"
	default:
		return nil, fmt.Errorf("unsupported proxy target scheme %q", parsed.Scheme)
	}
	return ProxyURLForAddress(scheme, parsed.Host)
}

// ProxyConfiguredForRawURL reports whether a URL should use an HTTP proxy.
func ProxyConfiguredForRawURL(rawURL string) (bool, error) {
	proxyURL, err := ProxyURLForRawURL(rawURL)
	if err != nil {
		return false, err
	}
	return proxyURL != nil, nil
}

// DialContextWithProxy dials targetAddr directly unless proxy environment
// variables require HTTP CONNECT tunneling.
func DialContextWithProxy(ctx context.Context, dialer ContextDialer, targetScheme, targetAddr, userAgent string) (net.Conn, *url.URL, error) {
	proxyURL, err := ProxyURLForAddress(targetScheme, targetAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve proxy for %s: %w", targetAddr, err)
	}
	if proxyURL == nil {
		conn, err := dialer.DialContext(ctx, "tcp", targetAddr)
		return conn, nil, err
	}

	proxyAddr, err := proxyAddress(proxyURL)
	if err != nil {
		return nil, proxyURL, err
	}

	proxyConn, err := dialer.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, proxyURL, fmt.Errorf("dial proxy %s: %w", RedactedURL(proxyURL), err)
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = proxyConn.SetDeadline(deadline)
		defer func() { _ = proxyConn.SetDeadline(time.Time{}) }()
	}

	conn := proxyConn
	if strings.EqualFold(proxyURL.Scheme, "https") {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: proxyURL.Hostname()})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, proxyURL, fmt.Errorf("tls handshake with proxy %s: %w", RedactedURL(proxyURL), err)
		}
		conn = tlsConn
	}

	conn, err = doHTTPConnect(ctx, conn, targetAddr, proxyURL, userAgent)
	if err != nil {
		return nil, proxyURL, err
	}
	return conn, proxyURL, nil
}

func proxyAddress(proxyURL *url.URL) (string, error) {
	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https":
	default:
		return "", fmt.Errorf("unsupported proxy scheme %q", proxyURL.Scheme)
	}

	host := proxyURL.Hostname()
	if host == "" {
		return "", fmt.Errorf("missing proxy host in %q", RedactedURL(proxyURL))
	}
	port := proxyURL.Port()
	if port == "" {
		if strings.EqualFold(proxyURL.Scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(host, port), nil
}

func doHTTPConnect(ctx context.Context, conn net.Conn, targetAddr string, proxyURL *url.URL, userAgent string) (_ net.Conn, err error) {
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()

	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Host: targetAddr},
		Header: make(http.Header),
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	if proxyURL.User != nil {
		username := proxyURL.User.Username()
		password, _ := proxyURL.User.Password()
		req.Header.Set(proxyAuthHeaderKey, "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
	}

	if err := req.WithContext(ctx).Write(conn); err != nil {
		return nil, fmt.Errorf("write CONNECT request: %w", err)
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		return nil, fmt.Errorf("read CONNECT response: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CONNECT failed with status %s", resp.Status)
	}

	if reader.Buffered() == 0 {
		return conn, nil
	}
	return &bufConn{Conn: conn, reader: reader}, nil
}

type bufConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufConn) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

// RedactedURL formats a proxy URL without credentials.
func RedactedURL(proxyURL *url.URL) string {
	if proxyURL == nil {
		return ""
	}
	redacted := *proxyURL
	redacted.User = nil
	return redacted.String()
}
