-- Create the separate database Forgejo uses. Quill metadata lives in the
-- POSTGRES_DB database; Forgejo gets its own so the two never collide.
-- Runs once on first cluster init (Postgres docker-entrypoint-initdb.d).
SELECT 'CREATE DATABASE forgejo'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'forgejo')\gexec

GRANT ALL PRIVILEGES ON DATABASE forgejo TO quill;
