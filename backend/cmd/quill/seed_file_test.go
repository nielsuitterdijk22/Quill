package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// seedForgejo is an in-memory double for seedForgejoLike. It records the file
// writes it receives and answers the repository/contents lookups from a small
// fixture, so seedFile can be exercised without a live Forgejo.
type seedForgejo struct {
	enabled   bool
	repos     map[string]forgejo.Repo // key "owner/name"
	files     map[string]forgejo.ContentEntry
	writeLog  []string
	writeBody map[string][]byte
}

func newSeedForgejo() *seedForgejo {
	return &seedForgejo{
		enabled:   true,
		repos:     map[string]forgejo.Repo{},
		files:     map[string]forgejo.ContentEntry{},
		writeBody: map[string][]byte{},
	}
}

func (f *seedForgejo) seedRepo(owner, name string) forgejo.Repo {
	r := forgejo.Repo{ID: 7, Name: name, FullName: owner + "/" + name, DefaultBranch: "main", HTMLURL: "http://forge.example/" + owner + "/" + name}
	f.repos[owner+"/"+name] = r
	return r
}

func (f *seedForgejo) seedFile(owner, name, path, sha string) {
	f.files[owner+"/"+name+"/"+path] = forgejo.ContentEntry{Name: path, Path: path, SHA: sha, Type: "file"}
}

func (f *seedForgejo) Enabled() bool { return f.enabled }

func (f *seedForgejo) GetRepo(ctx context.Context, owner, name string) (forgejo.Repo, error) {
	r, ok := f.repos[owner+"/"+name]
	if !ok {
		return forgejo.Repo{}, &forgejo.APIError{Status: http.StatusNotFound, Method: "GET", Path: "/repos/" + owner + "/" + name, Message: "not found"}
	}
	return r, nil
}

func (f *seedForgejo) GetOrCreateOrg(ctx context.Context, opts forgejo.CreateOrgOptions) (forgejo.Org, error) {
	return forgejo.Org{Name: opts.Name, FullName: opts.FullName}, nil
}

func (f *seedForgejo) CreateOrgRepo(ctx context.Context, org string, opts forgejo.CreateRepoOptions) (forgejo.Repo, error) {
	r := forgejo.Repo{ID: 8, Name: opts.Name, FullName: org + "/" + opts.Name, DefaultBranch: "main", HTMLURL: "http://forge.example/" + org + "/" + opts.Name}
	f.repos[org+"/"+opts.Name] = r
	return r, nil
}

func (f *seedForgejo) SeedFile(ctx context.Context, owner, repo, path, message, branch string, content []byte) (string, error) {
	key := owner + "/" + repo + "/" + path
	existing, ok := f.files[key]
	sha := existing.SHA
	if !ok || existing.Type != "file" {
		sha = "newsha"
	}
	f.files[key] = forgejo.ContentEntry{Name: path, Path: path, SHA: sha, Type: "file"}
	f.writeLog = append(f.writeLog, message)
	f.writeBody[key] = content
	return sha, nil
}

var _ seedForgejoLike = (*seedForgejo)(nil)

// newSeedTestForgejo builds a real *forgejo.Client pointed at an httptest server
// that emulates the contents endpoint, so the client's own create/update logic
// (POST vs PUT, base64, sha) is exercised.
func newSeedTestForgejo(t *testing.T, existing bool, fileSHA string) (*forgejo.Client, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/repos/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/api/v1/repos/")
		switch r.Method {
		case http.MethodGet:
			// contents lookup
			if strings.Contains(p, "/contents/") {
				if existing {
					enc := "base64"
					fmt.Fprintf(w, `{"name":"README.md","path":"README.md","sha":%q,"type":"file","size":5,"encoding":%q,"content":%q}`, fileSHA, enc, base64.StdEncoding.EncodeToString([]byte("old")))
					return
				}
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"message":"not found"}`)
				return
			}
			// repo lookup
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"id":7,"name":"suite-meta","full_name":"acme/suite-meta","default_branch":"main","html_url":"http://forge.example/acme/suite-meta"}`)
		case http.MethodPut:
			var body struct {
				Message string `json:"message"`
				Content string `json:"content"`
				SHA     string `json:"sha"`
				Branch  string `json:"branch"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"name":"README.md","path":"README.md","sha":"updated-%s","type":"file"}`, body.SHA)
		case http.MethodPost:
			var body struct {
				Message string `json:"message"`
				Content string `json:"content"`
				Branch  string `json:"branch"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"name":"README.md","path":"README.md","sha":"created","type":"file"}`)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cfg := config.ForgejoConfig{BaseURL: srv.URL, AdminToken: "tok", PublicURL: "http://forge.example"}
	return forgejo.New(cfg), srv
}

func TestSeedFileCreatesMissingFile(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	tenant := st.seedTenant("acme", "Acme Corp")
	st.seedProject(tenant.ID, "acme", "acme")
	st.seedRepoRow(tenant.ID, "suite-meta")

	fj := newSeedForgejo()
	fj.seedRepo("acme", "suite-meta")

	url, err := seedFile(ctx, st, fj, "http://forge.example", seedFileInput{
		TenantRef: "acme", Repo: "suite-meta", Path: "README.md", Content: []byte("Hello"),
	})
	if err != nil {
		t.Fatalf("seedFile: %v", err)
	}
	if url != "http://forge.example/acme/suite-meta" {
		t.Fatalf("url = %q, want the repo URL", url)
	}
	if len(fj.writeLog) != 1 || !strings.Contains(fj.writeLog[0], "README.md") || !strings.Contains(fj.writeLog[0], "admin seed-file") {
		t.Fatalf("commit message = %q, want it to mention the path and admin seed-file", fj.writeLog)
	}
	if got := string(fj.writeBody["acme/suite-meta/README.md"]); got != "Hello" {
		t.Fatalf("content = %q, want %q", got, "Hello")
	}
}

