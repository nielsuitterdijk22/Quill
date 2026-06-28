package server

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/platform"
)

const (
	githubTokenCookie = "quill_gh_token"
	githubStateCookie = "quill_gh_state"
	tokenCookieTTL    = 15 * time.Minute
)

// ---- token cookie encryption ------------------------------------------------

// cookieKey derives a 32-byte AES key from the JWT secret via SHA-256.
func (s *Server) cookieKey() []byte {
	sum := sha256.Sum256([]byte(s.cfg.JWT.Secret + "github-token"))
	return sum[:]
}

func (s *Server) encryptToken(plain string) (string, error) {
	block, err := aes.NewCipher(s.cookieKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.URLEncoding.EncodeToString(sealed), nil
}

func (s *Server) decryptToken(enc string) (string, error) {
	raw, err := base64.URLEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.cookieKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(raw) < ns {
		return "", fmt.Errorf("ciphertext too short")
	}
	plain, err := gcm.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *Server) githubTokenFromRequest(r *http.Request) (string, error) {
	c, err := r.Cookie(githubTokenCookie)
	if err != nil {
		return "", fmt.Errorf("no github token cookie")
	}
	return s.decryptToken(c.Value)
}

// ---- OAuth flow -------------------------------------------------------------

// handleGitHubOAuthRedirect starts the GitHub OAuth flow by redirecting the
// browser to the GitHub authorization page.
func (s *Server) handleGitHubOAuthRedirect(w http.ResponseWriter, r *http.Request) {
	if s.cfg.GitHub.ClientID == "" {
		httpx.Error(w, http.StatusNotImplemented, "not_configured", "GitHub OAuth is not configured")
		return
	}

	// Generate a random state value to prevent CSRF.
	b := make([]byte, 16)
	_, _ = io.ReadFull(rand.Reader, b)
	state := base64.URLEncoding.EncodeToString(b)

	http.SetCookie(w, &http.Cookie{
		Name:     githubStateCookie,
		Value:    state,
		Path:     "/",
		MaxAge:   int(tokenCookieTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.IsProduction(),
	})

	q := url.Values{}
	q.Set("client_id", s.cfg.GitHub.ClientID)
	q.Set("scope", "repo")
	q.Set("state", state)
	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+q.Encode(), http.StatusFound)
}

// handleGitHubOAuthCallback exchanges the OAuth code for an access token,
// encrypts it into a short-lived cookie, and redirects to the onboarding page.
func (s *Server) handleGitHubOAuthCallback(w http.ResponseWriter, r *http.Request) {
	// Validate state.
	stateCookie, err := r.Cookie(githubStateCookie)
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		httpx.Error(w, http.StatusBadRequest, "invalid_state", "OAuth state mismatch")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: githubStateCookie, MaxAge: -1, Path: "/"})

	code := r.URL.Query().Get("code")
	if code == "" {
		httpx.Error(w, http.StatusBadRequest, "missing_code", "no OAuth code in callback")
		return
	}

	// Exchange code for token.
	token, err := s.exchangeGitHubCode(r.Context(), code)
	if err != nil {
		s.logger.Error("github oauth code exchange failed", "error", err)
		httpx.Error(w, http.StatusBadGateway, "exchange_failed", "could not exchange GitHub OAuth code")
		return
	}

	enc, err := s.encryptToken(token)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not encrypt token")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     githubTokenCookie,
		Value:    enc,
		Path:     "/",
		MaxAge:   int(tokenCookieTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.IsProduction(),
	})

	// Redirect back to the frontend onboarding flow.
	origin := s.cfg.CORSAllowedOrigins[0]
	http.Redirect(w, r, origin+"/onboarding?step=import", http.StatusFound)
}

func (s *Server) exchangeGitHubCode(ctx context.Context, code string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	body := url.Values{}
	body.Set("client_id", s.cfg.GitHub.ClientID)
	body.Set("client_secret", s.cfg.GitHub.ClientSecret)
	body.Set("code", code)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://github.com/login/oauth/access_token",
		strings.NewReader(body.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Error != "" {
		return "", fmt.Errorf("github: %s", result.Error)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("github: empty access token")
	}
	return result.AccessToken, nil
}

// ---- repo listing ----------------------------------------------------------

type githubRepo struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	FullName    string `json:"fullName"`
	Description string `json:"description"`
	Private     bool   `json:"private"`
	CloneURL    string `json:"cloneUrl"`
	HTMLURL     string `json:"htmlUrl"`
}

