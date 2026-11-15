package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/pipeline"
	"github.com/nielsuitterdijk22/quill/internal/policy"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Deploy gates (2026-11-15). Environment policies were stored and edited but
// never consulted: a run reached an environment only through the API's
// explicit `environment` field, and nothing evaluated the rules. Now a run
// targets an environment either explicitly or through its workflow's
// `environment:` key, and the environment policies of the repository,
// project and organization are evaluated before anything is queued. A
// refused run is recorded as failed with the reasons and never reaches a
// worker (nor the environment's secrets).

// runGate is the verdict for one run.
type runGate struct {
	EnvID   uuid.NullUUID
	Env     string // environment slug ("" = no environment)
	Blocked string // why the gate refused ("" = allowed)
}

// deployGate resolves the run's environment and evaluates its policies.
// An explicitly requested unknown environment is a client error; one named
// by the workflow blocks the run (visible on the run page) instead.
func (s *Service) deployGate(ctx context.Context, repo db.Repository, explicitEnv, workflowYAML, ref, sha string) (runGate, error) {
	slug := strings.TrimSpace(explicitEnv)
	if slug == "" {
		name, err := pipeline.WorkflowEnvironment(workflowYAML)
		if err != nil {
			return runGate{Env: "?", Blocked: err.Error()}, nil
		}
		slug = name
	}
	if slug == "" {
		return runGate{}, nil
	}
	env, err := s.store.GetEnvironmentBySlug(ctx, db.GetEnvironmentBySlugParams{ProjectID: repo.ProjectID, Lower: normalizeSlug(slug)})
	if errors.Is(err, pgx.ErrNoRows) {
		if explicitEnv != "" {
			return runGate{}, fmt.Errorf("%w: unknown environment %q", ErrInvalidInput, slug)
		}
		return runGate{Env: slug, Blocked: fmt.Sprintf("environment %q is not defined on this project (create it under the project's environments)", slug)}, nil
	}
	if err != nil {
		return runGate{}, fmt.Errorf("lookup environment: %w", err)
	}
	gate := runGate{EnvID: uuid.NullUUID{UUID: env.ID, Valid: true}, Env: env.Slug}

	project, err := s.store.GetProjectByID(ctx, repo.ProjectID)
	if err != nil {
		return runGate{}, fmt.Errorf("load project: %w", err)
	}
	views, err := s.inheritedEnvironmentPolicies(ctx, project.ID, project.TenantID)
	if err != nil {
		return runGate{}, err
	}
	own, err := s.listEnvironmentPoliciesAt(ctx, policyScope{policy.ScopeRepo, repo.ID})
	if err != nil {
		return runGate{}, err
	}
	var policies []policy.ScopedPolicy
	for _, v := range append(views, own...) {
		raw, err := v.Rule.MarshalRules()
		if err != nil {
			return runGate{}, err
		}
		policies = append(policies, policy.ScopedPolicy{Scope: v.Scope, Selector: v.Selector, Rules: raw})
	}
	if len(policies) == 0 {
		return gate, nil
	}
	facts, err := s.store.CommitDeployFacts(ctx, repo.ID, sha)
	if err != nil {
		return runGate{}, fmt.Errorf("deploy facts: %w", err)
	}
	age := 0
	if !facts.FirstSeen.IsZero() {
		age = int(time.Since(facts.FirstSeen).Minutes())
	}
	decision, err := s.evaluator.Evaluate(ctx, policy.KindEnvironment, policies, policy.Context{
		Now: time.Now(),
		Environment: &policy.EnvironmentFacts{
			Environment: env.Slug,
			Ref:         strings.TrimPrefix(ref, "refs/heads/"),
			// Deploy approvals have no approval flow yet: a rule requiring
			// them blocks (and says so) rather than passing silently.
			Approvals:            0,
			RunSucceeded:         facts.RunSucceeded,
			PreviousEnvironments: facts.SucceededEnvironments,
			AgeMinutes:           age,
		},
	})
	if err != nil {
		return runGate{}, fmt.Errorf("evaluate environment policies: %w", err)
	}
	if !decision.Allow {
		msgs := make([]string, 0, len(decision.Denials))
		for _, d := range decision.Denials {
			msgs = append(msgs, fmt.Sprintf("%s (%s policy %q)", d.Message, d.Scope, d.Selector))
		}
		gate.Blocked = "deploy to " + env.Slug + " blocked: " + strings.Join(msgs, "; ")
	}
	return gate, nil
}

// recordGate stores the run's environment and, when the gate refused it,
// finishes the run as failed. Returns true when the run must not execute.
func (s *Service) recordGate(ctx context.Context, run db.PipelineRun, gate runGate) (bool, error) {
	if gate.Env == "" {
		return false, nil
	}
	if err := s.store.SetRunEnvironment(ctx, run.ID, gate.EnvID, gate.Env, gate.Blocked); err != nil {
		return false, fmt.Errorf("record run environment: %w", err)
	}
	if gate.Blocked == "" {
		return false, nil
	}
	if _, err := s.store.UpdatePipelineRunStatus(ctx, db.UpdatePipelineRunStatusParams{
		ID: run.ID, Status: pipeline.StatusFailure, FinishedAt: nowTS(),
	}); err != nil {
		return true, fmt.Errorf("block run: %w", err)
	}
	s.logger.Info("deploy gate blocked run", "run", run.ID, "environment", gate.Env, "reason", gate.Blocked)
	return true, nil
}
