// Package secretbox provides authenticated encryption for values Quill stores at
// rest, such as pipeline secrets. It wraps AES-256-GCM behind a small Cipher
// type so callers never touch nonces or key handling directly.
//
// Ciphertext and nonce are stored separately (the DB columns mirror that split).
// A fresh random nonce is generated per Seal, so encrypting the same plaintext
// twice yields different ciphertext — callers must persist the returned nonce
// alongside the ciphertext and pass both back to Open.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// devKey is used only when no encryption key is configured (development). The
// config layer requires a real QUILL_SECRET_ENCRYPTION_KEY in production, so this
// insecure fallback never applies there. It mirrors auth.devSecret.
const devKey = "quill-dev-insecure-secret-encryption-key-change-me"

// ErrKeyMissing is returned by NewFromBase64Key when no key material is given.
var ErrKeyMissing = errors.New("secretbox: encryption key is required")

// Cipher seals and opens values with a fixed AES-256-GCM key.
type Cipher struct {
	aead cipher.AEAD
}

// New constructs a Cipher from a 32-byte key.
func New(key [32]byte) (*Cipher, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("secretbox: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretbox: new gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// NewFromBase64Key derives a Cipher from a base64-encoded key. Any key material
// is accepted and folded to 32 bytes via SHA-256, so operators may supply a
// standard 32-byte key or a longer passphrase. An empty key returns
// ErrKeyMissing so the caller can decide whether to fall back to the dev key.
func NewFromBase64Key(b64 string) (*Cipher, error) {
	if b64 == "" {
		return nil, ErrKeyMissing
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		// Not valid base64 — accept the raw string bytes as key material rather
		// than failing, so a plain passphrase also works.
		raw = []byte(b64)
	}
	return New(sha256.Sum256(raw))
}

// NewDev returns a Cipher backed by the insecure development key. Never use in
// production; the config layer gates this behind a non-production environment.
func NewDev() *Cipher {
	c, err := New(sha256.Sum256([]byte(devKey)))
	if err != nil {
		// The dev key is a compile-time constant of valid length; New cannot fail.
		panic(err)
	}
	return c
}

// Seal encrypts plaintext, returning the ciphertext and the random nonce used.
// Both must be stored; Open needs the same nonce.
func (c *Cipher) Seal(plaintext []byte) (ciphertext, nonce []byte, err error) {
	nonce = make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("secretbox: read nonce: %w", err)
	}
	ciphertext = c.aead.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

// Open decrypts ciphertext produced by Seal with the matching nonce. It returns
// an error when the key is wrong or the ciphertext was tampered with.
func (c *Cipher) Open(ciphertext, nonce []byte) ([]byte, error) {
	if len(nonce) != c.aead.NonceSize() {
		return nil, fmt.Errorf("secretbox: nonce is %d bytes, want %d", len(nonce), c.aead.NonceSize())
	}
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("secretbox: open: %w", err)
	}
	return plaintext, nil
}
