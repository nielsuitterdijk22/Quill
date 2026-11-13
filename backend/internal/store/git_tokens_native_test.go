package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// A user holds any number of native qgt_ tokens. The Forgejo-era unique index
// on (user_id, forgejo_token_name) made the SECOND native token collide on the
// empty Forgejo name (found live in the D2 cutover: "could not create git
// token" for every user who already had one).
func TestMultipleNativeGitTokensPerUser(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	suffix := uuid.NewString()[:8]
	u, err := st.CreateUser(ctx, db.CreateUserParams{
		Username: "tok_" + suffix, Email: "tok_" + suffix + "@example.test", DisplayName: "tok", IsActive: true,
	})
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	t.Cleanup(func() { _, _ = st.Pool().Exec(ctx, "DELETE FROM users WHERE id = $1", u.ID) })
	for i, h := range []string{"hash-a-" + suffix, "hash-b-" + suffix, "hash-c-" + suffix} {
		if _, err := st.CreateNativeGitToken(ctx, u.ID, "laptop", h); err != nil {
			t.Fatalf("native token %d: %v", i+1, err)
		}
	}
}
