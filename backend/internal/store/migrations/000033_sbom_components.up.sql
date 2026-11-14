-- SBOM component index (2026-11-14): one row per component of every ready
-- SBOM, extracted from the stored CycloneDX document, so components can be
-- searched across all repositories a user can see without parsing documents
-- per request. Rows are written by store.FinishSBOM (same extraction as the
-- backfill below) and die with their SBOM.
CREATE TABLE sbom_components (
    sbom_id uuid NOT NULL REFERENCES sboms(id) ON DELETE CASCADE,
    repo_id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    name text NOT NULL,
    version text NOT NULL DEFAULT '',
    ecosystem text NOT NULL DEFAULT '',
    license text NOT NULL DEFAULT '',
    purl text NOT NULL DEFAULT '',
    location text NOT NULL DEFAULT ''
);

CREATE INDEX sbom_components_sbom_idx ON sbom_components (sbom_id);
CREATE INDEX sbom_components_name_idx ON sbom_components (lower(name) text_pattern_ops);

INSERT INTO sbom_components (sbom_id, repo_id, tenant_id, name, version, ecosystem, license, purl, location)
SELECT s.id, s.repo_id, s.tenant_id,
    CASE WHEN COALESCE(c->>'group', '') <> '' THEN (c->>'group') || '/' || COALESCE(c->>'name', '')
         ELSE COALESCE(c->>'name', '') END,
    COALESCE(c->>'version', ''),
    COALESCE(substring(c->>'purl' from '^pkg:([^/]+)/'), c->>'type', 'other'),
    COALESCE((SELECT string_agg(COALESCE(l->>'expression', l->'license'->>'id', l->'license'->>'name'), ' OR ')
              FROM jsonb_array_elements(CASE WHEN jsonb_typeof(c->'licenses') = 'array' THEN c->'licenses' ELSE '[]'::jsonb END) l), ''),
    COALESCE(c->>'purl', ''),
    COALESCE((SELECT p->>'value'
              FROM jsonb_array_elements(CASE WHEN jsonb_typeof(c->'properties') = 'array' THEN c->'properties' ELSE '[]'::jsonb END) p
              WHERE p->>'name' = 'syft:location:0:path' LIMIT 1), '')
FROM sboms s
CROSS JOIN LATERAL (SELECT convert_from(s.document, 'UTF8')::jsonb AS d) doc
CROSS JOIN LATERAL jsonb_array_elements(
    CASE WHEN jsonb_typeof(doc.d->'components') = 'array' THEN doc.d->'components' ELSE '[]'::jsonb END) c
WHERE s.status = 'ready' AND s.document IS NOT NULL AND COALESCE(c->>'name', '') <> '';
