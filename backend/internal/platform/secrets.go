package platform

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// This file implements pipeline secrets: encrypted key/value pairs exposed to CI
// workflows as ${{ secrets.NAME }}. Secrets live at three scopes in one table —
// project (shared by every repo), repository, and environment — and merge at run
// time in that order (project → repo → environment, later winning) so a repo or
// environment secret overrides a project secret of the same name.
//
// Values are encrypted at rest (see internal/secretbox) and are write-only: the
// API accepts a value on create/update but never returns one. Managing secrets
// is project configuration, so every operation requires a project admin.

// maxSecretsPerScope caps how many secrets a single scope may hold.
const maxSecretsPerScope = 100

// maxSecretValueBytes bounds a single secret's plaintext size.
const maxSecretValueBytes = 64 << 10 // 64 KiB

// secretNameRe matches a valid secret name after upper-casing: it must start
// with a letter or underscore and contain only letters, digits, and
// underscores, mirroring GitHub Actions' rules.
var secretNameRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// Secret scope discriminators, surfaced to the UI so a listing (especially the
// repo's inherited view, which mixes scopes) can label where each secret lives.
const (
	SecretScopeProject     = "project"
	SecretScopeRepo        = "repo"
	SecretScopeEnvironment = "environment"
)

// SecretSummary is the write-only public view of a secret: its name, scope, and
// timestamps, never its value. ScopeName carries the environment slug for
// environment-scoped secrets (empty for project and repo scopes).
type SecretSummary struct {
	Name      string
	Scope     string
	ScopeName string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func toSecretSummary(s db.PipelineSecret, scope, scopeName string) SecretSummary {
	return SecretSummary{
		Name:      s.Name,
		Scope:     scope,
		ScopeName: scopeName,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}

func toSecretSummaries(rows []db.PipelineSecret, scope, scopeName string) []SecretSummary {
	out := make([]SecretSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, toSecretSummary(r, scope, scopeName))
	}
	return out
}

// ---- project-scoped secrets ------------------------------------------------

// ListProjectSecrets returns the names (never values) of a project's
// project-wide secrets for a project admin.
func (s *Service) ListProjectSecrets(ctx context.Context, actor Actor, projectSlug string) ([]SecretSummary, error) {
	project, err := s.authorizedProject(ctx, actor, projectSlug)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.ListProjectSecrets(ctx, project.ID)
	if err != nil {
		return nil, fmt.Errorf("list project secrets: %w", err)
	}
	return toSecretSummaries(rows, SecretScopeProject, ""), nil
}

// SetProjectSecret creates or replaces a project-wide secret for a project admin.
func (s *Service) SetProjectSecret(ctx context.Context, actor Actor, projectSlug, name, value string) (SecretSummary, error) {
	project, err := s.authorizedProject(ctx, actor, projectSlug)
	if err != nil {
		return SecretSummary{}, err
	}
	name, ciphertext, nonce, err := s.prepareSecret(name, value)
	if err != nil {
		return SecretSummary{}, err
	}
	existing, err := s.store.GetProjectSecretByName(ctx, db.GetProjectSecretByNameParams{ProjectID: project.ID, Name: name})
	switch {
	case err == nil:
		return s.updateSecretValue(ctx, existing.ID, ciphertext, nonce, SecretScopeProject, "")
	case errors.Is(err, pgx.ErrNoRows):
		if err := enforceSecretCap(s.store.ListProjectSecrets(ctx, project.ID)); err != nil {
			return SecretSummary{}, err
		}
		return s.createSecret(ctx, db.CreatePipelineSecretParams{
			ProjectID:  project.ID,
			Name:       name,
			Ciphertext: ciphertext,
			Nonce:      nonce,
			CreatedBy:  uuid.NullUUID{UUID: actor.UserID, Valid: true},
		}, SecretScopeProject, "")
	default:
		return SecretSummary{}, fmt.Errorf("lookup project secret: %w", err)
	}
}

// DeleteProjectSecret removes a project-wide secret for a project admin.
func (s *Service) DeleteProjectSecret(ctx context.Context, actor Actor, projectSlug, name string) error {
	project, err := s.authorizedProject(ctx, actor, projectSlug)
	if err != nil {
		return err
	}
	existing, err := s.store.GetProjectSecretByName(ctx, db.GetProjectSecretByNameParams{ProjectID: project.ID, Name: normalizeSecretName(name)})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lookup project secret: %w", err)
	}
	if err := s.store.DeletePipelineSecret(ctx, existing.ID); err != nil {
		return fmt.Errorf("delete project secret: %w", err)
	}
	return nil
}

