//go:build !js

package client

import (
	"github.com/netbirdio/netbird/client/iface"
	nbnet "github.com/netbirdio/netbird/client/net"
	"github.com/netbirdio/netbird/shared/relay/client/dialer"
	"github.com/netbirdio/netbird/shared/relay/client/dialer/quic"
	"github.com/netbirdio/netbird/shared/relay/client/dialer/ws"
)

// getDialers returns the ordered dialers for connecting to the relay server. It
// applies the datagram fallback generically: if this server recently rejected a
// datagram-sized transport, those dialers are dropped, leaving the rest.
func (c *Client) getDialers(mode TransportMode) []dialer.DialeFn {
	// An HTTP proxy can only carry TCP via CONNECT, so QUIC cannot reach the
	// relay through it. WebSocket is the only viable transport, which overrides
	// the requested mode rather than being filtered by it.
	if c.proxyForcesWebSocket() {
		return []dialer.DialeFn{ws.Dialer{}}
	}

	dialers := c.baseDialers(mode)

	if c.transportFallback != nil && c.transportFallback.avoidDatagramSized(c.connectionURL) {
		if filtered := nonDatagramSized(dialers); len(filtered) > 0 {
			c.log.Infof("relay recently rejected a datagram-sized transport, avoiding it")
			return filtered
		}
	}
	return dialers
}

// proxyForcesWebSocket reports whether an HTTP proxy stands between this client
// and the relay server. An unevaluable proxy configuration is treated as "proxied"
// so a malformed setting degrades to the transport that can traverse a proxy.
func (c *Client) proxyForcesWebSocket() bool {
	proxyConfigured, err := nbnet.ProxyConfiguredForRawURL(c.connectionURL)
	if err != nil {
		c.log.Warnf("failed to evaluate proxy for relay URL, forcing WebSocket transport: %v", err)
		return true
	}
	if proxyConfigured {
		c.log.Infof("HTTP proxy configured for Relay, forcing WebSocket transport")
	}
	return proxyConfigured
}

// baseDialers returns the ordered dialers for the mode, before any datagram
// fallback filtering. For racing modes (auto) the order is irrelevant; for
// prefer modes the first entry is tried before falling back to the second.
func (c *Client) baseDialers(mode TransportMode) []dialer.DialeFn {
	switch mode {
	case TransportModeWS:
		c.log.Infof("%s=ws, using WebSocket transport", EnvRelayTransport)
		return []dialer.DialeFn{ws.Dialer{}}
	case TransportModeQUIC:
		c.log.Infof("%s=quic, using QUIC transport", EnvRelayTransport)
		return []dialer.DialeFn{quic.Dialer{}}
	}

	all := []dialer.DialeFn{quic.Dialer{}, ws.Dialer{}}
	if mode == TransportModePreferWS {
		all = []dialer.DialeFn{ws.Dialer{}, quic.Dialer{}}
	}

	if c.mtu > 0 && c.mtu > iface.DefaultMTU {
		c.log.Infof("MTU %d exceeds default (%d), avoiding datagram-sized transports", c.mtu, iface.DefaultMTU)
		return nonDatagramSized(all)
	}
	return all
}
