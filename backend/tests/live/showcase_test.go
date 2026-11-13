package live

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPipelineShowcase (QUILL_SHOWCASE=1) builds and exercises the public
// pipeline showcase on the live platform, entirely through the own git engine:
//
//   - pipeline-templates: a cross-repo reusable workflow
//   - pipeline-showcase:  ci (local reusable + cross-repo template + docker
//     build with needs:), secrets (masked secret), deploy (docker build → push
//     to the Scaleway registry → deploy a serverless container → curl it)
//
// Idempotent: re-running pushes a fresh commit and re-verifies. The Scaleway
// deploy credentials arrive via QUILL_SHOWCASE_SCW_* (operator env, from
// terraform `showcase` outputs) and are stored ONLY as the repo's secrets.
func TestPipelineShowcase(t *testing.T) {
	requireLive(t)
	if os.Getenv("QUILL_SHOWCASE") != "1" {
		t.Skip("QUILL_SHOWCASE=1 not set")
	}
	deployEnv := map[string]string{
		"SCW_ACCESS_KEY":              os.Getenv("QUILL_SHOWCASE_SCW_ACCESS_KEY"),
		"SCW_SECRET_KEY":              os.Getenv("QUILL_SHOWCASE_SCW_SECRET_KEY"),
		"SCW_DEFAULT_PROJECT_ID":      os.Getenv("QUILL_SHOWCASE_SCW_PROJECT_ID"),
		"SCW_DEFAULT_ORGANIZATION_ID": os.Getenv("QUILL_SHOWCASE_SCW_ORGANIZATION_ID"),
		"SCW_CONTAINER_NAMESPACE_ID":  os.Getenv("QUILL_SHOWCASE_SCW_CONTAINER_NS"),
	}
	for k, v := range deployEnv {
		if v == "" {
			t.Fatalf("QUILL_SHOWCASE_* env missing for %s", k)
		}
	}

	token := login(t)
	if st, b := apiCall(t, token, http.MethodPost, "/me/personal-project", nil); st != 200 && st != 409 {
		t.Fatalf("personal-project: %d %s", st, b)
	}
	_, meB := apiCall(t, token, http.MethodGet, "/auth/me", nil)
	username, _ := jbody(t, meB)["username"].(string)
	proj := username // personal project slug == username (ci.yml references it)

	ensureRepo := func(slug string) {
		st, b := apiCall(t, token, http.MethodPost, "/projects/"+proj+"/repos", map[string]any{
			"slug": slug, "name": slug, "visibility": "public",
			"description": "Quill pipeline showcase (" + slug + ")",
		})
		if st != 200 && st != 201 && st != 409 {
			t.Fatalf("create repo %s: %d %s", slug, st, b)
		}
	}
	ensureRepo("pipeline-templates")
	ensureRepo("pipeline-showcase")

	demo := make([]byte, 12)
	_, _ = rand.Read(demo)
	demoSecret := "showcase-" + hex.EncodeToString(demo)
	secrets := map[string]string{"DEMO_SECRET": demoSecret}
	for k, v := range deployEnv {
		secrets[k] = v
	}
	for name, value := range secrets {
		st, b := apiCall(t, token, http.MethodPut, "/projects/"+proj+"/repos/pipeline-showcase/secrets/"+name, map[string]any{"value": value})
		if st != 200 && st != 201 && st != 204 {
			t.Fatalf("set secret %s: %d %s", name, st, redactTokens(string(b)))
		}
	}

	st, b := apiCall(t, token, http.MethodPost, "/me/git-token", map[string]any{"name": "showcase"})
	if st != 200 && st != 201 {
		t.Fatalf("git token: %d %s", st, b)
	}
	tok, _ := jbody(t, b)["token"].(string)

	stamp := time.Now().UTC().Format(time.RFC3339)
	push := func(repo, srcDir string) string {
		dir := t.TempDir()
		sh(t, dir, "git", "clone", "-q", gitCloneURL(t, username, tok, proj, repo), "r")
		rd := filepath.Join(dir, "r")
		sh(t, rd, "git", "config", "user.email", "showcase@black-tulip.nl")
		sh(t, rd, "git", "config", "user.name", "quill showcase")
		sh(t, rd, "git", "checkout", "-B", "main")
		if err := copyTree(srcDir, rd); err != nil {
			t.Fatalf("copy %s: %v", srcDir, err)
		}
		_ = os.WriteFile(filepath.Join(rd, ".showcase-stamp"), []byte(stamp+"\n"), 0o644)
		sh(t, rd, "git", "add", "-A")
		sh(t, rd, "git", "commit", "-qm", "showcase "+stamp)
		sh(t, rd, "git", "push", "-q", "origin", "main")
		return strings.TrimSpace(sh(t, rd, "git", "rev-parse", "HEAD"))
	}
	push("pipeline-templates", "showcase/templates")
	sha := push("pipeline-showcase", "showcase/app")
	fmt.Printf("SHOWCASE pushed %s; https://app.black-tulip.nl/%s/pipeline-showcase/pipelines\n", sha, proj)

	// Every workflow listening on push must run for this commit and succeed.
	want := map[string]bool{".github/workflows/ci.yml": true, ".github/workflows/secrets.yml": true, ".github/workflows/deploy.yml": true}
	rp := "/projects/" + proj + "/repos/pipeline-showcase/pipelines"
	type runInfo struct {
		num    int
		status string
	}
	runs := map[string]runInfo{}
	deadline := time.Now().Add(25 * time.Minute)
	for time.Now().Before(deadline) {
		st, b := apiCall(t, token, http.MethodGet, rp+"/runs", nil)
		if st == 200 {
			for _, r := range jbody(t, b)["runs"].([]any) {
				m := r.(map[string]any)
				wf, _ := m["workflowPath"].(string)
				if m["commitSha"] != sha || !want[wf] {
					continue
				}
				n, _ := m["runNumber"].(float64)
				s, _ := m["status"].(string)
				if prev, ok := runs[wf]; !ok || prev.status != s {
					fmt.Printf("SHOWCASE %s #%d %s\n", wf, int(n), s)
				}
				runs[wf] = runInfo{int(n), s}
			}
		}
		done := len(runs) == len(want)
		for _, r := range runs {
			if r.status != "success" && r.status != "failure" && r.status != "failed" && r.status != "cancelled" {
				done = false
			}
		}
		if done {
			break
		}
		time.Sleep(15 * time.Second)
	}

	logs := func(wf string, num int) string {
		st, b := apiCall(t, token, http.MethodGet, fmt.Sprintf("%s/runs/%d/logs?workflow=%s", rp, num, wf), nil)
		if st != 200 {
			t.Errorf("logs %s #%d: %d", wf, num, st)
		}
		return string(b)
	}
	for wf := range want {
		r, ok := runs[wf]
		if !ok {
			t.Errorf("%s never ran for %s", wf, sha)
			continue
		}
		l := logs(wf, r.num)
		if r.status != "success" {
			tail := l
			if len(tail) > 3000 {
				tail = tail[len(tail)-3000:]
			}
			t.Errorf("%s #%d ended %s; log tail:\n%s", wf, r.num, r.status, strings.ReplaceAll(tail, demoSecret, "<LEAKED>"))
			continue
		}
		switch wf {
		case ".github/workflows/secrets.yml":
			if strings.Contains(l, demoSecret) {
				t.Errorf("secret leaked in plain text in the secrets run log")
			}
			if !strings.Contains(l, "***") {
				t.Errorf("secrets run log shows no masked value")
			}
		case ".github/workflows/ci.yml":
			for _, s := range []string{"running the unit suite", "shared lint template", "<h1>Quill pipeline showcase</h1>"} {
				if !strings.Contains(l, s) {
					t.Errorf("ci log missing %q", s)
				}
			}
		case ".github/workflows/deploy.yml":
			if i := strings.Index(l, "LIVE: "); i >= 0 {
				end := strings.Index(l[i:], "\\n")
				if end < 0 {
					end = len(l) - i
				}
				fmt.Printf("SHOWCASE %s\n", l[i:i+end])
			} else {
				t.Errorf("deploy log has no LIVE: line")
			}
		}
	}
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}
