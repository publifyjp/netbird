//go:build !js

package client

import (
	"testing"

	"github.com/netbirdio/netbird/client/iface"
	"github.com/netbirdio/netbird/shared/relay/auth/hmac"
)

func TestGetDialersForcesWebSocketWhenProxyConfigured(t *testing.T) {
	clearRelayProxyEnv(t)
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:8080")

	client := NewClient("rels://relay.example.com:443", &hmac.TokenStore{}, "peer", iface.DefaultMTU)
	dialers := client.getDialers()

	if len(dialers) != 1 {
		t.Fatalf("getDialers() len = %d, want 1", len(dialers))
	}
	if got := dialers[0].Protocol(); got != "WS" {
		t.Fatalf("getDialers()[0].Protocol() = %q, want WS", got)
	}
}

func TestGetDialersKeepsDefaultOrderWithoutProxy(t *testing.T) {
	clearRelayProxyEnv(t)

	client := NewClient("rels://relay.example.com:443", &hmac.TokenStore{}, "peer", iface.DefaultMTU)
	dialers := client.getDialers()

	if len(dialers) != 2 {
		t.Fatalf("getDialers() len = %d, want 2", len(dialers))
	}
	if got := dialers[0].Protocol(); got != "quic" {
		t.Fatalf("getDialers()[0].Protocol() = %q, want quic", got)
	}
	if got := dialers[1].Protocol(); got != "WS" {
		t.Fatalf("getDialers()[1].Protocol() = %q, want WS", got)
	}
}

func clearRelayProxyEnv(t *testing.T) {
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
