-- name: CreateInvite :one
INSERT INTO org_invites (tenant_id, email, role, token_hash, invited_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, tenant_id, email, role, token_hash, status, invited_by, accepted_user_id, expires_at, created_at, accepted_at;

-- name: GetInviteByTokenHash :one
SELECT id, tenant_id, email, role, token_hash, status, invited_by, accepted_user_id, expires_at, created_at, accepted_at
FROM org_invites WHERE token_hash = $1;

-- name: ListPendingInvitesByTenant :many
SELECT id, tenant_id, email, role, token_hash, status, invited_by, accepted_user_id, expires_at, created_at, accepted_at
FROM org_invites
WHERE tenant_id = $1 AND status = 'pending'
ORDER BY created_at DESC;

-- name: RevokeInvite :execrows
UPDATE org_invites SET status = 'revoked'
WHERE id = $1 AND tenant_id = $2 AND status = 'pending';

-- name: RevokePendingInvitesByEmail :exec
UPDATE org_invites SET status = 'revoked'
WHERE tenant_id = $1 AND lower(email) = lower($2) AND status = 'pending';

-- name: MarkInviteAccepted :exec
UPDATE org_invites SET status = 'accepted', accepted_user_id = $2, accepted_at = now()
WHERE id = $1;
