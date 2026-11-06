package config

import "testing"

func TestLoad_TerraformPlaceholderMeansUnconfigured(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "unset")
	t.Setenv("GITHUB_CLIENT_SECRET", "unset")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GitHub.ClientID != "" || cfg.GitHub.ClientSecret != "" {
		t.Fatalf("placeholder creds must normalize to empty, got %+v", cfg.GitHub)
	}
}

func TestLoad_DummyGitHubCredsMeanUnconfigured(t *testing.T) {
	// "x" is what a tfvars placeholder actually deployed once; it must not
	// reach github.com as a client_id.
	for _, dummy := range []string{"x", "X", " changeme ", "placeholder"} {
		t.Setenv("GITHUB_CLIENT_ID", dummy)
		t.Setenv("GITHUB_CLIENT_SECRET", dummy)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load(%q): %v", dummy, err)
		}
		if cfg.GitHub.ClientID != "" || cfg.GitHub.ClientSecret != "" {
			t.Fatalf("dummy creds %q must normalize to empty, got %+v", dummy, cfg.GitHub)
		}
	}
}

func TestLoad_KeepsRealGitHubCreds(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "Iv1.abc123")
	t.Setenv("GITHUB_CLIENT_SECRET", "secret-value")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GitHub.ClientID != "Iv1.abc123" || cfg.GitHub.ClientSecret != "secret-value" {
		t.Fatalf("real creds must pass through, got %+v", cfg.GitHub)
	}
}
