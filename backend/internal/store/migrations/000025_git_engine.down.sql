DROP TRIGGER git_repo_state_set_updated_at ON git_repo_state;
DROP TABLE git_repo_state;
ALTER TABLE tenants DROP COLUMN runtime_hint;
