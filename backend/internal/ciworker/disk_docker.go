//go:build !windows

package ciworker

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

// DockerDisk reclaims through the host's Docker daemon, over the Engine API
// on the mounted socket (stdlib HTTP; no Docker client dependency), and
// measures the filesystem at Path.
type DockerDisk struct {
	Path   string // a path on the Docker data filesystem ("/" in the worker)
	Socket string // default /var/run/docker.sock (or $DOCKER_HOST unix://…)
}

func (d DockerDisk) FreeBytes() (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(d.Path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}

func (d DockerDisk) http() *http.Client {
	sock := d.Socket
	if sock == "" {
		sock = strings.TrimPrefix(os.Getenv("DOCKER_HOST"), "unix://")
	}
	if sock == "" || strings.Contains(sock, "://") {
		sock = "/var/run/docker.sock"
	}
	return &http.Client{
		Timeout: 10 * time.Minute,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		}},
	}
}

func (d DockerDisk) Reclaim(ctx context.Context, keep []string) (string, error) {
	hc := d.http()
	do := func(method, path string, out any) error {
		req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, nil)
		if err != nil {
			return err
		}
		resp, err := hc.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return fmt.Errorf("%s %s: %s", method, path, resp.Status)
		}
		if out != nil {
			return json.NewDecoder(resp.Body).Decode(out)
		}
		return nil
	}
	var freed uint64
	var prune struct {
		SpaceReclaimed uint64 `json:"SpaceReclaimed"`
	}
	if err := do(http.MethodPost, "/build/prune?all=true", &prune); err == nil {
		freed += prune.SpaceReclaimed
	}
	var imgs []struct {
		ID         string   `json:"Id"`
		RepoTags   []string `json:"RepoTags"`
		Containers int64    `json:"Containers"`
		Size       int64    `json:"Size"`
	}
	if err := do(http.MethodGet, "/images/json", &imgs); err != nil {
		return fmt.Sprintf("build cache %s GiB", gib(freed)), err
	}
	removed := 0
	for _, img := range imgs {
		if img.Containers > 0 || keepImage(img.RepoTags, keep) {
			continue
		}
		// Untag by reference (dangling images by id), never forced: Docker
		// refuses images a container still uses, and that is what we want.
		refs := img.RepoTags
		if len(refs) == 0 || (len(refs) == 1 && refs[0] == "<none>:<none>") {
			refs = []string{img.ID}
		}
		gone := false
		for _, ref := range refs {
			if err := do(http.MethodDelete, "/images/"+url.PathEscape(ref), nil); err == nil {
				gone = true
			}
		}
		if gone {
			removed++
			if img.Size > 0 {
				freed += uint64(img.Size)
			}
		}
	}
	return fmt.Sprintf("build cache + %d images, ~%s GiB", removed, gib(freed)), nil
}
