package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/gitengine"
	"github.com/nielsuitterdijk22/quill/internal/policy"
	"github.com/nielsuitterdijk22/quill/internal/store"
)

// Production adapters for the git engine's write-path seams (serverless epic,
// PR B3): the referee over the Postgres ref-CAS row, the cross-instance push
// lease, and the pre-receive guard enforcing branch policies on pushes.
//
// Policy scope decision (DECISIONS.md): a pre-receive hook can only see ref
// old/new — it cannot count approvals or pipeline statuses. RequiredApprovals,
// RequirePullRequest and RequireStatusChecks target the MERGE gate (when Quill
// merges a PR through its API, B5); the push path enforces only what a push
// can actually violate: BlockForcePush, which covers force pushes AND
// deletions of matched branches (removing history from the branch is the
// damage force-push protection exists for).

var zeroGitID = "0000000000000000000000000000000000000000"

// GitReferee adapts the Postgres referee row to the engine's Referee seam.
func (s *Service) GitReferee() gitengine.Referee { return &pgReferee{store: s.store} }

type pgReferee struct {
	store *store.Store
}

func (r *pgReferee) Load(ctx context.Context, repoID string) (gitengine.RefereeState, error) {
	id, err := uuid.Parse(repoID)
	if err != nil {
		return gitengine.RefereeState{}, fmt.Errorf("parse repo id: %w", err)
	}
	st, err := r.store.GetGitRepoState(ctx, id)
	if err != nil {
		return gitengine.RefereeState{}, err
	}
	return gitengine.RefereeState{Revision: st.Revision, Refs: st.Refs, StorageBytes: st.StorageBytes}, nil
}

func (r *pgReferee) Commit(ctx context.Context, repoID string, from gitengine.RefereeState, refs map[string]string, storageBytes int64) (gitengine.RefereeState, error) {
	id, err := uuid.Parse(repoID)
	if err != nil {
		return gitengine.RefereeState{}, fmt.Errorf("parse repo id: %w", err)
	}
	rev, err := r.store.CASGitRepoRefs(ctx, id, from.Revision, refs, storageBytes)
	if err != nil {
		if errors.Is(err, store.ErrRefCASLost) {
			return gitengine.RefereeState{}, gitengine.ErrLostRace
		}
		return gitengine.RefereeState{}, err
	}
	return gitengine.RefereeState{Revision: rev, Refs: refs, StorageBytes: storageBytes}, nil
}

// GitPushLeases adapts the push_leases table to the engine's lease seam. Every
// Acquire draws a fresh random holder, so two pushes from the same instance
// still contend for the single-writer slot exactly like distant instances.
func (s *Service) GitPushLeases() gitengine.PushLeases { return &pgLeases{store: s.store} }

type pgLeases struct {
	store *store.Store
}

func (l *pgLeases) Acquire(ctx context.Context, key string, ttl time.Duration) (func() error, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return nil, fmt.Errorf("parse repo id: %w", err)
	}
	raw := make([]byte, 8)
	_, _ = rand.Read(raw)
	holder := "push-" + hex.EncodeToString(raw)
	won, err := l.store.AcquirePushLease(ctx, id, holder, ttl)
	if err != nil {
		return nil, err
	}
	if !won {
		return nil, gitengine.ErrLeaseHeld
	}
	return func() error { return l.store.ReleasePushLease(ctx, id, holder) }, nil
}

// GitPushGuard returns the production pre-receive guard: policy-driven ref
// protection enforced in the smart-HTTP push handler.
func (s *Service) GitPushGuard() gitengine.ReceiveGuard { return &gitPushGuard{svc: s} }

type gitPushGuard struct {
	svc *Service
}

// PreReceive rejects push commands that violate an effective branch rule.
// The snapshot of referenced commits lives in the hydrated repo dir
// (ReceiveContext.Dir), provided by the handler before receive-pack runs.
func (g *gitPushGuard) PreReceive(ctx context.Context, rcx gitengine.ReceiveContext, cmds []gitengine.PushCommand, actor string) error {
	repoID, err := uuid.Parse(rcx.ID)
	if err != nil {
		return fmt.Errorf("parse repo id: %w", err)
	}
	repo, err := g.svc.store.GetRepositoryByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("load repo: %w", err)
	}
	project, err := g.svc.store.GetProjectByID(ctx, repo.ProjectID)
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}
	scoped, err := g.svc.repoScopedBranchPolicies(ctx, repoID, project.ID, project.TenantID)
	if err != nil {
		return err
	}
	for _, c := range cmds {
		branch := strings.TrimPrefix(c.Ref, "refs/heads/")
		rule, pattern := policy.EffectiveBranch(scoped, branch)
		if rule == nil || !rule.BlockForcePush {
			continue
		}
		if c.New == zeroGitID {
			return fmt.Errorf("deleting %s is blocked by branch policy %q", c.Ref, pattern)
		}
		if c.Old == zeroGitID {
			continue // creation is not a force push
		}
		ff, err := isAncestor(rcx.Dir, c.Old, c.New)
		if err != nil {
			return fmt.Errorf("cannot verify fast-forward for %s: %w", c.Ref, err)
		}
		if !ff {
			return fmt.Errorf("force push to %s is blocked by branch policy %q", c.Ref, pattern)
		}
	}
	return nil
}

// repoScopedBranchPolicies loads the branch policies governing one repo from
// all three scopes (repo, project, tenant) in the resolver's input shape.
func (s *Service) repoScopedBranchPolicies(ctx context.Context, repoID, projectID, tenantID uuid.UUID) ([]policy.ScopedBranch, error) {
	var out []policy.ScopedBranch
	for _, scope := range []struct {
		typ policy.ScopeType
		id  uuid.UUID
	}{
		{policy.ScopeTenant, tenantID},
		{policy.ScopeProject, projectID},
		{policy.ScopeRepo, repoID},
	} {
		views, err := s.listBranchPoliciesAt(ctx, policyScope{scope.typ, scope.id})
		if err != nil {
			return nil, err
		}
		for _, v := range views {
			out = append(out, policy.ScopedBranch{
				Scope: v.Scope, Selector: v.Pattern, Locked: v.Locked, Rule: v.Rule,
			})
		}
	}
	return out, nil
}

// isAncestor reports whether old is an ancestor of new (a fast-forward update)
// using git's own graph walk in the hydrated repo.
func isAncestor(dir, oldSHA, newSHA string) (bool, error) {
	cmd := exec.Command("git", "merge-base", "--is-ancestor", oldSHA, newSHA)
	cmd.Dir = dir
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}
