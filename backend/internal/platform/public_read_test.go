package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Public repos are browsable anonymously (GH-style open-source browsing): the
// read paths allow a caller that is neither a project member nor authenticated
// whenever the repository's visibility is public, while private repos must look
// like they don't exist to outsiders. Members are unaffected.
func TestAnonymousPublicRepoReads(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()

	owner := Actor{UserID: scopeMakeUser(t, st, "prowner")}
	if _, _, err := svc.CreateOrganization(ctx, owner, "puborg", "Pub Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	project, err := st.GetProjectBySlug(ctx, "puborg")
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if _, err := st.CreateRepository(ctx, db.CreateRepositoryParams{
		ProjectID: project.ID, Slug: "site", Name: "site", Visibility: VisibilityPublic, DefaultBranch: "main",
	}); err != nil {
		t.Fatalf("seed public repo: %v", err)
	}
	if _, err := st.CreateRepository(ctx, db.CreateRepositoryParams{
		ProjectID: project.ID, Slug: "secret", Name: "secret", Visibility: VisibilityPrivate, DefaultBranch: "main",
	}); err != nil {
		t.Fatalf("seed private repo: %v", err)
	}

	anon := Actor{UserID: uuid.Nil}

	// Anonymous caller reads the public repo's metadata.
	repo, err := svc.GetRepo(ctx, anon, "puborg", "site")
	if err != nil {
		t.Fatalf("anon read public repo: %v", err)
	}
	if repo.Slug != "site" || repo.Visibility != VisibilityPublic {
		t.Fatalf("unexpected repo: %+v", repo)
	}

	// Anonymous caller on a private repo gets NOT FOUND (not forbidden — a
	// 404 doesn't leak the repository's existence).
	if _, err := svc.GetRepo(ctx, anon, "puborg", "secret"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("anon read private repo: want ErrNotFound, got %v", err)
	}

	// A logged-in non-member gets the same treatment on both.
	stranger := Actor{UserID: scopeMakeUser(t, st, "stranger")}
	if _, err := svc.GetRepo(ctx, stranger, "puborg", "site"); err != nil {
		t.Fatalf("non-member public read: %v", err)
	}
	if _, err := svc.GetRepo(ctx, stranger, "puborg", "secret"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-member private read: want ErrForbidden, got %v", err)
	}

	// The member still reads both (behavior unchanged for members).
	if _, err := svc.GetRepo(ctx, owner, "puborg", "secret"); err != nil {
		t.Fatalf("member private read: %v", err)
	}
}

// resolveRepo (the write path) must NOT be loosened: anonymous and non-members
// are rejected even on public repos.
func TestAnonymousCannotUseWriteResolution(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()

	owner := Actor{UserID: scopeMakeUser(t, st, "wrowner")}
	if _, _, err := svc.CreateOrganization(ctx, owner, "worg", "W Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	project, err := st.GetProjectBySlug(ctx, "worg")
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if _, err := st.CreateRepository(ctx, db.CreateRepositoryParams{
		ProjectID: project.ID, Slug: "app", Name: "app", Visibility: VisibilityPublic, DefaultBranch: "main",
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	anon := Actor{UserID: uuid.Nil}
	_, _, err = svc.ListPulls(ctx, anon, "worg", "app", "open")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("anon repo write path: want ErrForbidden, got %v", err)
	}
}
