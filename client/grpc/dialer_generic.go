//go:build !js

package grpc

import (
	"context"
	"fmt"
	"net"
	"os/user"
	"runtime"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	nbnet "github.com/netbirdio/netbird/client/net"
)

func WithCustomDialer(tlsEnabled bool, component string) grpc.DialOption {
	return grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
		targetScheme := "http"
		if tlsEnabled {
			targetScheme = "https"
		}

		dialer, err := controlPlaneDialer()
		if err != nil {
			return nil, err
		}

		conn, proxyURL, err := nbnet.DialContextWithProxy(ctx, dialer, targetScheme, addr, "netbird-grpc")
		if err != nil {
			if proxyURL != nil {
				return nil, fmt.Errorf("dial %s via proxy %s: %w", component, nbnet.RedactedURL(proxyURL), err)
			}
			return nil, fmt.Errorf("dial %s directly: %w", component, err)
		}
		if proxyURL != nil {
			log.Debugf("connected to %s via proxy %s", component, nbnet.RedactedURL(proxyURL))
		}
		return conn, nil
	})
}

func controlPlaneDialer() (nbnet.ContextDialer, error) {
	if runtime.GOOS == "linux" {
		currentUser, err := user.Current()
		if err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get current user: %v", err)
		}

		// the custom dialer requires root permissions which are not required for use cases run as non-root
		if currentUser.Uid != "0" {
			log.Debug("Not running as root, using standard dialer")
			return &net.Dialer{}, nil
		}
	}

	return nbnet.NewDialer(), nil
}
