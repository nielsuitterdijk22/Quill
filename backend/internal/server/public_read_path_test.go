package server

import (
	"net/http"
	"testing"
)

func TestPublicRepoReadPath(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodGet, "/api/v1/projects/acme/repos/site", true},
		{http.MethodGet, "/api/v1/projects/acme/repos/site/branches", true},
		{http.MethodGet, "/api/v1/projects/acme/repos/site/commits", true},
		{http.MethodGet, "/api/v1/projects/acme/repos/site/commits/abc123", true},
		{http.MethodGet, "/api/v1/projects/acme/repos/site/contents", true},
		{http.MethodGet, "/api/v1/projects/acme/repos/site/contents?path=a", true},
		{http.MethodPost, "/api/v1/projects/acme/repos/site/markup", true},

		// Repository list/create and all write/member paths stay auth-required.
		{http.MethodGet, "/api/v1/projects/acme/repos", false},
		{http.MethodPost, "/api/v1/projects/acme/repos", false},
		{http.MethodPost, "/api/v1/projects/acme/repos/site/fork", false},
		{http.MethodPut, "/api/v1/projects/acme/repos/site/star", false},
		{http.MethodGet, "/api/v1/projects/acme/repos/site/pulls", false},
		{http.MethodGet, "/api/v1/projects/acme/repos/site/issues", false},
		{http.MethodGet, "/api/v1/projects/acme/repos/site/settings", false},
		{http.MethodGet, "/api/v1/projects/acme/repos/site/secrets", false},
		{http.MethodGet, "/api/v1/orgs/acme", false},
		{http.MethodGet, "/api/v1/me/projects", false},
	}
	for _, c := range cases {
		r, _ := http.NewRequest(c.method, c.path, nil)
		if got := publicRepoReadPath(r); got != c.want {
			t.Errorf("%s %s: publicRepoReadPath = %v, want %v", c.method, c.path, got, c.want)
		}
	}
}