// handleListGitHubRepos proxies the authenticated user's GitHub repo list using
// the token stored in the short-lived cookie set by the OAuth callback.
func (s *Server) handleListGitHubRepos(w http.ResponseWriter, r *http.Request) {
	token, err := s.githubTokenFromRequest(r)
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "no_token", "GitHub token not found — complete OAuth first")
		return
	}

	repos, err := fetchGitHubRepos(r.Context(), token)
	if err != nil {
		s.logger.Error("list github repos failed", "error", err)
		httpx.Error(w, http.StatusBadGateway, "github_error", "could not list GitHub repositories")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"repos": repos})
}

func fetchGitHubRepos(ctx context.Context, token string) ([]githubRepo, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var all []githubRepo
	page := 1
	for {
		u := fmt.Sprintf("https://api.github.com/user/repos?per_page=100&page=%d&sort=updated", page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("github API returned %d", resp.StatusCode)
		}

		var page []struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			FullName    string `json:"full_name"`
			Description string `json:"description"`
			Private     bool   `json:"private"`
			CloneURL    string `json:"clone_url"`
			HTMLURL     string `json:"html_url"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			return nil, err
		}
		for _, r := range page {
			all = append(all, githubRepo{
				ID:          r.ID,
				Name:        r.Name,
				FullName:    r.FullName,
				Description: r.Description,
				Private:     r.Private,
				CloneURL:    r.CloneURL,
				HTMLURL:     r.HTMLURL,
			})
		}
		if len(page) < 100 {
			break
		}
		resp.Body.Close()
		page2 := page
		_ = page2
		page++
	}
	return all, nil
}

// ---- import ----------------------------------------------------------------

type importGitHubRequest struct {
	ProjectSlug string `json:"projectSlug"`
	Repos       []struct {
		Name        string `json:"name"`
		CloneURL    string `json:"cloneUrl"`
		Description string `json:"description"`
		Private     bool   `json:"private"`
	} `json:"repos"`
}

type importResult struct {
	Name  string `json:"name"`
	Error string `json:"error,omitempty"`
	OK    bool   `json:"ok"`
}

// handleImportGitHubRepos migrates selected GitHub repositories into Forgejo
// and creates the corresponding Quill DB records. Each repo is processed
// concurrently; per-repo errors are reported without aborting the others.
func (s *Server) handleImportGitHubRepos(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	ghToken, err := s.githubTokenFromRequest(r)
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "no_token", "GitHub token not found — complete OAuth first")
		return
	}

	var req importGitHubRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	results := make([]importResult, len(req.Repos))
	var wg sync.WaitGroup
	for i, repo := range req.Repos {
		wg.Add(1)
		go func(i int, name, cloneURL, desc string, private bool) {
			defer wg.Done()
			results[i].Name = name
			_, err := s.platform.CreateRepo(r.Context(), actor, req.ProjectSlug, platform.CreateRepoInput{
				Slug:        name,
				Name:        name,
				Description: desc,
				Visibility:  visibilityFromPrivate(private),
				CloneFrom: &platform.CloneSource{
					URL:       cloneURL,
					AuthToken: ghToken,
				},
			})
			if err != nil {
				results[i].Error = err.Error()
				results[i].OK = false
			} else {
				results[i].OK = true
			}
		}(i, repo.Name, repo.CloneURL, repo.Description, repo.Private)
	}
	wg.Wait()

	// Clear the GitHub token cookie — it's single-use for the import flow.
	http.SetCookie(w, &http.Cookie{Name: githubTokenCookie, MaxAge: -1, Path: "/"})

	httpx.JSON(w, http.StatusOK, map[string]any{"results": results})
}

func visibilityFromPrivate(private bool) string {
	if private {
		return "private"
	}
	return "public"
}
