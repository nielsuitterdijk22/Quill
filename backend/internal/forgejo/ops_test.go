package forgejo_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *forgejo.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return forgejo.New(config.ForgejoConfig{BaseURL: srv.URL, AdminToken: "test-token"})
}

func TestGetOrCreateOrg_Existing(t *testing.T) {
	var posted bool
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme":
			_, _ = w.Write([]byte(`{"id":42,"username":"acme"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs":
			posted = true
			http.Error(w, "should not create", http.StatusInternalServerError)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})

	org, err := c.GetOrCreateOrg(context.Background(), forgejo.CreateOrgOptions{Name: "acme"})
	if err != nil {
		t.Fatalf("GetOrCreateOrg: %v", err)
	}
	if org.ID != 42 {
		t.Fatalf("org ID = %d, want 42", org.ID)
	}
	if posted {
		t.Fatal("created org that already existed")
	}
}

func TestGetOrCreateOrg_CreatesOn404(t *testing.T) {
	var posted bool
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme":
			http.Error(w, "GetOrgByName", http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs":
			posted = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":7,"username":"acme"}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})

	org, err := c.GetOrCreateOrg(context.Background(), forgejo.CreateOrgOptions{Name: "acme"})
	if err != nil {
		t.Fatalf("GetOrCreateOrg: %v", err)
	}
	if !posted {
		t.Fatal("did not create the missing org")
	}
	if org.ID != 7 {
		t.Fatalf("org ID = %d, want 7", org.ID)
	}
}

func TestGetOrCreateOrg_CreateRace(t *testing.T) {
	// GET 404, then POST loses a race (409/422), then GET now succeeds.
	var gets int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme":
			gets++
			if gets == 1 {
				http.Error(w, "GetOrgByName", http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"id":99,"username":"acme"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs":
			http.Error(w, "org already exists", http.StatusUnprocessableEntity)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})

	org, err := c.GetOrCreateOrg(context.Background(), forgejo.CreateOrgOptions{Name: "acme"})
	if err != nil {
		t.Fatalf("GetOrCreateOrg: %v", err)
	}
	if org.ID != 99 {
		t.Fatalf("org ID = %d, want 99 (re-fetched after create race)", org.ID)
	}
}

func TestGetOrCreateOrg_PropagatesNon404(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			t.Error("must not create org when GET fails with a non-404")
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	if _, err := c.GetOrCreateOrg(context.Background(), forgejo.CreateOrgOptions{Name: "acme"}); err == nil {
		t.Fatal("expected error to propagate on 500")
	}
}
