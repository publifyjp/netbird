package ws

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestPrepareURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "rel scheme with non-standard port",
			input: "rel://test-domain-2:45678",
			want:  "ws://test-domain-2:45678/relay",
		},
		{
			name:  "rels scheme with non-standard port",
			input: "rels://test-domain-2:45678",
			want:  "wss://test-domain-2:45678/relay",
		},
		{
			name:  "rel scheme without port",
			input: "rel://test-domain-2",
			want:  "ws://test-domain-2/relay",
		},
		{
			name:  "rels scheme without port",
			input: "rels://test-domain-2",
			want:  "wss://test-domain-2/relay",
		},
		{
			name:  "rel scheme with IP and port",
			input: "rel://1.2.3.4:45678",
			want:  "ws://1.2.3.4:45678/relay",
		},
		{
			name:  "rel scheme with hostname starting with rel",
			input: "rel://relay.example.com:45678",
			want:  "ws://relay.example.com:45678/relay",
		},
		{
			name:  "rel scheme with IPv6 and port",
			input: "rel://[2001:db8::1]:45678",
			want:  "ws://[2001:db8::1]:45678/relay",
		},
		{
			name:  "rels scheme with IPv6 loopback and port",
			input: "rels://[::1]:45678",
			want:  "wss://[::1]:45678/relay",
		},
		{
			name:    "unsupported scheme",
			input:   "http://test-domain-2:45678",
			wantErr: true,
		},
		{
			name:    "no scheme",
			input:   "test-domain-2:45678",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := prepareURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("prepareURL(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("prepareURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestHTTPClientNbDialerUsesProxyConnectForWSS(t *testing.T) {
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
	}()

	t.Setenv("HTTPS_PROXY", "http://"+listener.Addr().String())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://relay.example.com/relay", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := httpClientNbDialer("relay.example.com", nil).Do(req)
	if err == nil {
		defer resp.Body.Close()
		t.Fatal("httpClientNbDialer().Do() succeeded, want TLS failure after CONNECT")
	}

	select {
	case req := <-reqCh:
		if req.Method != http.MethodConnect {
			t.Fatalf("proxy request method = %s, want CONNECT", req.Method)
		}
		if req.URL.Host != "relay.example.com:443" {
			t.Fatalf("proxy request URL host = %q, want relay.example.com:443", req.URL.Host)
		}
	case err := <-errCh:
		t.Fatalf("proxy server error = %v", err)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for proxy CONNECT request")
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