func TestSeedFileUpdatesExistingFile(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	tenant := st.seedTenant("acme", "Acme Corp")
	st.seedProject(tenant.ID, "acme", "acme")
	st.seedRepoRow(tenant.ID, "suite-meta")

	fj := newSeedForgejo()
	fj.seedRepo("acme", "suite-meta")
	fj.seedFile("acme", "suite-meta", "README.md", "oldsha")

	if _, err := seedFile(ctx, st, fj, "http://forge.example", seedFileInput{
		TenantRef: tenant.ID.String(), Repo: "suite-meta", Path: "README.md", Content: []byte("v2"),
	}); err != nil {
		t.Fatalf("seedFile: %v", err)
	}
	if got := fj.files["acme/suite-meta/README.md"].SHA; got != "oldsha" {
		t.Fatalf("sha = %q, want the existing sha retained for the update", got)
	}
	if got := string(fj.writeBody["acme/suite-meta/README.md"]); got != "v2" {
		t.Fatalf("content = %q, want %q", got, "v2")
	}
}

func TestSeedFileCreatesRepoWhenMissing(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	tenant := st.seedTenant("acme", "Acme Corp")
	st.seedProject(tenant.ID, "acme", "acme")
	// no repo row and no forgejo repo

	fj := newSeedForgejo()
	// no repo seeded -> GetRepo 404s

	if _, err := seedFile(ctx, st, fj, "http://forge.example", seedFileInput{
		TenantRef: "acme", Repo: "suite-meta", Path: "README.md", Content: []byte("Hello"),
	}); err != nil {
		t.Fatalf("seedFile: %v", err)
	}
	if _, ok := fj.repos["acme/suite-meta"]; !ok {
		t.Fatal("expected the repository to be created in Forgejo")
	}
}

func TestSeedFileUsesProjectOrgName(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	tenant := st.seedTenant("acme", "Acme Corp")
	st.seedProject(tenant.ID, "acme", "acme-oss")
	st.seedRepoRow(tenant.ID, "suite-meta")

	fj := newSeedForgejo()
	fj.seedRepo("acme-oss", "suite-meta")

	url, err := seedFile(ctx, st, fj, "http://forge.example", seedFileInput{
		TenantRef: "acme", Repo: "suite-meta", Path: "README.md", Content: []byte("Hello"),
	})
	if err != nil {
		t.Fatalf("seedFile: %v", err)
	}
	if url != "http://forge.example/acme-oss/suite-meta" {
		t.Fatalf("url = %q, want the project org name to be used", url)
	}
}

func TestSeedFileValidation(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	tenant := st.seedTenant("acme", "Acme Corp")
	st.seedProject(tenant.ID, "acme", "acme")
	fj := newSeedForgejo()

	if _, err := seedFile(ctx, st, fj, "", seedFileInput{TenantRef: "nope", Repo: "suite-meta", Path: "README.md", Content: []byte("x")}); err == nil {
		t.Fatal("expected error for unknown tenant")
	}
	if _, err := seedFile(ctx, st, fj, "", seedFileInput{TenantRef: "acme", Repo: "Bad Name!", Path: "README.md", Content: []byte("x")}); err == nil {
		t.Fatal("expected error for invalid repo name")
	}
	if _, err := seedFile(ctx, st, fj, "", seedFileInput{TenantRef: "acme", Repo: "suite-meta", Path: "  ", Content: []byte("x")}); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestSeedFileForgejoDisabled(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	tenant := st.seedTenant("acme", "Acme Corp")
	st.seedProject(tenant.ID, "acme", "acme")
	fj := newSeedForgejo()
	fj.enabled = false
	if _, err := seedFile(ctx, st, fj, "", seedFileInput{TenantRef: "acme", Repo: "suite-meta", Path: "README.md", Content: []byte("x")}); err == nil {
		t.Fatal("expected error when forgejo is not configured")
	}
}

// TestSeedFileClientCreateAndUpdate drives the real *forgejo.Client.SeedFile
// against a mock contents endpoint, asserting the POST/PUT split and base64.
func TestSeedFileClientCreateAndUpdate(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	tenant := st.seedTenant("acme", "Acme Corp")
	st.seedProject(tenant.ID, "acme", "acme")
	st.seedRepoRow(tenant.ID, "suite-meta")

	fj, _ := newSeedTestForgejo(t, false, "")
	sha, err := fj.SeedFile(ctx, "acme", "suite-meta", "README.md", "Seed README.md (admin seed-file)", "main", []byte("Hello"))
	if err != nil {
		t.Fatalf("SeedFile (create): %v", err)
	}
	if sha != "created" {
		t.Fatalf("sha = %q, want %q", sha, "created")
	}

	fj2, _ := newSeedTestForgejo(t, true, "oldsha")
	sha, err = fj2.SeedFile(ctx, "acme", "suite-meta", "README.md", "Seed README.md (admin seed-file)", "main", []byte("v2"))
	if err != nil {
		t.Fatalf("SeedFile (update): %v", err)
	}
	if sha != "updated-oldsha" {
		t.Fatalf("sha = %q, want the update to echo the existing sha", sha)
	}
}

// seedRepoRow records a repository row in the fake store.
func (f *fakeStore) seedRepoRow(projectID uuid.UUID, slug string) db.Repository {
	r := db.Repository{ID: uuid.New(), ProjectID: projectID, Slug: slug, Name: slug, Visibility: "private", DefaultBranch: "main"}
	f.repos[r.ID] = r
	return r
}
