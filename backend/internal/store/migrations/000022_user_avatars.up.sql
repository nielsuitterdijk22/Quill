-- Profile pictures. Stored in Postgres (max ~2 MiB enforced by the API, not
-- the schema) and served by the api publicly at /users/{id}/avatar so <img>
-- tags work without a bearer. One row per user: uploading replaces it.
CREATE TABLE user_avatars (
    user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    media_type text NOT NULL,
    data bytea NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
