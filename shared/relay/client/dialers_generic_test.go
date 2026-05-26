//go:build !js

package client

import (
	"os"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/netbirdio/netbird/client/iface"
	"github.com/netbirdio/netbird/shared/relay/client/dialer"
	netErr "github.com/netbirdio/netbird/shared/relay/client/dialer/net"
	"github.com/netbirdio/netbird/shared/relay/client/dialer/quic"
	"github.com/netbirdio/netbird/shared/relay/client/dialer/ws"
)

// TestDatagramSizedCapability locks the capability the generic fallback relies
// on: QUIC is datagram-sized, WebSocket is not.
func TestDatagramSizedCapability(t *testing.T) {
	assert.True(t, dialer.IsDatagramSized(quic.Dialer{}), "QUIC must advertise datagram-sized")
	assert.False(t, dialer.IsDatagramSized(ws.Dialer{}), "WebSocket must not advertise datagram-sized")
}

func protocols(dialers []dialer.DialeFn) []string {
	out := make([]string, len(dialers))
	for i, d := range dialers {
		out[i] = d.Protocol()
	}
	return out
}

// clearRelayProxyEnv neutralises any proxy configuration inherited from the
// developer's shell. getDialers consults the environment, so without this a
// machine with HTTPS_PROXY set would see every transport collapse to WebSocket.
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

func TestGetDialers(t *testing.T) {
	const url = "rels://relay.example:443"

	tests := []struct {
		name     string
		mode     string
		mtu      uint16
		preferWS bool
		want     []string
	}{
		{name: "auto races quic and ws", mode: "auto", mtu: iface.DefaultMTU, want: []string{"quic", "ws"}},
		{name: "ws pinned", mode: "ws", mtu: iface.DefaultMTU, want: []string{"ws"}},
		{name: "quic pinned", mode: "quic", mtu: iface.DefaultMTU, want: []string{"quic"}},
		{name: "prefer-quic orders quic first", mode: "prefer-quic", mtu: iface.DefaultMTU, want: []string{"quic", "ws"}},
		{name: "prefer-ws orders ws first", mode: "prefer-ws", mtu: iface.DefaultMTU, want: []string{"ws", "quic"}},
		{name: "mtu above default forces ws", mode: "auto", mtu: iface.DefaultMTU + 100, want: []string{"ws"}},
		{name: "sticky fallback forces ws in auto", mode: "auto", mtu: iface.DefaultMTU, preferWS: true, want: []string{"ws"}},
		{name: "sticky fallback forces ws in prefer-quic", mode: "prefer-quic", mtu: iface.DefaultMTU, preferWS: true, want: []string{"ws"}},
		{name: "quic pin overrides sticky fallback", mode: "quic", mtu: iface.DefaultMTU, preferWS: true, want: []string{"quic"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearRelayProxyEnv(t)
			t.Setenv(EnvRelayTransport, tc.mode)
			if tc.mode == "" {
				os.Unsetenv(EnvRelayTransport)
			}

			tf := newTransportFallback()
			if tc.preferWS {
				tf.recordFailure(url)
			}

			c := &Client{
				log:               log.WithField("test", t.Name()),
				connectionURL:     url,
				mtu:               tc.mtu,
				transportFallback: tf,
			}

			assert.Equal(t, tc.want, protocols(c.getDialers(transportModeFromEnv())))
		})
	}
}

// TestGetDialersProxyForcesWebSocket locks the Publify behaviour: an HTTP proxy
// can only carry TCP via CONNECT, so QUIC is unreachable and WebSocket must win
// over every transport mode, including an explicit QUIC pin.
func TestGetDialersProxyForcesWebSocket(t *testing.T) {
	const url = "rels://relay.example:443"

	for _, mode := range []string{"auto", "quic", "prefer-quic", "prefer-ws", "ws"} {
		t.Run(mode, func(t *testing.T) {
			clearRelayProxyEnv(t)
			t.Setenv("HTTPS_PROXY", "http://127.0.0.1:8080")
			t.Setenv(EnvRelayTransport, mode)

			c := &Client{
				log:               log.WithField("test", t.Name()),
				connectionURL:     url,
				mtu:               iface.DefaultMTU,
				transportFallback: newTransportFallback(),
			}

			assert.Equal(t, []string{"ws"}, protocols(c.getDialers(transportModeFromEnv())))
		})
	}
}

// TestGetDialersNoProxyKeepsDefaultOrder guards the inverse: NO_PROXY covering the
// relay host must leave the transport selection untouched.
func TestGetDialersNoProxyKeepsDefaultOrder(t *testing.T) {
	clearRelayProxyEnv(t)
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:8080")
	t.Setenv("NO_PROXY", "relay.example")
	t.Setenv(EnvRelayTransport, string(TransportModeAuto))

	c := &Client{
		log:               log.WithField("test", t.Name()),
		connectionURL:     "rels://relay.example:443",
		mtu:               iface.DefaultMTU,
		transportFallback: newTransportFallback(),
	}

	assert.Equal(t, []string{"quic", "ws"}, protocols(c.getDialers(transportModeFromEnv())))
}

// TestStickyFallbackAfterDatagramTooLarge verifies the full chain: an oversized
// datagram records a fallback that makes the next dial pick WebSocket, the way a
// reconnect would after the connection is closed.
func TestStickyFallbackAfterDatagramTooLarge(t *testing.T) {
	const url = "rels://relay.example:443"
	clearRelayProxyEnv(t)
	t.Setenv(EnvRelayTransport, string(TransportModeAuto))

	c := &Client{
		log:               log.WithField("test", t.Name()),
		connectionURL:     url,
		mtu:               iface.DefaultMTU,
		transportFallback: newTransportFallback(),
	}

	// First dial races both transports.
	assert.Equal(t, []string{"quic", "ws"}, protocols(c.getDialers(transportModeFromEnv())))

	// An oversized datagram records the fallback for this server.
	c.onDatagramTooLarge(&closeTrackingConn{}, netErr.ErrDatagramTooLarge)

	// The reconnect now sticks to WebSocket.
	assert.Equal(t, []string{"ws"}, protocols(c.getDialers(transportModeFromEnv())))
}
