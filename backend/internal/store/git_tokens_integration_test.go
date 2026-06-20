package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// TestGitTokenCreateListRevoke exercises the full lifecycle: create a token,
// verify it appears in the list, revoke it, and confirm it's gone.
func TestGitTokenCreateListRevoke(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	suffix := uuid.NewString()[:8]

	user, err := st.CreateUser(ctx, db.CreateUserParams{
		Username:    "tokuser-" + suffix,
		Email:       "tokuser-" + suffix + "@example.test",
		DisplayName: "Token User",
		IsActive:    true,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = st.Pool().Exec(ctx, "DELETE FROM git_tokens WHERE user_id = $1", user.ID)
		cleanupUser(t, st, user.ID)
	})

	tokenName := "quill-git-" + suffix
	tok, err := st.CreateGitToken(ctx, db.CreateGitTokenParams{
		UserID:           user.ID,
		Name:             "laptop",
		ForgejoTokenName: tokenName,
	})
	if err != nil {
		t.Fatalf("create git token: %v", err)
	}
	if tok.Name != "laptop" {
		t.Fatalf("token name = %q, want laptop", tok.Name)
	}
	if tok.UserID != user.ID {
		t.Fatalf("token user_id mismatch")
	}

	// List confirms the token is present.
	rows, err := st.ListGitTokensByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != tok.ID {
		t.Fatalf("list returned %d tokens, want 1 with id %v", len(rows), tok.ID)
	}
	if rows[0].ForgejoTokenName != tokenName {
		t.Fatalf("forgejo_token_name = %q, want %q", rows[0].ForgejoTokenName, tokenName)
	}

	// Revoke removes it.
	if err := st.DeleteGitToken(ctx, db.DeleteGitTokenParams{ID: tok.ID, UserID: user.ID}); err != nil {
		t.Fatalf("delete git token: %v", err)
	}
	rows, err = st.ListGitTokensByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list after revoke: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 tokens after revoke, got %d", len(rows))
	}
}

// TestGitTokenOwnershipBoundary verifies that a token created for user A
// cannot be revoked by user B (the user_id filter in GetGitToken / DeleteGitToken
// must prevent cross-user token access).
func TestGitTokenOwnershipBoundary(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	suffix := uuid.NewString()[:8]

	userA, err := st.CreateUser(ctx, db.CreateUserParams{
		Username: "toka-" + suffix,
		Email:    "toka-" + suffix + "@example.test",
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}
	userB, err := st.CreateUser(ctx, db.CreateUserParams{
		Username: "tokb-" + suffix,
		Email:    "tokb-" + suffix + "@example.test",
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("create user B: %v", err)
	}
	t.Cleanup(func() {
		_, _ = st.Pool().Exec(ctx, "DELETE FROM git_tokens WHERE user_id IN ($1, $2)", userA.ID, userB.ID)
		cleanupUser(t, st, userA.ID)
		cleanupUser(t, st, userB.ID)
	})

	tok, err := st.CreateGitToken(ctx, db.CreateGitTokenParams{
		UserID:           userA.ID,
		Name:             "userA-token",
		ForgejoTokenName: "quill-git-a-" + suffix,
	})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	// User B trying to fetch A's token by ID must get no rows.
	if _, err := st.GetGitToken(ctx, db.GetGitTokenParams{ID: tok.ID, UserID: userB.ID}); err == nil {
		t.Fatal("expected no-rows error fetching another user's token, got nil")
	}

	// Delete with B's ID must be a no-op (not an error, but token still exists).
	_ = st.DeleteGitToken(ctx, db.DeleteGitTokenParams{ID: tok.ID, UserID: userB.ID})
	rows, err := st.ListGitTokensByUser(ctx, userA.ID)
	if err != nil {
		t.Fatalf("list after cross-user delete: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("token was deleted by wrong user; expected 1 remaining, got %d", len(rows))
	}
}
