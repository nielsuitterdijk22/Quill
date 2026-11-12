package gitengine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SmartHTTP serves git's smart HTTP protocol (v0 and v2) — both handled by
// upstream git itself in stateless-rpc mode; the engine only frames
// pkg-lines and pipes bytes. The optional Receive config (B3) enables the
// push path: receive-pack advertisement + POST, with policy and truth
// committed in-handler. Auth/tenancy (B6) wraps this handler: as shipped it
// trusts no one and expects the wiring to resolve + authorize before
// delegating, so it never hydrates an unresolved repo.
type SmartHTTP struct {
	// Hydrate resolves owner/repo to a hydrated bare-repo directory. It does
	// authorization + resolution + cache hydration in one seam (B6 swaps the
	// resolver for token-scoped logic; the handler is unchanged).
	Hydrate func(ctx context.Context, owner, repo string) (dir string, err error)
	// Receive enables the write path when non-nil (see ReceiveConfig).
	Receive *ReceiveConfig
}

// ServeHTTP routes /{owner}/{repo}[.git]/info/refs, /git-upload-pack and —
// when the receive path is wired — /git-receive-pack.
func (h *SmartHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The action is itself two segments (info/refs), so match by suffix:
	// /{owner}/{repo}[/info/refs | /git-upload-pack | /git-receive-pack].
	trimmed := strings.Trim(r.URL.Path, "/")
	var owner, repo, action string
	switch {
	case strings.HasSuffix(trimmed, "/info/refs"):
		action = "info/refs"
		trimmed = strings.TrimSuffix(trimmed, "/info/refs")
	case strings.HasSuffix(trimmed, "/git-upload-pack"):
		action = "git-upload-pack"
		trimmed = strings.TrimSuffix(trimmed, "/git-upload-pack")
	case strings.HasSuffix(trimmed, "/git-receive-pack"):
		action = "git-receive-pack"
		trimmed = strings.TrimSuffix(trimmed, "/git-receive-pack")
	default:
		http.NotFound(w, r)
		return
	}
	slash := strings.Index(trimmed, "/")
	if slash <= 0 || strings.Contains(trimmed[slash+1:], "/") {
		http.NotFound(w, r)
		return
	}
	owner, repo = trimmed[:slash], strings.TrimSuffix(trimmed[slash+1:], ".git")

	switch {
	case action == "info/refs" && r.Method == http.MethodGet:
		h.infoRefs(w, r, owner, repo)
	case action == "git-upload-pack" && r.Method == http.MethodPost:
		h.uploadPack(w, r, owner, repo)
	case action == "git-receive-pack" && r.Method == http.MethodPost:
		h.serveReceivePack(w, r, owner, repo)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "not a git smart HTTP route", http.StatusMethodNotAllowed)
	}
}

// infoRefs answers the clone/push handshake's first request.
func (h *SmartHTTP) infoRefs(w http.ResponseWriter, r *http.Request, owner, repo string) {
	switch service := r.URL.Query().Get("service"); service {
	case "git-upload-pack":
		h.serveUploadAdvertisement(w, r, owner, repo)
	case "git-receive-pack":
		h.serveReceiveAdvertisement(w, r, owner, repo)
	default:
		http.Error(w, "unsupported git service", http.StatusBadRequest)
	}
}

// serveUploadAdvertisement is the clone-side (read path) advertisement.
func (h *SmartHTTP) serveUploadAdvertisement(w http.ResponseWriter, r *http.Request, owner, repo string) {
	dir, err := h.Hydrate(r.Context(), owner, repo)
	if err != nil {
		http.Error(w, "repository unavailable", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
	w.Header().Set("Cache-Control", "no-cache")
	writePktLine(w, "# service=git-upload-pack\n")
	fmt.Fprint(w, "0000")
	if err := runUploadPack(r, dir, w, "--advertise-refs"); err != nil {
		// Headers are already sent; nothing to do but hang up.
		panic(http.ErrAbortHandler)
	}
}

// uploadPack pipes the client's negotiation (want/have + pack request) into
// `git upload-pack --stateless-rpc` and the resulting packfile back.
func (h *SmartHTTP) uploadPack(w http.ResponseWriter, r *http.Request, owner, repo string) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-git-upload-pack-request") {
		http.Error(w, "unexpected content type", http.StatusBadRequest)
		return
	}
	dir, err := h.Hydrate(r.Context(), owner, repo)
	if err != nil {
		http.Error(w, "repository unavailable", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
	w.Header().Set("Cache-Control", "no-cache")
	if err := runUploadPack(r, dir, w); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// runUploadPack executes `git upload-pack --stateless-rpc [flags] dir`,
// wiring stdin from the request and stdout to the response. Protocol v2 is
// negotiated by passing the client's Git-Protocol header through the
// GIT_PROTOCOL env var — upstream git does all protocol handling.
func runUploadPack(r *http.Request, dir string, w io.Writer, args ...string) error {
	full := append([]string{"upload-pack", "--stateless-rpc"}, args...)
	full = append(full, filepath.Clean(dir))
	cmd := exec.CommandContext(r.Context(), "git", full...)
	cmd.Env = append(os.Environ(), "GIT_PROTOCOL="+protocolVersion(r))
	cmd.Stdin = r.Body
	cmd.Stdout = w
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git upload-pack: %w: %s", err, stderr.String())
	}
	return nil
}

// protocolVersion echoes the client's protocol preference, defaulting to v0
// (S3-compatible git servers must handle both; git picks v2 when the client
// advertises it).
func protocolVersion(r *http.Request) string {
	if v := r.Header.Get("Git-Protocol"); strings.Contains(v, "version=2") {
		return "version=2"
	}
	return ""
}

// writePktLine writes one pkt-line frame.
func writePktLine(w io.Writer, s string) {
	fmt.Fprintf(w, "%04x%s", len(s)+4, s)
}
