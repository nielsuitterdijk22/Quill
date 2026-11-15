package platform_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Operator analytics: real signups with their activation milestones, e2e
// fixtures left out, feature usage grouped from the audit log, admins only.
// (The test database is shared, so assertions look at this test's rows.)
func TestAnalytics(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	suffix := fmt.Sprint(time.Now().UnixNano() % 1e9)
	active := makeUser(t, st, "an-active-"+suffix)
	idle := makeUser(t, st, "an-idle-"+suffix)
	fixture := makeUser(t, st, "e2e-an-"+suffix)
	audit := func(user uuid.UUID, action string) {
		t.Helper()
		if _, err := st.InsertAuditLog(ctx, db.InsertAuditLogParams{
			ActorUserID: uuid.NullUUID{UUID: user, Valid: true}, Action: action, Metadata: []byte("{}"),
		}); err != nil {
			t.Fatal(err)
		}
	}
	audit(active, "repo.created")
	audit(active, "git.push")
	audit(active, "git.push")
	audit(active, "issue.opened")
	audit(fixture, "repo.created")

	if _, err := svc.GetAnalytics(ctx, platform.Actor{UserID: active}, 30); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("non-admin: %v", err)
	}
	a, err := svc.GetAnalytics(ctx, platform.Actor{UserID: active, IsAdmin: true}, 30)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]platform.AnalyticsSignup{}
	for _, s := range a.Signups {
		seen[s.Username] = s
	}
	if _, ok := seen["e2e-an-"+suffix]; ok {
		t.Fatal("an e2e fixture counted as a signup")
	}
	act, ok := seen["an-active-"+suffix]
	if !ok {
		t.Fatal("active signup missing")
	}
	if _, ok := act.Reached["repo"]; !ok {
		t.Fatalf("active user's milestones: %v", act.Reached)
	}
	if _, ok := act.Reached["push"]; !ok || act.LastSeen == nil {
		t.Fatalf("active user's push/last seen: %v %v", act.Reached, act.LastSeen)
	}
	if len(seen["an-idle-"+suffix].Reached) != 0 {
		t.Fatalf("idle user reached %v", seen["an-idle-"+suffix].Reached)
	}
	_ = idle
	if a.TotalSignups < 2 || a.WindowSignups < 2 || len(a.SignupsByDay) != 31 {
		t.Fatalf("signup counts: total=%d window=%d days=%d", a.TotalSignups, a.WindowSignups, len(a.SignupsByDay))
	}
	if a.Funnel[0].Key != "repo" || a.Funnel[0].Users < 1 {
		t.Fatalf("funnel: %+v", a.Funnel)
	}
	var push, issue *platform.FeatureUsage
	for i := range a.Features {
		switch a.Features[i].Action {
		case "git.push":
			push = &a.Features[i]
		case "issue.opened":
			issue = &a.Features[i]
		}
	}
	if push == nil || push.Feature != "Repositories" || push.Events < 2 || issue == nil || issue.Feature != "Issues" {
		t.Fatalf("features: push=%+v issue=%+v", push, issue)
	}
}
