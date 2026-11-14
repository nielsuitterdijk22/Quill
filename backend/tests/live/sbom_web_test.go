package live

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestWebSBOM drives the repo landing page's SBOM button the way a browser
// does: signed-in app session, status through the /api/backend proxy, then
// the viewer page and the download link it renders.
func TestWebSBOM(t *testing.T) {
	requireLive(t)
	c := webSession(t)
	const proj, repo = "e2e-tester", "pipeline-showcase"
	st, _, b := get(t, c, appURL+"/api/backend/projects/"+proj+"/repos/"+repo+"/sbom", nil)
	if st != http.StatusOK {
		t.Fatalf("sbom status via proxy: %d %s", st, b)
	}
	var status struct {
		SBOM *struct {
			Status    string `json:"status"`
			CommitSHA string `json:"commitSha"`
		} `json:"sbom"`
	}
	if err := json.Unmarshal(b, &status); err != nil || status.SBOM == nil {
		t.Fatalf("sbom status body: %v %s", err, b)
	}
	if status.SBOM.Status != "ready" {
		t.Fatalf("sbom not ready: %s", status.SBOM.Status)
	}
	st, hdr, b := get(t, c, appURL+"/api/backend/projects/"+proj+"/repos/"+repo+"/sbom/download?sha="+status.SBOM.CommitSHA, nil)
	t.Logf("download: %d ct=%s cd=%s len=%d", st, hdr.Get("Content-Type"), hdr.Get("Content-Disposition"), len(b))
	if st != http.StatusOK || !strings.Contains(string(b), "CycloneDX") {
		t.Fatalf("download via proxy: %d %.200s", st, b)
	}
	st, _, page := get(t, c, appURL+"/"+proj+"/"+repo+"/sbom", nil)
	if st != http.StatusOK {
		t.Fatalf("sbom viewer page: %d", st)
	}
	for _, want := range []string{"sbom-table", "sbom-bar", "Browse code at", "License risk"} {
		if !strings.Contains(string(page), want) {
			t.Errorf("sbom viewer page lacks %q (len=%d)", want, len(page))
		}
	}

	// The same commit by id, and the commit page linking to it and its code.
	sha := status.SBOM.CommitSHA
	if st, _, page = get(t, c, appURL+"/"+proj+"/"+repo+"/sbom?sha="+sha, nil); st != http.StatusOK || !strings.Contains(string(page), "sbom-table") {
		t.Errorf("sbom viewer at ?sha=: %d", st)
	}
	st, _, page = get(t, c, appURL+"/"+proj+"/"+repo+"/commit/"+sha, nil)
	if st != http.StatusOK || !strings.Contains(string(page), "Browse files at") || !strings.Contains(string(page), "sbom?sha="+sha) {
		t.Errorf("commit page lacks browse/SBOM links: %d", st)
	}
	st, _, page = get(t, c, appURL+"/"+proj+"/"+repo+"/tree/"+sha, nil)
	if st != http.StatusOK || !strings.Contains(string(page), "commit-ref-banner") {
		t.Errorf("tree at a commit: %d (banner missing?)", st)
	}
	st, _, b = get(t, c, appURL+"/api/backend/projects/"+proj+"/repos/"+repo+"/sbom/history", nil)
	if st != http.StatusOK || !strings.Contains(string(b), sha) {
		t.Errorf("history: %d %.200s", st, b)
	}

	// Cross-repo search: empty query lists the most used packages; the
	// search page renders them.
	st, _, b = get(t, c, appURL+"/api/backend/sbom/search?q=", nil)
	var res struct {
		ReposSearched int `json:"reposSearched"`
		Packages      []struct {
			Name string `json:"name"`
		} `json:"packages"`
	}
	if st != http.StatusOK || json.Unmarshal(b, &res) != nil || res.ReposSearched < 1 {
		t.Fatalf("search: %d %.300s", st, b)
	}
	t.Logf("search: %d repos, %d packages", res.ReposSearched, len(res.Packages))
	if len(res.Packages) > 0 {
		name := res.Packages[0].Name
		st, _, page = get(t, c, appURL+"/sbom?q="+url.QueryEscape(name), nil)
		if st != http.StatusOK || !strings.Contains(string(page), "sbom-pkg") {
			t.Errorf("search page for %q: %d", name, st)
		}
	}
}
