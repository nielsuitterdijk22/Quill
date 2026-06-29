package auth

import "testing"

func TestSlugifyOrgDomain(t *testing.T) {
	cases := map[string]string{
		"acme.quill.local": "acme",
		"ACME.example.com": "acme",
		"single":           "single",
		"  spaced.io  ":    "spaced",
		"":                 "",
	}
	for in, want := range cases {
		if got := slugifyOrgDomain(in); got != want {
			t.Errorf("slugifyOrgDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestZitadelVerifierProviderAndEnabled(t *testing.T) {
	off := &ZitadelVerifier{}
	if off.Enabled() {
		t.Error("empty issuer should be disabled")
	}
	on := &ZitadelVerifier{issuer: "https://auth.example.com"}
	if !on.Enabled() {
		t.Error("configured issuer should be enabled")
	}
	if on.Provider() != ProviderZitadel {
		t.Errorf("Provider() = %q, want %q", on.Provider(), ProviderZitadel)
	}
}
