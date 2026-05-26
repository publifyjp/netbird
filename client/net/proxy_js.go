//go:build js

package net

import (
	"net/http"
	"net/url"
)

func ProxyFromEnvironment(_ *http.Request) (*url.URL, error) {
	return nil, nil
}

func RedactedURL(proxyURL *url.URL) string {
	if proxyURL == nil {
		return ""
	}
	redacted := *proxyURL
	redacted.User = nil
	return redacted.String()
}
