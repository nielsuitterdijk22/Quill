-- Explicit project access. Organization membership no longer implies access to
-- the organization's projects: access is a direct grant (project_members) or a
-- group grant, both still live only for a current org member (see 000043).
--
-- Backfill first so every existing org keeps working: each current org member
-- receives the direct row the implicit rule used to synthesize (admin -> owner,
-- member -> member), never weakening a stronger row they already hold.
INSERT INTO project_members (project_id, user_id, role)
SELECT p.id, tm.user_id, CASE tm.role WHEN 'admin' THEN 'owner' ELSE 'member' END
FROM tenant_members tm
JOIN tenants t ON t.id = tm.tenant_id AND t.kind = 'org'
JOIN projects p ON p.tenant_id = tm.tenant_id
ON CONFLICT (project_id, user_id) DO UPDATE
  SET role = EXCLUDED.role
  WHERE (CASE project_members.role WHEN 'owner' THEN 3 WHEN 'member' THEN 2 WHEN 'read' THEN 1 ELSE 0 END)
      < (CASE EXCLUDED.role WHEN 'owner' THEN 3 WHEN 'member' THEN 2 WHEN 'read' THEN 1 ELSE 0 END);

CREATE OR REPLACE FUNCTION project_access(p_user uuid, p_project uuid DEFAULT NULL)
RETURNS TABLE (project_id uuid, role text)
LANGUAGE sql STABLE AS $$
  WITH grants AS (
    -- direct membership
    SELECT pm.project_id, pm.role
    FROM project_members pm
    JOIN projects p ON p.id = pm.project_id
    JOIN tenants t ON t.id = p.tenant_id
    WHERE pm.user_id = p_user
      AND (p_project IS NULL OR pm.project_id = p_project)
      AND (t.kind <> 'org' OR EXISTS (
            SELECT 1 FROM tenant_members tm
            WHERE tm.tenant_id = p.tenant_id AND tm.user_id = p_user))
    UNION ALL
    -- group grants (a group only ever grants within its own tenant)
    SELECT gpr.project_id, gpr.role
    FROM group_members gm
    JOIN groups g ON g.id = gm.group_id
    JOIN group_project_roles gpr ON gpr.group_id = g.id
    JOIN projects p ON p.id = gpr.project_id AND p.tenant_id = g.tenant_id
    JOIN tenant_members tm ON tm.tenant_id = p.tenant_id AND tm.user_id = p_user
    WHERE gm.user_id = p_user AND (p_project IS NULL OR gpr.project_id = p_project)
  )
  SELECT DISTINCT ON (gr.project_id) gr.project_id, gr.role
  FROM grants gr
  ORDER BY gr.project_id,
           CASE gr.role WHEN 'owner' THEN 3 WHEN 'member' THEN 2 WHEN 'read' THEN 1 ELSE 0 END DESC
$$;
