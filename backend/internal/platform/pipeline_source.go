package platform

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/pipeline"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// pipelineSource is where a repo's workflows, commit SHAs and clone
// credentials come from (D2 cutover). Engine repos (referee row present) read
// everything from the hydrated bare repo and hand CI a scoped run token;
// Forgejo repos keep the legacy API path until they are migrated.
type pipelineSource struct {
	be   gitBackend
	repo db.Repository
}

const workflowsDir = ".github/workflows"

// pipelineSourceFor discriminates the repo's git backend.
func (s *Service) pipelineSourceFor(ctx context.Context, repo db.Repository) (pipelineSource, error) {
	project, err := s.store.GetProjectByID(ctx, repo.ProjectID)
	if err != nil {
		return pipelineSource{}, fmt.Errorf("lookup project: %w", err)
	}
	if s.gitEngine != nil && s.gitCache != nil {
		_, stateErr := s.store.GetGitRepoState(ctx, repo.ID)
		switch {
		case stateErr == nil:
			return pipelineSource{repo: repo, be: gitBackend{
				engine: true, repoID: repo.ID, tenantID: project.TenantID,
				projectSlug: project.Slug, repoSlug: repo.Slug,
			}}, nil
		case !errors.Is(stateErr, pgx.ErrNoRows):
			return pipelineSource{}, fmt.Errorf("lookup git state: %w", stateErr)
		}
	}
	owner, name, _ := forgejoTarget(repo, project)
	return pipelineSource{repo: repo, be: gitBackend{owner: owner, name: name}}, nil
}

// fullName is act's github.repository ("owner/name").
func (p pipelineSource) fullName() string {
	if p.be.engine {
		return p.be.projectSlug + "/" + p.be.repoSlug
	}
	if p.repo.ForgejoOwner.String == "" || p.repo.ForgejoName.String == "" {
		return p.repo.Slug
	}
	return p.repo.ForgejoOwner.String + "/" + p.repo.ForgejoName.String
}

func (p pipelineSource) label() string {
	if p.be.engine {
		return p.be.projectSlug + "/" + p.be.repoSlug
	}
	return p.be.owner + "/" + p.be.name
}

// engineRev maps an API ref ("", "main", "refs/heads/main", a sha) to a
// revision git understands; "" is the repo's HEAD (default branch).
func engineRev(ref string) string {
	if strings.TrimSpace(ref) == "" {
		return "HEAD"
	}
	return strings.TrimSpace(ref)
}

func (s *Service) listWorkflows(ctx context.Context, p pipelineSource, ref string) ([]forgejo.WorkflowFile, error) {
	if !p.be.engine {
		files, err := s.forgejo.ListWorkflows(ctx, p.be.owner, p.be.name, ref)
		if err != nil {
			return nil, translateForgejoRead(err)
		}
		return files, nil
	}
	if empty, err := s.engineRepoEmpty(ctx, p.be); err != nil || empty {
		return nil, err
	}
	dir, err := s.engineDir(ctx, p.be)
	if err != nil {
		return nil, err
	}
	out, err := gitBrowse(dir, "ls-tree", "--full-tree", engineRev(ref), workflowsDir+"/")
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var files []forgejo.WorkflowFile
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		meta, p, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 || fields[1] != "blob" {
			continue
		}
		name := path.Base(p)
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".yml") && !strings.HasSuffix(lower, ".yaml") {
			continue
		}
		files = append(files, forgejo.WorkflowFile{Path: p, Name: name, SHA: fields[2]})
	}
	return files, nil
}

// maxEngineWorkflowBytes mirrors the Forgejo path's cap.
const maxEngineWorkflowBytes = 512 << 10

func (s *Service) workflowContent(ctx context.Context, p pipelineSource, wfPath, ref string) (string, error) {
	if !p.be.engine {
		yaml, err := s.forgejo.GetWorkflowContent(ctx, p.be.owner, p.be.name, wfPath, ref)
		if err != nil {
			return "", translateForgejoRead(err)
		}
		return yaml, nil
	}
	dir, err := s.engineDir(ctx, p.be)
	if err != nil {
		return "", err
	}
	out, err := gitBrowse(dir, "show", engineRev(ref)+":"+strings.TrimPrefix(wfPath, "/"))
	if err != nil {
		return "", err
	}
	if len(out) > maxEngineWorkflowBytes {
		out = out[:maxEngineWorkflowBytes]
	}
	return out, nil
}

// commitSHA is best-effort, like the Forgejo path: "" when unresolvable.
func (s *Service) commitSHA(ctx context.Context, p pipelineSource, ref string) string {
	if !p.be.engine {
		return s.resolveCommitSHA(ctx, p.be.owner, p.be.name, ref)
	}
	dir, err := s.engineDir(ctx, p.be)
	if err != nil {
		return ""
	}
	out, err := gitBrowse(dir, "rev-parse", "--verify", "--quiet", engineRev(ref)+"^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// cloneAuth builds the job's git credentials. Engine repos get the public
// clone URL plus a read-only run token scoped to the repo's tenant — the same
// token feeds act's remote reusable workflows/actions (GITHUB_SERVER_URL is
// the engine base, so `uses: project/repo/...@ref` resolves there).
func (s *Service) cloneAuth(p pipelineSource) pipeline.CloneAuth {
	if !p.be.engine {
		return s.cloneAuthForRun(p.be.owner, p.be.name)
	}
	if s.gitPublicURL == "" {
		s.logger.Warn("pipeline clone URL unavailable: QUILL_GIT_PUBLIC_URL unset", "repo", p.label())
		return pipeline.CloneAuth{}
	}
	tok, err := s.MintRunToken(p.be.tenantID, RunTokenTTL)
	if err != nil {
		s.logger.Warn("pipeline run token unavailable", "repo", p.label(), "error", err)
		return pipeline.CloneAuth{URL: s.gitPublicURL + "/" + p.label() + ".git", ServerURL: s.gitPublicURL}
	}
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + tok))
	return pipeline.CloneAuth{
		URL:        s.gitPublicURL + "/" + p.label() + ".git",
		AuthHeader: "Authorization: Basic " + basic,
		Token:      tok,
		ServerURL:  s.gitPublicURL,
	}
}
