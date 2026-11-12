package store

import (
	"context"

	"github.com/google/uuid"
)

// Git-endpoint authentication helpers (serverless epic, PR B6). Native git
// tokens validate by SHA-256 hash lookup: the presented secret is
// high-entropy random material, so a fast deterministic digest is the right
// primitive (lookup is indexed; nothing reversible is stored).

// GitTokenByHash resolves a presented native git token to its owner + row id.
// ErrNotFound semantics follow the pool's pgx.ErrNoRows.
func (s *Store) GitTokenByHash(ctx context.Context, tokenHash string) (tokenID, userID uuid.UUID, err error) {
	err = s.pool.QueryRow(ctx,
		"SELECT id, user_id FROM git_tokens WHERE token_hash = $1 LIMIT 1", tokenHash).
		Scan(&tokenID, &userID)
	return tokenID, userID, err
}

// BumpPushMeter counts one push for the tenant today.
func (s *Store) BumpPushMeter(ctx context.Context, tenantID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO git_push_meters (tenant_id, day, pushes) VALUES ($1, current_date, 1)
		ON CONFLICT (tenant_id, day) DO UPDATE SET pushes = git_push_meters.pushes + 1`,
		tenantID)
	return err
}

// CreateNativeGitToken inserts a Quill-native git token row with its hash.
func (s *Store) CreateNativeGitToken(ctx context.Context, userID uuid.UUID, name, tokenHash string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO git_tokens (user_id, name, forgejo_token_name, token_hash)
		VALUES ($1, $2, '', $3) RETURNING id`, userID, name, tokenHash).Scan(&id)
	return id, err
}
