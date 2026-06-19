package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Environments are project-owned, ranked deployment targets (staging,
// production, …). They are pure Quill platform metadata — Forgejo has no
// equivalent, so there is no git mirror. Every repository in a project shares
// the project's environments, and `rank` orders the promotion ladder so
// environment policies can require an ordered path (deploy to a lower-ranked
// environment before a higher one).
//
// Defining environments is project configuration, so writes require a project
// admin (matching environment-policy writes); reads are open to project members.
// The deploy flow that consumes these targets and runs the environment gate
// lands in a later PR.

// maxEnvironments caps the number of environments a single project may hold.
const maxEnvironments = 50

// maxEnvironmentRank bounds the promotion-ladder rank to a sane range.
const maxEnvironmentRank = 1000

// CreateEnvironmentInput is the payload for defining an environment under a
// project.
type CreateEnvironmentInput struct {
	Slug        string
	Name        string
	Description string
	Rank        int
}

// UpdateEnvironmentInput replaces an environment's mutable fields. The slug is
// immutable (it is the stable identifier matched by policy selectors and used in
// URLs), so renaming means deleting and recreating.
type UpdateEnvironmentInput struct {
	Name        string
	Description string
	Rank        int
}

// CreateEnvironment defines a new environment under projectSlug for a project
// admin.
func (s *Service) CreateEnvironment(ctx context.Context, actor Actor, projectSlug string, in CreateEnvironmentInput) (db.Environment, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return db.Environment{}, err
	}
	if err := s.authorizeProjectAdmin(ctx, actor, project.ID); err != nil {
		return db.Environment{}, err
	}

	slug := normalizeSlug(in.Slug)
	if !validSlug(slug) {
		return db.Environment{}, fmt.Errorf("%w: slug must be 1-63 chars of lowercase letters, digits, '-', '_' or '.', start alphanumeric, and not be a reserved word", ErrInvalidInput)
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = slug
	}
	if err := validateEnvironmentFields(name, in.Description, in.Rank); err != nil {
		return db.Environment{}, err
	}

	// Fail fast on a known-taken slug for a friendlier error than the unique
	// violation, and enforce the per-project cap.
	if _, err := s.store.GetEnvironmentBySlug(ctx, db.GetEnvironmentBySlugParams{ProjectID: project.ID, Lower: slug}); err == nil {
		return db.Environment{}, ErrConflict
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.Environment{}, fmt.Errorf("lookup environment: %w", err)
	}
	existing, err := s.store.ListEnvironmentsByProject(ctx, project.ID)
	if err != nil {
		return db.Environment{}, fmt.Errorf("list environments: %w", err)
	}
	if len(existing) >= maxEnvironments {
		return db.Environment{}, fmt.Errorf("%w: too many environments", ErrInvalidInput)
	}

	env, err := s.store.CreateEnvironment(ctx, db.CreateEnvironmentParams{
		ProjectID:   project.ID,
		Slug:        slug,
		Name:        name,
		Description: strings.TrimSpace(in.Description),
		Rank:        int32(in.Rank),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return db.Environment{}, ErrConflict
		}
		return db.Environment{}, fmt.Errorf("create environment: %w", err)
	}
	return env, nil
}

// ListEnvironments returns a project's environments ordered by promotion rank
// then slug, for an authorized project member.
func (s *Service) ListEnvironments(ctx context.Context, actor Actor, projectSlug string) (db.Project, []db.Environment, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return db.Project{}, nil, err
	}
	if err := s.authorizeProjectMember(ctx, actor, project.ID); err != nil {
		return db.Project{}, nil, err
	}
	envs, err := s.store.ListEnvironmentsByProject(ctx, project.ID)
	if err != nil {
		return db.Project{}, nil, fmt.Errorf("list environments: %w", err)
	}
	return project, envs, nil
}

// GetEnvironment returns a single environment by slug for an authorized project
// member.
func (s *Service) GetEnvironment(ctx context.Context, actor Actor, projectSlug, envSlug string) (db.Environment, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return db.Environment{}, err
	}
	if err := s.authorizeProjectMember(ctx, actor, project.ID); err != nil {
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

// UpdateEnvironment changes an environment's display fields and rank for a
// project admin. The slug is immutable.
func (s *Service) UpdateEnvironment(ctx context.Context, actor Actor, projectSlug, envSlug string, in UpdateEnvironmentInput) (db.Environment, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return db.Environment{}, err
	}
	if err := s.authorizeProjectAdmin(ctx, actor, project.ID); err != nil {
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

	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = env.Slug
	}
	if err := validateEnvironmentFields(name, in.Description, in.Rank); err != nil {
		return db.Environment{}, err
	}

	updated, err := s.store.UpdateEnvironment(ctx, db.UpdateEnvironmentParams{
		ID:          env.ID,
		Name:        name,
		Description: strings.TrimSpace(in.Description),
		Rank:        int32(in.Rank),
	})
	if err != nil {
		return db.Environment{}, fmt.Errorf("update environment: %w", err)
	}
	return updated, nil
}

// DeleteEnvironment removes an environment for a project admin.
func (s *Service) DeleteEnvironment(ctx context.Context, actor Actor, projectSlug, envSlug string) error {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeProjectAdmin(ctx, actor, project.ID); err != nil {
		return err
	}
	env, err := s.store.GetEnvironmentBySlug(ctx, db.GetEnvironmentBySlugParams{
		ProjectID: project.ID,
		Lower:     normalizeSlug(envSlug),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lookup environment: %w", err)
	}
	if err := s.store.DeleteEnvironment(ctx, env.ID); err != nil {
		return fmt.Errorf("delete environment: %w", err)
	}
	return nil
}

// validateEnvironmentFields enforces the shared bounds on an environment's
// display fields and promotion rank.
func validateEnvironmentFields(name, description string, rank int) error {
	if len(name) > 100 {
		return fmt.Errorf("%w: name is too long", ErrInvalidInput)
	}
	if len(strings.TrimSpace(description)) > 500 {
		return fmt.Errorf("%w: description is too long", ErrInvalidInput)
	}
	if rank < 0 || rank > maxEnvironmentRank {
		return fmt.Errorf("%w: rank must be between 0 and %d", ErrInvalidInput, maxEnvironmentRank)
	}
	return nil
}
