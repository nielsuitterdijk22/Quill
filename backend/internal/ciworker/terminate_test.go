package ciworker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Scaleway's metadata service is 169.254.42.42/conf (NOT the AWS-style
// 169.254.169.254 — the first live run never found its own id, fell through
// to cloud-init's OS shutdown and left the box "stopped in place").
func TestOwnInstanceIDFromScalewayMetadata(t *testing.T) {
	if metadataBase != "http://169.254.42.42" {
		t.Fatalf("metadataBase = %q, want Scaleway's http://169.254.42.42", metadataBase)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/conf" || r.URL.Query().Get("format") != "json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"id":"0f8e-instance","name":"quill-ci-worker-1","tags":["quill-ci-worker"]}`))
	}))
	defer srv.Close()
	old := metadataBase
	metadataBase = srv.URL
	defer func() { metadataBase = old }()
	t.Setenv("SCW_INSTANCE_ID", "")

	id, err := OwnInstanceID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if id != "0f8e-instance" {
		t.Fatalf("id = %q", id)
	}
}
