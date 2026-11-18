package platform

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Operator analytics (2026-11-15): signups and activation, plan usage per
// organization (the three metered gates), and feature usage from the audit
// log. Admin-only; first-party data only; e2e fixtures excluded.

// FunnelStep is one activation milestone, in order.
type FunnelStep struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Action string `json:"-"`
	Users  int    `json:"users"`
}

// funnelSteps are the activation milestones after signing up, each the first
// time the user performed the audit action.
var funnelSteps = []FunnelStep{
	{Key: "repo", Label: "Created a repository", Action: "repo.created"},
	{Key: "push", Label: "Pushed code", Action: "git.push"},
	{Key: "pr", Label: "Opened a pull request", Action: "pr.opened"},
	{Key: "run", Label: "Ran a pipeline", Action: "pipeline.run_triggered"},
	{Key: "invite", Label: "Invited a teammate", Action: "org.invite.created"},
	{Key: "paid", Label: "Upgraded", Action: "billing.plan_upgraded_creem"},
}

// AnalyticsSignup is one real account and how far it got.
type AnalyticsSignup struct {
	Username  string               `json:"username"`
	CreatedAt time.Time            `json:"createdAt"`
	Plan      string               `json:"plan"`
	IsAdmin   bool                 `json:"isAdmin"`
	LastSeen  *time.Time           `json:"lastSeen,omitempty"`
	Reached   map[string]time.Time `json:"reached"` // funnel key → first time
}

// DayCount is a count on one UTC day.
type DayCount struct {
	Day   string `json:"day"` // YYYY-MM-DD
	Count int    `json:"count"`
}

// TenantUsage is one organization against the free-plan gates.
type TenantUsage struct {
	Slug         string    `json:"slug"`
	Name         string    `json:"name"`
	Plan         string    `json:"plan"`
	Personal     bool      `json:"personal"`
	Members      int       `json:"members"`
	Repos        int64     `json:"repos"`
	Minutes      float64   `json:"minutes"` // this calendar month
	MinuteLimit  int       `json:"minuteLimit"`
	StorageBytes int64     `json:"storageBytes"`
	StorageLimit int64     `json:"storageLimit"`
	NearLimit    bool      `json:"nearLimit"` // a free tenant at ≥80% of any gate
	CreatedAt    time.Time `json:"createdAt"`
}

// FeatureUsage is one audit action's volume in the window.
type FeatureUsage struct {
	Feature  string `json:"feature"`
	Action   string `json:"action"`
	Events   int    `json:"events"`
	Users    int    `json:"users"`
	Tenants  int    `json:"tenants"`
	Previous int    `json:"previous"` // events in the window before
}

// Analytics is the admin analytics page's data.
type Analytics struct {
	WindowDays    int               `json:"windowDays"`
	GeneratedAt   time.Time         `json:"generatedAt"`
	TotalSignups  int               `json:"totalSignups"`
	WindowSignups int               `json:"windowSignups"`
	ActiveUsers   int               `json:"activeUsers"` // did anything in the window
	SignupsByDay  []DayCount        `json:"signupsByDay"`
	Funnel        []FunnelStep      `json:"funnel"`
	Signups       []AnalyticsSignup `json:"signups"`
	Tenants       []TenantUsage     `json:"tenants"`
	Features      []FeatureUsage    `json:"features"`
	Runs          struct {
		Total     int     `json:"total"`
		Succeeded int     `json:"succeeded"`
		Failed    int     `json:"failed"`
		Minutes   float64 `json:"minutes"`
	} `json:"runs"`
}

// featureOf groups audit actions into product areas.
func featureOf(action string) string {
	prefixes := []struct{ prefix, feature string }{
		{"auth.", "Accounts"}, {"profile.", "Accounts"},
		{"repo.", "Repositories"}, {"git.", "Repositories"},
		{"pr.", "Pull requests"},
		{"issue.", "Issues"},
		{"pipeline.", "Pipelines"},
		{"policy.", "Governance"}, {"egress.", "Governance"}, {"environment.", "Governance"},
		{"org.", "Teams"}, {"group.", "Teams"}, {"project.", "Teams"},
		{"billing.", "Billing"},
		{"admin.", "Admin"},
	}
	for _, p := range prefixes {
		if len(action) >= len(p.prefix) && action[:len(p.prefix)] == p.prefix {
			return p.feature
		}
	}
	return "Other"
}

