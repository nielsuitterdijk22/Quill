package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Product analytics for operators (2026-11-15). Everything is read from data
// Quill already keeps (users, tenants, the audit log, pipeline runs, the
// referee): no third-party tracker, no client-side events. End-to-end test
// fixtures (users/tenants named e2e…) are left out of every number.

// e2eUser is the SQL predicate (on alias u) for live-test fixture accounts.
const e2eUser = `(u.username ILIKE 'e2e%' OR u.email ILIKE 'e2e%')`

// AnalyticsUser is one real account with its activation milestones.
type AnalyticsUser struct {
	ID         uuid.UUID
	Username   string
	CreatedAt  time.Time
	IsAdmin    bool
	Plan       string // the personal tenant's plan ("" when none)
	LastSeen   *time.Time
	Milestones map[string]time.Time // audit action → first time the user did it
}

// AnalyticsUsers lists real accounts (oldest first) with the first time each
// did one of the milestone actions, and when they were last active.
func (s *Store) AnalyticsUsers(ctx context.Context, milestones []string) ([]AnalyticsUser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.username, u.created_at, u.is_admin, COALESCE(t.plan, ''),
		       (SELECT max(a.created_at) FROM audit_log a WHERE a.actor_user_id = u.id)
		FROM users u
		LEFT JOIN tenants t ON t.id = u.tenant_id
		WHERE NOT `+e2eUser+`
		ORDER BY u.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AnalyticsUser
	index := map[uuid.UUID]int{}
	for rows.Next() {
		var u AnalyticsUser
		if err := rows.Scan(&u.ID, &u.Username, &u.CreatedAt, &u.IsAdmin, &u.Plan, &u.LastSeen); err != nil {
			return nil, err
		}
		u.Milestones = map[string]time.Time{}
		index[u.ID] = len(out)
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(milestones) == 0 || len(out) == 0 {
		return out, nil
	}
	mrows, err := s.pool.Query(ctx, `
		SELECT a.actor_user_id, a.action, min(a.created_at)
		FROM audit_log a
		WHERE a.actor_user_id IS NOT NULL AND a.action = ANY($1)
		GROUP BY 1, 2`, milestones)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var id uuid.UUID
		var action string
		var at time.Time
		if err := mrows.Scan(&id, &action, &at); err != nil {
			return nil, err
		}
		if i, ok := index[id]; ok {
			out[i].Milestones[action] = at
		}
	}
	return out, mrows.Err()
}

// AnalyticsTenant is one organization (or personal account) with its size.
type AnalyticsTenant struct {
	ID        uuid.UUID
	Slug      string
	Name      string
	Plan      string
	CreatedAt time.Time
	Members   int
	Personal  bool // a user's own account (no members table rows)
}

// AnalyticsTenants lists tenants that aren't e2e fixtures.
func (s *Store) AnalyticsTenants(ctx context.Context) ([]AnalyticsTenant, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.slug, t.name, t.plan, t.created_at,
		       (SELECT count(*) FROM tenant_members m WHERE m.tenant_id = t.id)::int,
		       EXISTS (SELECT 1 FROM users u WHERE u.tenant_id = t.id)
		FROM tenants t
		WHERE t.slug NOT ILIKE 'e2e%'
		ORDER BY t.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AnalyticsTenant
	for rows.Next() {
		var t AnalyticsTenant
		var owned bool
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.Plan, &t.CreatedAt, &t.Members, &owned); err != nil {
			return nil, err
		}
		t.Personal = owned && t.Members == 0
		out = append(out, t)
	}
	return out, rows.Err()
}

// AnalyticsAction is one audit action's volume in a window.
type AnalyticsAction struct {
	Action        string
	Events        int
	Users         int
	Tenants       int
	PreviousEvent int // events in the window before (for a trend)
}

// AnalyticsActions counts audit actions by real users since `since`, with the
// same-length window before it for comparison.
func (s *Store) AnalyticsActions(ctx context.Context, since time.Time) ([]AnalyticsAction, error) {
	window := time.Since(since)
	rows, err := s.pool.Query(ctx, `
		SELECT a.action,
		       count(*) FILTER (WHERE a.created_at >= $1)::int,
		       count(DISTINCT a.actor_user_id) FILTER (WHERE a.created_at >= $1)::int,
		       count(DISTINCT a.tenant_id) FILTER (WHERE a.created_at >= $1)::int,
		       count(*) FILTER (WHERE a.created_at < $1)::int
		FROM audit_log a
		LEFT JOIN users u ON u.id = a.actor_user_id
		WHERE a.created_at >= $2 AND (u.id IS NULL OR NOT `+e2eUser+`)
		GROUP BY a.action
		ORDER BY 2 DESC, 1`, since, since.Add(-window))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AnalyticsAction
	for rows.Next() {
		var a AnalyticsAction
		if err := rows.Scan(&a.Action, &a.Events, &a.Users, &a.Tenants, &a.PreviousEvent); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AnalyticsRuns summarises pipeline runs (any trigger) of non-e2e tenants
// since `since`.
type AnalyticsRuns struct {
	Total, Succeeded, Failed int
	Minutes                  float64
}

func (s *Store) AnalyticsRunTotals(ctx context.Context, since time.Time) (AnalyticsRuns, error) {
	var r AnalyticsRuns
	err := s.pool.QueryRow(ctx, `
		SELECT count(*)::int,
		       count(*) FILTER (WHERE pr.status = 'success')::int,
		       count(*) FILTER (WHERE pr.status = 'failure')::int,
		       COALESCE(sum(EXTRACT(EPOCH FROM (pr.finished_at - pr.started_at)) / 60.0)
		                FILTER (WHERE pr.finished_at IS NOT NULL AND pr.started_at IS NOT NULL), 0)::float8
		FROM pipeline_runs pr
		JOIN pipelines p ON p.id = pr.pipeline_id
		JOIN repositories r ON r.id = p.repo_id
		JOIN projects pj ON pj.id = r.project_id
		JOIN tenants t ON t.id = pj.tenant_id
		WHERE pr.created_at >= $1 AND t.slug NOT ILIKE 'e2e%'`, since).Scan(&r.Total, &r.Succeeded, &r.Failed, &r.Minutes)
	return r, err
}
