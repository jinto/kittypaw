package browser

import (
	"fmt"
	"net/url"

	"github.com/jinto/kittypaw/core"
)

func validateNavigationURL(rawURL string, allowedHosts []string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme %q; only http and https are allowed", parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("URL host is required")
	}
	if len(allowedHosts) > 0 {
		for _, h := range allowedHosts {
			if h == "*" || h == host {
				return parsed.String(), nil
			}
		}
		return "", fmt.Errorf("host %q not in browser allowed hosts", host)
	}
	if core.IsPrivateIP(host) {
		return "", fmt.Errorf("navigation to private/internal address %q is blocked", host)
	}
	return parsed.String(), nil
}
