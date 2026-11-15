package platform_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/egress"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Pipeline egress: an organization policy (defaults until customised) is
// inherited by every repository, which can add hosts; the resolved policy
// travels in the queued job spec; only org admins / the personal owner
// change the org policy, and non-members can't read it.
func TestEgressPolicyInheritance(t *testing.T) {
	svc, st := newService(t)
	svc = svc.WithQueueMode()
	ctx := context.Background()
	repo := makeProjectWithRepo(t, st, "egress")
	project, err := st.GetProjectByID(ctx, repo.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := st.GetTenantByID(ctx, project.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	admin := platform.Actor{UserID: makeUser(t, st, "egress-admin"), IsAdmin: true}

	runPolicy := func() *egress.Policy {
		t.Helper()
		run, err := svc.EnqueuePipelineRun(ctx, repo, platform.TriggerInput{WorkflowPath: ".github/workflows/test.yml", Ref: "main", Event: "push"},
			queueWorkflowYAML, "deadbeef", uuid.NullUUID{})
		if err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		for i := 0; i < 50; i++ {
			j, err := svc.ClaimQueuedJob(ctx, "worker-egress")
			if err != nil || j == nil {
				t.Fatalf("claim: %v %v", j, err)
			}
			if j.RunID == run.ID {
				if j.Spec.Egress == nil {
					t.Fatal("job spec carries no egress policy")
				}
				return j.Spec.Egress
			}
		}
		t.Fatal("job not claimed")
		return nil
	}

	// Defaults.
	v, err := svc.GetTenantEgress(ctx, admin, tenant.Slug)
	if err != nil || v.Mode != egress.ModeAllowlist || v.Customized || !slices.Contains(v.Hosts, "registry.npmjs.org") || !v.CanEdit {
		t.Fatalf("default view: %+v %v", v, err)
	}
	pol := runPolicy()
	if !pol.Enforced() || !slices.Contains(pol.Hosts, "registry.npmjs.org") || !slices.Contains(pol.Hosts, "github.com") {
		t.Fatalf("default run policy: %+v", pol)
	}

	// Repository extras extend the inherited list.
	if _, err := svc.SetRepoEgress(ctx, admin, project.Slug, repo.Slug, []string{"https://Artifacts.Example.com/repo", "not a host"}); !errors.Is(err, platform.ErrInvalidInput) {
		t.Fatalf("invalid repo host accepted: %v", err)
	}
	rv, err := svc.SetRepoEgress(ctx, admin, project.Slug, repo.Slug, []string{"https://Artifacts.Example.com/repo"})
	if err != nil || !slices.Equal(rv.Hosts, []string{"artifacts.example.com"}) || rv.Inherited.Mode != egress.ModeAllowlist {
		t.Fatalf("repo extras: %+v %v", rv, err)
	}
	pol = runPolicy()
	if !slices.Contains(pol.Hosts, "artifacts.example.com") || !slices.Contains(pol.Hosts, "registry.npmjs.org") {
		t.Fatalf("repo extras missing from run policy: %v", pol.Hosts)
	}

	// A customised org list replaces the defaults; extras still apply.
	only := []string{"*.internal-mirror.example"}
	if _, err := svc.SetTenantEgress(ctx, admin, tenant.Slug, platform.TenantEgressInput{Mode: "block_some"}); !errors.Is(err, platform.ErrInvalidInput) {
		t.Fatalf("bad mode accepted: %v", err)
	}
	if v, err = svc.SetTenantEgress(ctx, admin, tenant.Slug, platform.TenantEgressInput{Mode: egress.ModeAllowlist, Hosts: &only}); err != nil || !v.Customized {
		t.Fatalf("customise: %+v %v", v, err)
	}
	pol = runPolicy()
	if slices.Contains(pol.Hosts, "registry.npmjs.org") || !slices.Contains(pol.Hosts, "*.internal-mirror.example") || !slices.Contains(pol.Hosts, "artifacts.example.com") {
		t.Fatalf("customised run policy: %v", pol.Hosts)
	}

	// Allow all.
	if _, err := svc.SetTenantEgress(ctx, admin, tenant.Slug, platform.TenantEgressInput{Mode: egress.ModeAllowAll, Hosts: &only}); err != nil {
		t.Fatal(err)
	}
	if pol = runPolicy(); pol.Enforced() || pol.Mode != egress.ModeAllowAll {
		t.Fatalf("allow-all run policy: %+v", pol)
	}
	// Reset to defaults.
	if v, err = svc.SetTenantEgress(ctx, admin, tenant.Slug, platform.TenantEgressInput{Mode: egress.ModeAllowlist}); err != nil || v.Customized || !slices.Contains(v.Hosts, "pypi.org") {
		t.Fatalf("reset: %+v %v", v, err)
	}

	// Non-members: no read, no write. A plain org member reads, can't write.
	stranger := actor(makeUser(t, st, "egress-stranger"))
	if _, err := svc.GetTenantEgress(ctx, stranger, tenant.Slug); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("stranger read: %v", err)
	}
	if _, err := svc.SetRepoEgress(ctx, stranger, project.Slug, repo.Slug, []string{"evil.example"}); err == nil {
		t.Fatal("stranger changed repo egress")
	}
	memberID := makeUser(t, st, "egress-member")
	if err := st.AddTenantMember(ctx, db.AddTenantMemberParams{TenantID: tenant.ID, UserID: memberID, Role: "member"}); err != nil {
		t.Fatal(err)
	}
	member := platform.Actor{UserID: memberID, TenantID: tenant.ID}
	if v, err := svc.GetTenantEgress(ctx, member, tenant.Slug); err != nil || v.CanEdit {
		t.Fatalf("member read: %+v %v", v, err)
	}
	if _, err := svc.SetTenantEgress(ctx, member, tenant.Slug, platform.TenantEgressInput{Mode: egress.ModeAllowAll}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("member write: %v", err)
	}

	// The owner of a personal tenant (no membership rows) administers it.
	personal, err := st.GetOrCreateTenantByExternalOrg(ctx, uuid.NewString(), "egress-personal")
	if err != nil {
		t.Fatal(err)
	}
	owner := platform.Actor{UserID: makeUser(t, st, "egress-owner"), TenantID: personal.ID}
	if _, err := svc.SetTenantEgress(ctx, owner, personal.Slug, platform.TenantEgressInput{Mode: egress.ModeAllowAll}); err != nil {
		t.Fatalf("personal owner write: %v", err)
	}
}
