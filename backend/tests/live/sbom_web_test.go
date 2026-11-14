package live

import (
	"encoding/json"
	"net/http"
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
	if !strings.Contains(string(page), "sbom-table") {
		t.Errorf("sbom viewer page does not render the component table (len=%d)", len(page))
	}
}
