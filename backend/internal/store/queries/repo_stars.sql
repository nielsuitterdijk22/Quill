-- name: StarRepo :exec
INSERT INTO repo_stars (user_id, repo_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: UnstarRepo :exec
DELETE FROM repo_stars
WHERE user_id = $1 AND repo_id = $2;

-- name: GetRepoStar :one
SELECT user_id, repo_id, created_at FROM repo_stars
WHERE user_id = $1 AND repo_id = $2;

-- name: CountRepoStars :one
SELECT COUNT(*) FROM repo_stars
WHERE repo_id = $1;