// GetAnalytics assembles the analytics for the last `days` days. Admins only.
func (s *Service) GetAnalytics(ctx context.Context, actor Actor, days int) (Analytics, error) {
	if !actor.IsAdmin {
		return Analytics{}, ErrForbidden
	}
	if days <= 0 || days > 365 {
		days = 30
	}
	now := time.Now().UTC()
	since := now.AddDate(0, 0, -days)
	out := Analytics{WindowDays: days, GeneratedAt: now}

	actions := make([]string, len(funnelSteps))
	for i, f := range funnelSteps {
		actions[i] = f.Action
	}
	users, err := s.store.AnalyticsUsers(ctx, actions)
	if err != nil {
		return Analytics{}, fmt.Errorf("analytics users: %w", err)
	}
	byDay := map[string]int{}
	out.Funnel = make([]FunnelStep, len(funnelSteps))
	copy(out.Funnel, funnelSteps)
	for _, u := range users {
		out.TotalSignups++
		if !u.CreatedAt.Before(since) {
			out.WindowSignups++
			byDay[u.CreatedAt.UTC().Format("2006-01-02")]++
		}
		if u.LastSeen != nil && !u.LastSeen.Before(since) {
			out.ActiveUsers++
		}
		su := AnalyticsSignup{Username: u.Username, CreatedAt: u.CreatedAt, Plan: u.Plan, IsAdmin: u.IsAdmin, LastSeen: u.LastSeen, Reached: map[string]time.Time{}}
		for i, f := range out.Funnel {
			if at, ok := u.Milestones[f.Action]; ok {
				su.Reached[f.Key] = at
				out.Funnel[i].Users++
			}
		}
		out.Signups = append(out.Signups, su)
	}
	// Newest signups first on the page.
	sort.SliceStable(out.Signups, func(i, j int) bool { return out.Signups[i].CreatedAt.After(out.Signups[j].CreatedAt) })
	for d := since; !d.After(now); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		out.SignupsByDay = append(out.SignupsByDay, DayCount{Day: key, Count: byDay[key]})
	}

	tenants, err := s.store.AnalyticsTenants(ctx)
	if err != nil {
		return Analytics{}, fmt.Errorf("analytics tenants: %w", err)
	}
	for _, t := range tenants {
		tu := TenantUsage{Slug: t.Slug, Name: t.Name, Plan: t.Plan, Personal: t.Personal, Members: t.Members, CreatedAt: t.CreatedAt}
		if l := limitsForPlan(t.Plan); l != nil {
			tu.MinuteLimit, tu.StorageLimit = int(*l.PipelineMinutes), *l.StorageBytes
		}
		if tu.Repos, err = s.store.CountRepositoriesByTenant(ctx, t.ID); err != nil {
			return Analytics{}, fmt.Errorf("count repositories: %w", err)
		}
		if tu.Minutes, err = s.store.SumPipelineMinutesByTenant(ctx, t.ID); err != nil {
			return Analytics{}, fmt.Errorf("sum pipeline minutes: %w", err)
		}
		if tu.StorageBytes, err = s.store.SumEngineStorageByTenant(ctx, t.ID); err != nil {
			return Analytics{}, fmt.Errorf("sum storage: %w", err)
		}
		if tu.MinuteLimit > 0 {
			tu.NearLimit = tu.Minutes >= 0.8*float64(tu.MinuteLimit) ||
				float64(tu.StorageBytes) >= 0.8*float64(tu.StorageLimit)
		}
		out.Tenants = append(out.Tenants, tu)
	}

	acts, err := s.store.AnalyticsActions(ctx, since)
	if err != nil {
		return Analytics{}, fmt.Errorf("analytics actions: %w", err)
	}
	for _, a := range acts {
		out.Features = append(out.Features, FeatureUsage{Feature: featureOf(a.Action), Action: a.Action, Events: a.Events, Users: a.Users, Tenants: a.Tenants, Previous: a.PreviousEvent})
	}

	runs, err := s.store.AnalyticsRunTotals(ctx, since)
	if err != nil {
		return Analytics{}, fmt.Errorf("analytics runs: %w", err)
	}
	out.Runs.Total, out.Runs.Succeeded, out.Runs.Failed, out.Runs.Minutes = runs.Total, runs.Succeeded, runs.Failed, runs.Minutes
	return out, nil
}
