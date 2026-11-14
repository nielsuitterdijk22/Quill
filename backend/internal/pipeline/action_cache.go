package pipeline

import (
	"context"
	"io"
	"strings"

	"github.com/nektos/act/pkg/runner"
)

// actionSource resolves `uses: org/repo@ref` step actions (2026-11-14).
//
// act builds a step action's URL from GITHUB_SERVER_URL, which Quill points
// at its own git host (so reusable workflows and the API resolve there). With
// Forgejo gone, Quill hosts no copy of actions/checkout, actions/setup-go & co,
// so every such step failed in its pre-stage: the job died with all steps
// "skipped". Short names now resolve the way GitHub and Forgejo
// (DEFAULT_ACTIONS_URL=github) resolve them — github.com first, anonymously,
// so the Quill run token never leaves Quill — and fall back to a Quill-hosted
// repository of that name (with the run token) only when GitHub has none.
// GitHub-first also means no tenant can shadow a well-known action for every
// other tenant by creating a public Quill repo called actions/checkout.
type actionSource struct {
	inner     runner.ActionCache
	quillBase string // GITHUB_SERVER_URL as given to act ("" = no Quill host)
	githubURL string // https://github.com (overridable in tests)
}

func newActionSource(inner runner.ActionCache, quillBase string) *actionSource {
	return &actionSource{
		inner:     inner,
		quillBase: strings.TrimSuffix(strings.TrimSpace(quillBase), "/"),
		githubURL: "https://github.com",
	}
}

// Fetch implements runner.ActionCache.
func (a *actionSource) Fetch(ctx context.Context, cacheDir, url, ref, token string) (string, error) {
	if a.quillBase == "" || !strings.EqualFold(url, a.quillBase+"/"+cacheDir) {
		// Not a short-name resolution against Quill: leave it alone.
		return a.inner.Fetch(ctx, cacheDir, url, ref, token)
	}
	sha, ghErr := a.inner.Fetch(ctx, cacheDir, a.githubURL+"/"+cacheDir, ref, "")
	if ghErr == nil {
		return sha, nil
	}
	sha, err := a.inner.Fetch(ctx, cacheDir, url, ref, token)
	if err != nil {
		// Report the GitHub miss too: that is where most actions live.
		return "", &actionFetchError{github: ghErr, quill: err}
	}
	return sha, nil
}

// GetTarArchive implements runner.ActionCache.
func (a *actionSource) GetTarArchive(ctx context.Context, cacheDir, sha, includePrefix string) (io.ReadCloser, error) {
	return a.inner.GetTarArchive(ctx, cacheDir, sha, includePrefix)
}

type actionFetchError struct{ github, quill error }

func (e *actionFetchError) Error() string {
	return "not found on github.com (" + e.github.Error() + ") nor on this Quill instance (" + e.quill.Error() + ")"
}