// ---- repository-scoped secrets ---------------------------------------------

// ListRepoSecrets returns the names (never values) of a repository's secrets for
// a project admin.
func (s *Service) ListRepoSecrets(ctx context.Context, actor Actor, projectSlug, repoSlug string) ([]SecretSummary, error) {
	repo, err := s.authorizedRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.ListRepoSecrets(ctx, uuid.NullUUID{UUID: repo.ID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list repo secrets: %w", err)
	}
	return toSecretSummaries(rows, SecretScopeRepo, ""), nil
}

// ListInheritedSecretsForRepo returns the read-only secrets that apply to a
// repository's runs beyond its own: the project-wide secrets, plus every
// environment's secrets (any of which a manual run may target). It is metadata
// only — names and scope labels, never values — so the repo settings page can
// show what a run will actually receive. Requires a project admin, matching the
// repo secrets it sits beside.
func (s *Service) ListInheritedSecretsForRepo(ctx context.Context, actor Actor, projectSlug, repoSlug string) ([]SecretSummary, error) {
	repo, err := s.authorizedRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return nil, err
	}
	projectRows, err := s.store.ListProjectSecrets(ctx, repo.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("list project secrets: %w", err)
	}
	out := toSecretSummaries(projectRows, SecretScopeProject, "")

	envs, err := s.store.ListEnvironmentsByProject(ctx, repo.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("list environments: %w", err)
	}
	for _, env := range envs {
		envRows, err := s.store.ListEnvironmentSecrets(ctx, uuid.NullUUID{UUID: env.ID, Valid: true})
		if err != nil {
			return nil, fmt.Errorf("list environment secrets: %w", err)
		}
		out = append(out, toSecretSummaries(envRows, SecretScopeEnvironment, env.Slug)...)
	}
	return out, nil
}

// SetRepoSecret creates or replaces a repository secret for a project admin.
func (s *Service) SetRepoSecret(ctx context.Context, actor Actor, projectSlug, repoSlug, name, value string) (SecretSummary, error) {
	repo, err := s.authorizedRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return SecretSummary{}, err
	}
	name, ciphertext, nonce, err := s.prepareSecret(name, value)
	if err != nil {
		return SecretSummary{}, err
	}
	repoID := uuid.NullUUID{UUID: repo.ID, Valid: true}
	existing, err := s.store.GetRepoSecretByName(ctx, db.GetRepoSecretByNameParams{RepoID: repoID, Name: name})
	switch {
	case err == nil:
		return s.updateSecretValue(ctx, existing.ID, ciphertext, nonce, SecretScopeRepo, "")
	case errors.Is(err, pgx.ErrNoRows):
		if err := enforceSecretCap(s.store.ListRepoSecrets(ctx, repoID)); err != nil {
			return SecretSummary{}, err
		}
		return s.createSecret(ctx, db.CreatePipelineSecretParams{
			ProjectID:  repo.ProjectID,
			RepoID:     repoID,
			Name:       name,
			Ciphertext: ciphertext,
			Nonce:      nonce,
			CreatedBy:  uuid.NullUUID{UUID: actor.UserID, Valid: true},
		}, SecretScopeRepo, "")
	default:
		return SecretSummary{}, fmt.Errorf("lookup repo secret: %w", err)
	}
}

