-- Groups are flat: a group is a named set of people in an organization, and a
-- grant on a project is (group, project, role). Nothing inherits through a tree.
--
-- Before dropping the idea, make it lossless: roles that used to reach a member
-- of a subgroup through an ancestor are copied onto the subgroup itself (the
-- strongest role wins where the subgroup already had its own grant), so no one's
-- access changes. Then the tree is cleared (parent_id NULL, depth 1): the
-- self-referencing ON DELETE CASCADE must not take former children with a
-- deleted group once nesting is no longer visible. The columns stay for now (a
-- previous revision still reads them while a deploy rolls out); a later
-- migration drops them. Safe to run twice.

WITH RECURSIVE lineage(group_id, ancestor_id) AS (
  SELECT id, parent_id FROM groups WHERE parent_id IS NOT NULL
  UNION
  SELECT l.group_id, g.parent_id
  FROM lineage l
  JOIN groups g ON g.id = l.ancestor_id
  WHERE g.parent_id IS NOT NULL
)
INSERT INTO group_project_roles (group_id, project_id, role)
SELECT l.group_id, r.project_id,
       CASE WHEN bool_or(r.role = 'owner')  THEN 'owner'
            WHEN bool_or(r.role = 'member') THEN 'member'
            ELSE 'read' END
FROM lineage l
JOIN group_project_roles r ON r.group_id = l.ancestor_id
GROUP BY l.group_id, r.project_id
ON CONFLICT (group_id, project_id) DO UPDATE
SET role = CASE
  WHEN (CASE EXCLUDED.role WHEN 'owner' THEN 3 WHEN 'member' THEN 2 WHEN 'read' THEN 1 ELSE 0 END)
     > (CASE group_project_roles.role WHEN 'owner' THEN 3 WHEN 'member' THEN 2 WHEN 'read' THEN 1 ELSE 0 END)
  THEN EXCLUDED.role ELSE group_project_roles.role END;

UPDATE groups SET parent_id = NULL, depth = 1 WHERE parent_id IS NOT NULL OR depth <> 1;

-- project_access without the ancestor expansion (see migration 000043 for the
-- invariant: every grant is live only for a current member of the org).
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
    UNION ALL
    -- membership of the project's organization
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
