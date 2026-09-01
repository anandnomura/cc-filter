// Package resourceindicator validates the strict BAP profile of the OAuth 2.0
// resource parameter defined by RFC 8707.
package resourceindicator

import (
	"errors"
	"net/url"
	"strings"
)

// Validate requires one canonical HTTPS resource URI. BAP deliberately uses a
// stricter profile than RFC 8707: query and fragment components are forbidden
// so a resource has one stable audience identity.
func Validate(value string) error {
	if value == "" || strings.TrimSpace(value) != value {
		return errors.New("resource must be a non-empty absolute URI")
	}
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.Opaque != "" || parsed.User != nil {
		return errors.New("resource must be an absolute HTTPS URI")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("resource must not contain a query or fragment")
	}
	return nil
}
