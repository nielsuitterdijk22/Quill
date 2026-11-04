"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";

import type { OrgGroup, OrgMember } from "../../lib/api";
import {
  addGroupMemberAction,
  createGroupAction,
  deleteGroupAction,
  setGroupProjectRoleAction,
} from "./actions";

// OrgGroups is the client surface for an org's nested group tree (departments
// → teams, max depth 5). Org admins can create/delete groups, add members, and
// grant a group a role on a project — members of subgroups inherit ancestor
// grants. Plain members get a read-only tree.
export function OrgGroups({
  org,
  canEdit,
  groups,
  members,
}: {
  org: string;
  canEdit: boolean;
  groups: OrgGroup[];
  members: OrgMember[];
}) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);
  const [slug, setSlug] = useState("");
  const [name, setName] = useState("");
  const [parentSlug, setParentSlug] = useState("");
  const [memberGroup, setMemberGroup] = useState("");
  const [memberUser, setMemberUser] = useState("");
  const [grantGroup, setGrantGroup] = useState("");
  const [grantProject, setGrantProject] = useState("");
  const [grantRole, setGrantRole] = useState("member");

  const sorted = [...groups].sort((a, b) => a.depth - b.depth || a.slug.localeCompare(b.slug));
  const byId = new Map(groups.map((g) => [g.id, g]));

  function run(fn: () => Promise<{ ok: true } | { ok: false; error: string }>) {
    setError(null);
    startTransition(async () => {
      const res = await fn();
      if (!res.ok) {
        setError(res.error);
        return;
      }
      router.refresh();
    });
  }

  function submitCreate(e: React.FormEvent) {
    e.preventDefault();
    if (!slug.trim() || !name.trim()) {
      setError("Slug and name are required.");
      return;
    }
    run(async () => {
      const res = await createGroupAction(org, slug.trim(), name.trim(), parentSlug);
      if (res.ok) {
        setSlug("");
        setName("");
        setParentSlug("");
      }
      return res;
    });
  }

  return (
    <div className="org-members">
      {sorted.length === 0 && (
        <p className="subtle">No groups yet. Create a top-level group (a department) below.</p>
      )}
      {sorted.length > 0 && (
        <table className="policy-table">
          <thead>
            <tr>
              <th>Group</th>
              <th>Slug</th>
              <th>Depth</th>
              <th>Parent</th>
              {canEdit && <th></th>}
            </tr>
          </thead>
          <tbody>
            {sorted.map((g) => (
              <tr key={g.id}>
                <td style={{ paddingLeft: `${(g.depth - 1) * 20 + 8}px` }}>
                  {g.depth > 1 ? "└ " : ""}
                  {g.name}
                </td>
                <td className="mono">{g.slug}</td>
                <td>{g.depth}</td>
                <td className="mono subtle">
                  {g.parentId ? byId.get(g.parentId)?.slug ?? "?" : "—"}
                </td>
                {canEdit && (
                  <td className="policy-row-actions">
                    <button
                      className="btn danger small"
                      disabled={pending}
                      onClick={() => run(() => deleteGroupAction(org, g.slug))}
                    >
                      Delete
                    </button>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {canEdit && (
        <>
          <h3 className="settings-subtitle">Create group</h3>
          <form className="org-invite-form" onSubmit={submitCreate}>
            <input
              className="ob-input"
              placeholder="slug (e.g. fraud-team)"
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
            />
            <input
              className="ob-input"
              placeholder="Display name"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            <select
              className="ob-input"
              value={parentSlug}
              onChange={(e) => setParentSlug(e.target.value)}
            >
              <option value="">— top level (department) —</option>
              {sorted.map((g) => (
                <option key={g.id} value={g.slug} disabled={g.depth >= 5}>
                  {"  ".repeat(g.depth - 1)}
                  {g.slug}
                  {g.depth >= 5 ? " (max depth)" : ""}
                </option>
              ))}
            </select>
            <button className="btn primary" type="submit" disabled={pending}>
              Create group
            </button>
          </form>

          <h3 className="settings-subtitle">Add member to group</h3>
          <form
            className="org-invite-form"
            onSubmit={(e) => {
              e.preventDefault();
              if (!memberGroup || !memberUser) return;
              run(async () => {
                const res = await addGroupMemberAction(org, memberGroup, memberUser);
                if (res.ok) {
                  setMemberGroup("");
                  setMemberUser("");
                }
                return res;
              });
            }}
          >
            <select
              className="ob-input"
              value={memberGroup}
              onChange={(e) => setMemberGroup(e.target.value)}
            >
              <option value="">— group —</option>
              {sorted.map((g) => (
                <option key={g.id} value={g.slug}>
                  {g.slug}
                </option>
              ))}
            </select>
            <select
              className="ob-input"
              value={memberUser}
              onChange={(e) => setMemberUser(e.target.value)}
            >
              <option value="">— member —</option>
              {members.map((m) => (
                <option key={m.userId} value={m.userId}>
                  {m.username} ({m.email})
                </option>
              ))}
            </select>
            <button className="btn primary" type="submit" disabled={pending || !memberGroup || !memberUser}>
              Add
            </button>
          </form>

          <h3 className="settings-subtitle">Grant group access to a project</h3>
          <form
            className="org-invite-form"
            onSubmit={(e) => {
              e.preventDefault();
              if (!grantGroup || !grantProject.trim()) return;
              run(async () => {
                const res = await setGroupProjectRoleAction(
                  org,
                  grantGroup,
                  grantProject.trim(),
                  grantRole,
                );
                if (res.ok) setGrantProject("");
                return res;
              });
            }}
          >
            <select
              className="ob-input"
              value={grantGroup}
              onChange={(e) => setGrantGroup(e.target.value)}
            >
              <option value="">— group —</option>
              {sorted.map((g) => (
                <option key={g.id} value={g.slug}>
                  {g.slug}
                </option>
              ))}
            </select>
            <input
              className="ob-input"
              placeholder="project slug"
              value={grantProject}
              onChange={(e) => setGrantProject(e.target.value)}
            />
            <select
              className="ob-input"
              value={grantRole}
              onChange={(e) => setGrantRole(e.target.value)}
            >
              <option value="member">member</option>
              <option value="owner">owner</option>
            </select>
            <button className="btn primary" type="submit" disabled={pending || !grantGroup || !grantProject.trim()}>
              Grant
            </button>
          </form>
        </>
      )}

      {error && <p className="form-error">{error}</p>}
      <p className="subtle">
        Members of a group inherit the roles granted to its ancestor groups, so a
        department-level grant covers every team beneath it. Max nesting depth: 5.
      </p>
    </div>
  );
}
