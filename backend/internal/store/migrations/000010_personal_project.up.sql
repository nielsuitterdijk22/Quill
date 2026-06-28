-- Mark projects that back a user's personal namespace (slug = username).
-- is_personal distinguishes them from team/org projects so the platform layer
-- can apply different Forgejo ownership rules (user namespace vs. org namespace).
ALTER TABLE projects ADD COLUMN is_personal boolean NOT NULL DEFAULT false;
