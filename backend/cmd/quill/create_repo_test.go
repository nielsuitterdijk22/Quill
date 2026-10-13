package main

import (
	"context"
	"errors"
	"fmt"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// fakeStore is an in-memory storeLike for tests.
type fakeStore struct {
	tenants     map[uuid.UUID]db.Tenant
	tenantSlugs map[string]db.Tenant
	projects    map[uuid.UUID]db.Project
	repos       map[uuid.UUID]db.Repository
	nextRepoID  int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		tenants:     map[uuid.UUID]db.Tenant{},
		tenantSlugs: map[string]db.Tenant{},
		projects:    map[uuid.UUID]db.Project{},
		repos:       map[uuid.UUID]db.Repository{},
	}
}

func (f *fakeStore) seedTenant(slug, name string) db.Tenant {
	t := db.Tenant{ID: uuid.New(), Slug: slug, Name: name, Plan: "free"}
	f.tenants[t.ID] = t
	f.tenantSlugs[slug] = t
	return t
}

func (f *fakeStore) seedProject(tenantID uuid.UUID, slug, orgName string) db.Project {
	p := db.Project{ID: uuid.New(), TenantID: tenantID, Slug: slug, Name: slug, ForgejoOrgName: pgtype.Text{String: orgName, Valid: orgName != ""}}
	f.projects[p.ID] = p
	return p
}

func (f *fakeStore) GetTenantByID(ctx context.Context, id uuid.UUID) (db.Tenant, error) {
	t, ok := f.tenants[id]
	if !ok {
		return db.Tenant{}, pgx.ErrNoRows
	}
	return t, nil
}

func (f *fakeStore) GetTenantBySlug(ctx context.Context, lower string) (db.Tenant, error) {
	t, ok := f.tenantSlugs[lower]
	if !ok {
		return db.Tenant{}, pgx.ErrNoRows
	}
	return t, nil
}

func (f *fakeStore) GetRepositoryBySlug(ctx context.Context, arg db.GetRepositoryBySlugParams) (db.Repository, error) {
	for _, r := range f.repos {
		if r.ProjectID == arg.ProjectID && strings.EqualFold(r.Slug, arg.Lower) {
			return r, nil
		}
	}
	return db.Repository{}, pgx.ErrNoRows
}

func (f *fakeStore) CreateRepository(ctx context.Context, arg db.CreateRepositoryParams) (db.Repository, error) {
	r := db.Repository{
		ID: uuid.New(), ProjectID: arg.ProjectID, Slug: arg.Slug, Name: arg.Name,
		Description: arg.Description, Visibility: arg.Visibility, DefaultBranch: arg.DefaultBranch,
	}
	f.repos[r.ID] = r
	return r, nil
}

func (f *fakeStore) UpdateRepository(ctx context.Context, arg db.UpdateRepositoryParams) (db.Repository, error) {
	r, ok := f.repos[arg.ID]
	if !ok {
		return db.Repository{}, pgx.ErrNoRows
	}
	r.Slug, r.Name, r.Description, r.Visibility, r.DefaultBranch, r.IsArchived = arg.Slug, arg.Name, arg.Description, arg.Visibility, arg.DefaultBranch, arg.IsArchived
	r.ForgejoName = arg.ForgejoName
	f.repos[r.ID] = r
	return r, nil
}

func (f *fakeStore) SetRepositoryForgejoLink(ctx context.Context, arg db.SetRepositoryForgejoLinkParams) (db.Repository, error) {
	r, ok := f.repos[arg.ID]
	if !ok {
		return db.Repository{}, pgx.ErrNoRows
	}
	r.ForgejoRepoID, r.ForgejoOwner, r.ForgejoName = arg.ForgejoRepoID, arg.ForgejoOwner, arg.ForgejoName
	f.repos[r.ID] = r
	return r, nil
}

func (f *fakeStore) InTx(ctx context.Context, fn func(q dbQuerier) error) error {
	return fn(f)
}

