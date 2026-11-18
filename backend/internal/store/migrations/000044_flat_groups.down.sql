-- The tree itself is not restorable (parent links were cleared); put the
-- ancestor-expanding resolver back. Grants copied onto former subgroups stay
-- (they are ordinary explicit grants).
CREATE OR REPLACE FUNCTION project_access(p_user uuid, p_project uuid DEFAULT NULL)
RETURNS TABLE (project_id uuid, role text)
LANGUAGE sql STABLE AS $$
  WITH RECURSIVE my_groups AS (
    SELECT gm.group_id FROM group_members gm WHERE gm.user_id = p_user
    UNION
    SELECT g.parent_id
    FROM groups g
    JOIN my_groups mg ON g.id = mg.group_id
    WHERE g.parent_id IS NOT NULL
  ),
  grants AS (
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
    SELECT gpr.project_id, gpr.role
    FROM group_project_roles gpr
    JOIN my_groups mg ON mg.group_id = gpr.group_id
    JOIN groups g ON g.id = gpr.group_id
    JOIN projects p ON p.id = gpr.project_id AND p.tenant_id = g.tenant_id
    JOIN tenant_members tm ON tm.tenant_id = p.tenant_id AND tm.user_id = p_user
    WHERE p_project IS NULL OR gpr.project_id = p_project
    UNION ALL
    SELECT p.id, CASE tm.role WHEN 'admin' THEN 'owner' ELSE 'member' END
    FROM tenant_members tm
    JOIN projects p ON p.tenant_id = tm.tenant_id
    WHERE tm.user_id = p_user AND (p_project IS NULL OR p.id = p_project)
  )
  SELECT DISTINCT ON (gr.project_id) gr.project_id, gr.role
  FROM grants gr
  ORDER BY gr.project_id,
           CASE gr.role WHEN 'owner' THEN 3 WHEN 'member' THEN 2 WHEN 'read' THEN 1 ELSE 0 END DESC
$$;
