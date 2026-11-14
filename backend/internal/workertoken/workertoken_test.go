package workertoken

import (
	"strings"
	"testing"
	"time"
)

func TestMintVerify(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tok, err := Mint("s3cret", Claims{Worker: "ciw-1", Instance: "inst-1", Expires: now.Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	c, err := Verify("s3cret", tok, now)
	if err != nil || c.Worker != "ciw-1" || c.Instance != "inst-1" {
		t.Fatalf("verify: %+v %v", c, err)
	}
	if _, err := Verify("other", tok, now); err == nil {
		t.Fatal("verified under the wrong secret")
	}
	if _, err := Verify("s3cret", tok, now.Add(2*time.Hour)); err == nil {
		t.Fatal("expired token verified")
	}
	// Tampered claims (another worker) with the old signature.
	forged, _ := Mint("attacker-guess", Claims{Worker: "ciw-2", Instance: "inst-1", Expires: now.Add(time.Hour).Unix()})
	payload := strings.Split(forged, ".")[1]
	sig := strings.Split(tok, ".")[2]
	if _, err := Verify("s3cret", Prefix+payload+"."+sig, now); err == nil {
		t.Fatal("tampered token verified")
	}
	if _, err := Verify("s3cret", "qcw1.garbage", now); err == nil {
		t.Fatal("garbage verified")
	}
	if _, err := Verify("", tok, now); err == nil {
		t.Fatal("empty secret verified")
	}
	if p, err := Peek(tok); err != nil || p.Worker != "ciw-1" {
		t.Fatalf("peek: %+v %v", p, err)
	}
}