// DeleteRepoSecret removes a repository secret for a project admin.
func (s *Service) DeleteRepoSecret(ctx context.Context, actor Actor, projectSlug, repoSlug, name string) error {
	repo, err := s.authorizedRepo(ctx, actor, projectSlug, repoSlug)
	if err != nil {
		return err
	}
	existing, err := s.store.GetRepoSecretByName(ctx, db.GetRepoSecretByNameParams{
		RepoID: uuid.NullUUID{UUID: repo.ID, Valid: true},
		Name:   normalizeSecretName(name),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lookup repo secret: %w", err)
	}
	if err := s.store.DeletePipelineSecret(ctx, existing.ID); err != nil {
		return fmt.Errorf("delete repo secret: %w", err)
	}
	return nil
}

// ---- environment-scoped secrets --------------------------------------------

// ListEnvironmentSecrets returns the names (never values) of an environment's
// secrets for a project admin.
func (s *Service) ListEnvironmentSecrets(ctx context.Context, actor Actor, projectSlug, envSlug string) ([]SecretSummary, error) {
	env, err := s.authorizedEnvironment(ctx, actor, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.ListEnvironmentSecrets(ctx, uuid.NullUUID{UUID: env.ID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list environment secrets: %w", err)
	}
	return toSecretSummaries(rows, SecretScopeEnvironment, env.Slug), nil
}

// SetEnvironmentSecret creates or replaces an environment secret for a project
// admin.
func (s *Service) SetEnvironmentSecret(ctx context.Context, actor Actor, projectSlug, envSlug, name, value string) (SecretSummary, error) {
	env, err := s.authorizedEnvironment(ctx, actor, projectSlug, envSlug)
	if err != nil {
		return SecretSummary{}, err
	}
	name, ciphertext, nonce, err := s.prepareSecret(name, value)
	if err != nil {
		return SecretSummary{}, err
	}
	envID := uuid.NullUUID{UUID: env.ID, Valid: true}
	existing, err := s.store.GetEnvironmentSecretByName(ctx, db.GetEnvironmentSecretByNameParams{EnvironmentID: envID, Name: name})
	switch {
	case err == nil:
		return s.updateSecretValue(ctx, existing.ID, ciphertext, nonce, SecretScopeEnvironment, env.Slug)
	case errors.Is(err, pgx.ErrNoRows):
		if err := enforceSecretCap(s.store.ListEnvironmentSecrets(ctx, envID)); err != nil {
			return SecretSummary{}, err
		}
		return s.createSecret(ctx, db.CreatePipelineSecretParams{
			ProjectID:     env.ProjectID,
			EnvironmentID: envID,
			Name:          name,
			Ciphertext:    ciphertext,
			Nonce:         nonce,
			CreatedBy:     uuid.NullUUID{UUID: actor.UserID, Valid: true},
		}, SecretScopeEnvironment, env.Slug)
	default:
		return SecretSummary{}, fmt.Errorf("lookup environment secret: %w", err)
	}
}

// DeleteEnvironmentSecret removes an environment secret for a project admin.
func (s *Service) DeleteEnvironmentSecret(ctx context.Context, actor Actor, projectSlug, envSlug, name string) error {
	env, err := s.authorizedEnvironment(ctx, actor, projectSlug, envSlug)
	if err != nil {
		return err
	}
	existing, err := s.store.GetEnvironmentSecretByName(ctx, db.GetEnvironmentSecretByNameParams{
		EnvironmentID: uuid.NullUUID{UUID: env.ID, Valid: true},
		Name:          normalizeSecretName(name),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lookup environment secret: %w", err)
	}
	if err := s.store.DeletePipelineSecret(ctx, existing.ID); err != nil {
		return fmt.Errorf("delete environment secret: %w", err)
	}
	return nil
}

// ---- resolution ------------------------------------------------------------

// resolveRunSecrets gathers, decrypts, and merges the secrets visible to a run:
// project-wide, then the repository's, then (when the run targets one) the
// environment's — later scopes overriding earlier ones by name. A decryption
// failure aborts the run rather than silently dropping a secret.
func (s *Service) resolveRunSecrets(ctx context.Context, projectID, repoID uuid.UUID, envID uuid.NullUUID) (map[string]string, error) {
	out := make(map[string]string)

	projectSecrets, err := s.store.ListProjectSecrets(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project secrets: %w", err)
	}
	if err := s.mergeSecrets(out, projectSecrets); err != nil {
		return nil, err
	}

	repoSecrets, err := s.store.ListRepoSecrets(ctx, uuid.NullUUID{UUID: repoID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list repo secrets: %w", err)
	}
	if err := s.mergeSecrets(out, repoSecrets); err != nil {
		return nil, err
	}

	if envID.Valid {
		envSecrets, err := s.store.ListEnvironmentSecrets(ctx, envID)
		if err != nil {
			return nil, fmt.Errorf("list environment secrets: %w", err)
		}
		if err := s.mergeSecrets(out, envSecrets); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// mergeSecrets decrypts each row and writes it into dst, overriding any existing
// entry of the same name (later scopes win).
func (s *Service) mergeSecrets(dst map[string]string, rows []db.PipelineSecret) error {
	for _, row := range rows {
		plaintext, err := s.cipher.Open(row.Ciphertext, row.Nonce)
		if err != nil {
			return fmt.Errorf("decrypt secret %q: %w", row.Name, err)
		}
		dst[row.Name] = string(plaintext)
	}
	return nil
}

// ---- shared helpers --------------------------------------------------------

// authorizedProject loads a project and requires the actor to be a project admin.
func (s *Service) authorizedProject(ctx context.Context, actor Actor, projectSlug string) (db.Project, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return db.Project{}, err
	}
	if err := s.authorizeProjectAdmin(ctx, actor, project.ID); err != nil {
		return db.Project{}, err
	}
	return project, nil
}

// authorizedRepo resolves a repo and requires the actor to be a project admin
// (secret management is configuration, tighter than the member read that
// resolveRepo enforces).
func (s *Service) authorizedRepo(ctx context.Context, actor Actor, projectSlug, repoSlug string) (db.Repository, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return db.Repository{}, err
	}
	if err := s.authorizeProjectAdmin(ctx, actor, project.ID); err != nil {
		return db.Repository{}, err
	}
	repo, err := s.store.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{
		ProjectID: project.ID,
		Lower:     normalizeSlug(repoSlug),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Repository{}, ErrNotFound
	}
	if err != nil {
		return db.Repository{}, fmt.Errorf("lookup repo: %w", err)
	}
	return repo, nil
}

// authorizedEnvironment resolves an environment and requires a project admin.
func (s *Service) authorizedEnvironment(ctx context.Context, actor Actor, projectSlug, envSlug string) (db.Environment, error) {
	project, err := s.authorizedProject(ctx, actor, projectSlug)
	if err != nil {
		return db.Environment{}, err
	}
	env, err := s.store.GetEnvironmentBySlug(ctx, db.GetEnvironmentBySlugParams{
		ProjectID: project.ID,
		Lower:     normalizeSlug(envSlug),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Environment{}, ErrNotFound
	}
	if err != nil {
		return db.Environment{}, fmt.Errorf("lookup environment: %w", err)
	}
	return env, nil
}

// prepareSecret validates the name and value and seals the value, returning the
// normalized (upper-cased) name plus ciphertext and nonce.
func (s *Service) prepareSecret(name, value string) (string, []byte, []byte, error) {
	normalized := normalizeSecretName(name)
	if !secretNameRe.MatchString(normalized) {
		return "", nil, nil, fmt.Errorf("%w: secret name must start with a letter or underscore and contain only letters, digits, and underscores", ErrInvalidInput)
	}
	if len(normalized) > 256 {
		return "", nil, nil, fmt.Errorf("%w: secret name is too long", ErrInvalidInput)
	}
	if strings.HasPrefix(normalized, "GITHUB_") {
		return "", nil, nil, fmt.Errorf("%w: secret names must not start with GITHUB_", ErrInvalidInput)
	}
	if value == "" {
		return "", nil, nil, fmt.Errorf("%w: secret value is required", ErrInvalidInput)
	}
	if len(value) > maxSecretValueBytes {
		return "", nil, nil, fmt.Errorf("%w: secret value is too large", ErrInvalidInput)
	}
	ciphertext, nonce, err := s.cipher.Seal([]byte(value))
	if err != nil {
		return "", nil, nil, fmt.Errorf("encrypt secret: %w", err)
	}
	return normalized, ciphertext, nonce, nil
}

// createSecret persists a new secret row and returns its summary.
func (s *Service) createSecret(ctx context.Context, params db.CreatePipelineSecretParams, scope, scopeName string) (SecretSummary, error) {
	row, err := s.store.CreatePipelineSecret(ctx, params)
	if err != nil {
		if isUniqueViolation(err) {
			return SecretSummary{}, ErrConflict
		}
		return SecretSummary{}, fmt.Errorf("create secret: %w", err)
	}
	return toSecretSummary(row, scope, scopeName), nil
}

// updateSecretValue rotates an existing secret's ciphertext and returns its
// summary.
func (s *Service) updateSecretValue(ctx context.Context, id uuid.UUID, ciphertext, nonce []byte, scope, scopeName string) (SecretSummary, error) {
	row, err := s.store.UpdatePipelineSecretValue(ctx, db.UpdatePipelineSecretValueParams{
		ID:         id,
		Ciphertext: ciphertext,
		Nonce:      nonce,
	})
	if err != nil {
		return SecretSummary{}, fmt.Errorf("update secret: %w", err)
	}
	return toSecretSummary(row, scope, scopeName), nil
}

// enforceSecretCap rejects a create that would exceed the per-scope cap. It
// takes the (rows, err) pair from a scope's list query directly so callers stay
// terse.
func enforceSecretCap(rows []db.PipelineSecret, listErr error) error {
	if listErr != nil {
		return fmt.Errorf("count secrets: %w", listErr)
	}
	if len(rows) >= maxSecretsPerScope {
		return fmt.Errorf("%w: too many secrets in this scope", ErrInvalidInput)
	}
	return nil
}

// normalizeSecretName trims and upper-cases a secret name so lookups and
// uniqueness are case-insensitive in effect (the column is stored upper-cased).
func normalizeSecretName(name string) string {
	return strings.ToUpper(strings.TrimSpace(name))
}