func (f *fakeStore) ListProjectsByTenant(ctx context.Context, tenantID uuid.UUID) ([]db.Project, error) {
	var out []db.Project
	for _, p := range f.projects {
		if p.TenantID == tenantID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeStore) CreateProject(ctx context.Context, arg db.CreateProjectParams) (db.Project, error) {
	p := db.Project{ID: uuid.New(), TenantID: arg.TenantID, Slug: arg.Slug, Name: arg.Name, Description: arg.Description, IsPersonal: arg.IsPersonal}
	f.projects[p.ID] = p
	return p, nil
}

var _ storeLike = (*fakeStore)(nil)

// newTestForgejo spins up an httptest server that behaves like the slice of the
// Forgejo API createRepo uses, and returns a client pointed at it.
func newTestForgejo(t *testing.T, createErr error, htmlURL string) (*forgejo.Client, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orgs/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":1,"username":"acme"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"not found"}`)
	})
	mux.HandleFunc("/api/v1/orgs/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/repos") {
			if createErr != nil {
				w.WriteHeader(http.StatusConflict)
				fmt.Fprint(w, `{"message":"must be unique"}`)
				return
			}
			var body struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"id":42,"name":%q,"full_name":"org/%s","private":true,"default_branch":"main","html_url":%q}`, body.Name, body.Name, htmlURL)
			return
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":1,"username":"org"}`)
	})
	mux.HandleFunc("/api/v1/repos/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/repos/"), "/")
		name := "suite-meta"
		if len(parts) >= 2 {
			name = parts[1]
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"id":42,"name":%q,"full_name":"acme/%s","private":true,"default_branch":"main","html_url":%q}`, name, name, htmlURL)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cfg := config.ForgejoConfig{BaseURL: srv.URL, AdminToken: "tok", PublicURL: srv.URL}
	return forgejo.New(cfg), srv
}

func TestCreateRepoHappyPath(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	tenant := st.seedTenant("acme", "Acme Corp")
	st.seedProject(tenant.ID, "acme", "acme")

	fj, _ := newTestForgejo(t, nil, "")
	url, err := createRepo(ctx, st, fj, "http://forge.example", createRepoInput{
		TenantRef: "acme", Name: "Suite Meta", Visibility: "private",
	})
	if err != nil {
		t.Fatalf("createRepo: %v", err)
	}
	want := "http://forge.example/acme/suite-meta"
	if url != want {
		t.Fatalf("url = %q, want %q", url, want)
	}
	if len(st.repos) != 1 {
		t.Fatalf("expected 1 repository row, got %d", len(st.repos))
	}
	for _, r := range st.repos {
		if r.Slug != "suite-meta" || r.Visibility != "private" || !r.ForgejoName.Valid || r.ForgejoName.String != "suite-meta" {
			t.Fatalf("unexpected repository row: %+v", r)
		}
	}
}

func TestCreateRepoUsesProjectOrgName(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	tenant := st.seedTenant("acme", "Acme Corp")
	st.seedProject(tenant.ID, "acme", "acme-oss")

	fj, _ := newTestForgejo(t, nil, "")
	url, err := createRepo(ctx, st, fj, "http://forge.example", createRepoInput{
		TenantRef: tenant.ID.String(), Name: "suite-meta", Visibility: "public",
	})
	if err != nil {
		t.Fatalf("createRepo: %v", err)
	}
	if url != "http://forge.example/acme-oss/suite-meta" {
		t.Fatalf("url = %q, want org-name based URL", url)
	}
}

func TestCreateRepoIdempotentOnConflict(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	tenant := st.seedTenant("acme", "Acme Corp")
	st.seedProject(tenant.ID, "acme", "acme")

	conflict := &forgejo.APIError{Status: http.StatusConflict, Method: "POST", Path: "/api/v1/orgs/acme/repos", Message: "must be unique"}
	fj, _ := newTestForgejo(t, conflict, "http://forge.example/acme/suite-meta")
	url, err := createRepo(ctx, st, fj, "http://forge.example", createRepoInput{
		TenantRef: "acme", Name: "suite-meta", Visibility: "private",
	})
	if err != nil {
		t.Fatalf("createRepo: %v", err)
	}
	if url != "http://forge.example/acme/suite-meta" {
		t.Fatalf("url = %q, want existing html_url", url)
	}
}

func TestCreateRepoCreatesProjectWhenMissing(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	st.seedTenant("acme", "Acme Corp") // no project

	fj, _ := newTestForgejo(t, nil, "")
	if _, err := createRepo(ctx, st, fj, "http://forge.example", createRepoInput{
		TenantRef: "acme", Name: "suite-meta", Visibility: "private",
	}); err != nil {
		t.Fatalf("createRepo: %v", err)
	}
	if len(st.projects) != 1 {
		t.Fatalf("expected a project to be created, got %d", len(st.projects))
	}
}

func TestCreateRepoValidation(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	st.seedTenant("acme", "Acme Corp")
	fj, _ := newTestForgejo(t, nil, "")

	if _, err := createRepo(ctx, st, fj, "", createRepoInput{TenantRef: "acme", Name: "Bad Name!", Visibility: "private"}); err == nil {
		t.Fatal("expected error for invalid name")
	}
	if _, err := createRepo(ctx, st, fj, "", createRepoInput{TenantRef: "acme", Name: "ok", Visibility: "secret"}); err == nil {
		t.Fatal("expected error for invalid visibility")
	}
	if _, err := createRepo(ctx, st, fj, "", createRepoInput{TenantRef: "nope", Name: "ok", Visibility: "private"}); err == nil {
		t.Fatal("expected error for unknown tenant")
	}
}

func TestCreateRepoForgejoDisabled(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	st.seedTenant("acme", "Acme Corp")
	fj := forgejo.New(config.ForgejoConfig{}) // no base URL / token
	if _, err := createRepo(ctx, st, fj, "", createRepoInput{TenantRef: "acme", Name: "ok", Visibility: "private"}); err == nil {
		t.Fatal("expected error when forgejo is not configured")
	}
}

func TestIsAlreadyExists(t *testing.T) {
	if !isAlreadyExists(&forgejo.APIError{Status: 409, Message: "x"}) {
		t.Fatal("409 APIError should be already-exists")
	}
	if isAlreadyExists(errors.New("some other failure")) {
		t.Fatal("unrelated error should not be already-exists")
	}
	if isAlreadyExists(nil) {
		t.Fatal("nil should not be already-exists")
	}
}
