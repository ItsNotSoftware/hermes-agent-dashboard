// Package util holds tiny helpers shared across the dashboard.
package util

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// DecodeJWTClaims base64url-decodes the payload segment of a JWT. It does
// not verify the signature; the dashboard only needs to read `exp`.
func DecodeJWTClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, errors.New("invalid jwt")
	}
	payload := parts[1]
	if pad := len(payload) % 4; pad != 0 {
		payload += strings.Repeat("=", 4-pad)
	}
	raw, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}
