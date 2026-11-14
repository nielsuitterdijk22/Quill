// Package workertoken is the credential of one ephemeral CI instance
// (2026-11-14). The broker mints it when it boots an instance and hands it
// over in user-data instead of the platform-wide dispatch secret; the api
// verifies it. A token names exactly one worker and one instance and
// expires: anything that escapes a job container on that machine learns a
// credential scoped to the repository the worker serves (the api binds a
// worker to the repo of its first claim) — never the dispatch secret.
//
// Format: "qcw1." + base64url(JSON claims) + "." + base64url(HMAC-SHA256),
// keyed with the dispatch secret (shared by the api and the broker only).
package workertoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Prefix marks a worker token.
const Prefix = "qcw1."

// Claims is what a token asserts.
type Claims struct {
	Worker   string `json:"w"` // worker id (unique per instance)
	Instance string `json:"i"` // Scaleway instance id
	Expires  int64  `json:"e"` // unix seconds
}

// ErrInvalid is returned for malformed, forged or expired tokens.
var ErrInvalid = errors.New("invalid worker token")

func sign(secret string, payload string) string {
	mac := hmac.New(sha256.New, []byte("quill-worker-token\x00"+secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Mint signs claims.
func Mint(secret string, c Claims) (string, error) {
	if secret == "" || c.Worker == "" || c.Expires == 0 {
		return "", errors.New("workertoken: secret, worker and expiry are required")
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return Prefix + payload + "." + sign(secret, payload), nil
}

// Verify checks the signature and expiry and returns the claims.
func Verify(secret, token string, now time.Time) (Claims, error) {
	if secret == "" || !strings.HasPrefix(token, Prefix) {
		return Claims{}, ErrInvalid
	}
	payload, sig, ok := strings.Cut(strings.TrimPrefix(token, Prefix), ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(sign(secret, payload))) {
		return Claims{}, ErrInvalid
	}
	c, err := Peek(token)
	if err != nil || c.Worker == "" || now.Unix() >= c.Expires {
		return Claims{}, ErrInvalid
	}
	return c, nil
}

// Peek decodes the claims WITHOUT verifying them — for the worker, which
// only needs its own id. Never use it for authorization.
func Peek(token string) (Claims, error) {
	payload, _, ok := strings.Cut(strings.TrimPrefix(token, Prefix), ".")
	if !ok {
		return Claims{}, ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Claims{}, ErrInvalid
	}
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return Claims{}, ErrInvalid
	}
	return c, nil
}
