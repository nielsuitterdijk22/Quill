package secretbox

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	c, err := New(sha256.Sum256([]byte("test-key")))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	plaintext := []byte("s3cr3t-value\nwith newline")
	ciphertext, nonce, err := c.Seal(plaintext)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if bytes.Contains(ciphertext, plaintext) {
		t.Fatal("ciphertext leaks plaintext")
	}
	got, err := c.Open(ciphertext, nonce)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round trip mismatch: got %q want %q", got, plaintext)
	}
}

func TestSealUsesFreshNonce(t *testing.T) {
	c := NewDev()
	_, n1, err := c.Seal([]byte("x"))
	if err != nil {
		t.Fatalf("seal 1: %v", err)
	}
	_, n2, err := c.Seal([]byte("x"))
	if err != nil {
		t.Fatalf("seal 2: %v", err)
	}
	if bytes.Equal(n1, n2) {
		t.Fatal("nonce reused across seals")
	}
}

func TestOpenRejectsWrongKey(t *testing.T) {
	a, _ := New(sha256.Sum256([]byte("key-a")))
	b, _ := New(sha256.Sum256([]byte("key-b")))
	ciphertext, nonce, err := a.Seal([]byte("value"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := b.Open(ciphertext, nonce); err == nil {
		t.Fatal("open with wrong key should fail")
	}
}

func TestOpenRejectsTamperedCiphertext(t *testing.T) {
	c := NewDev()
	ciphertext, nonce, err := c.Seal([]byte("value"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	ciphertext[0] ^= 0xff
	if _, err := c.Open(ciphertext, nonce); err == nil {
		t.Fatal("open of tampered ciphertext should fail")
	}
}

func TestNewFromBase64Key(t *testing.T) {
	if _, err := NewFromBase64Key(""); err != ErrKeyMissing {
		t.Fatalf("empty key: want ErrKeyMissing, got %v", err)
	}
	// A non-base64 passphrase is accepted as raw key material.
	if _, err := NewFromBase64Key("a plain passphrase"); err != nil {
		t.Fatalf("passphrase key: %v", err)
	}
	// A valid base64 key is accepted.
	if _, err := NewFromBase64Key("dGVzdC0zMi1ieXRlLWtleS1tYXRlcmlhbC1oZXJlIQ=="); err != nil {
		t.Fatalf("base64 key: %v", err)
	}
}
