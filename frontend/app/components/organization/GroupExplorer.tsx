"use client";

import Link from "next/link";
import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";

import type { GroupMember, GroupProjectAccess, OrgGroup, OrgMember, OrgProjectRef } from "../../lib/api";
import {
  addGroupMemberAction,
  createGroupAction,
  deleteGroupAction,
  removeGroupMemberAction,
  removeGroupProjectRoleAction,
  setGroupProjectRoleAction,
} from "./actions";

const MAX_DEPTH = 5;

function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 63);
}

type Result = { ok: true } | { ok: false; error: string };

// GroupExplorer is the organization's group management: the group tree on
// the left; the selected group's members, project access and subgroups on the
// right, each editable in place. Members of a subgroup inherit every project
// role granted to its ancestors.
export function GroupExplorer({
  org,
  canEdit,
  groups,
  members,
  projects,
  selected,
  selectedMembers,
  selectedProjects,
}: {
  org: string;
  canEdit: boolean;
  groups: OrgGroup[];
  members: OrgMember[];
  projects: OrgProjectRef[];
  selected: OrgGroup | null;
  selectedMembers: GroupMember[];
  selectedProjects: GroupProjectAccess[];
}) {
  const router = useRouter();
  const [pending, start] = useTransition();
  const [error, setError] = useState<string | null>(null);
  const [newName, setNewName] = useState("");
  const [subName, setSubName] = useState("");
  const [addUser, setAddUser] = useState("");
  const [grantProject, setGrantProject] = useState("");
  const [grantRole, setGrantRole] = useState("member");

  const base = `/orgs/${encodeURIComponent(org)}/settings/groups`;
  const byId = new Map(groups.map((g) => [g.id, g]));
  const children = (id: string | null) =>
    groups.filter((g) => (g.parentId ?? null) === id).sort((a, b) => a.name.localeCompare(b.name));

  function run(fn: () => Promise<Result>, after?: () => void) {
    setError(null);
    start(async () => {
      const res = await fn();
      if (!res.ok) {
        setError(res.error);
        return;
      }
      after?.();
      router.refresh();
    });
  }

  function create(name: string, parent: string, reset: () => void) {
    const slug = slugify(name);
    if (!slug) {
      setError("Give the group a name.");
      return;
    }
    run(() => createGroupAction(org, slug, name.trim(), parent), () => {
      reset();
      router.push(`${base}?group=${encodeURIComponent(slug)}`);
    });
  }

  function Tree({ parent }: { parent: string | null }) {
    const list = children(parent);
    if (list.length === 0) return null;
    return (
      <ul className="group-tree">
        {list.map((g) => (
          <li key={g.id}>
            <Link
              href={`${base}?group=${encodeURIComponent(g.slug)}`}
              className={`group-node${selected?.id === g.id ? " active" : ""}`}
            >
              {g.name}
            </Link>
            <Tree parent={g.id} />
          </li>
        ))}
      </ul>
    );
  }

  const path: OrgGroup[] = [];
  for (let g = selected; g; g = g.parentId ? byId.get(g.parentId) ?? null : null) path.unshift(g);
  const memberIds = new Set(selectedMembers.map((m) => m.userId));
  const granted = new Set(selectedProjects.map((p) => p.project));
  const subgroups = selected ? children(selected.id) : [];

  return (
    <div className="group-explorer">
      <aside className="group-explorer-tree">
        {groups.length === 0 ? (
          <p className="subtle">No groups yet.</p>
        ) : (
          <Tree parent={null} />
        )}
        {canEdit && (
          <form
            className="group-inline-form"
            onSubmit={(e) => {
              e.preventDefault();
              create(newName, "", () => setNewName(""));
            }}
          >
            <input className="ob-input" placeholder="New group (e.g. Engineering)" value={newName} onChange={(e) => setNewName(e.target.value)} />
            <button className="btn" type="submit" disabled={pending || !newName.trim()}>
              Add
            </button>
          </form>
        )}
      </aside>

      <div className="group-explorer-detail">
        {error && <div className="form-error">{error}</div>}
        {!selected ? (
          <p className="subtle">
            Groups model departments and teams. Grant a group a role on a project and every member — including members
            of its subgroups — gets that access.
          </p>
        ) : (
          <>
            <div className="group-detail-head">
              <div>
                <div className="subtle">
                  {path.slice(0, -1).map((g) => (
                    <span key={g.id}>
                      <Link href={`${base}?group=${encodeURIComponent(g.slug)}`}>{g.name}</Link> ›{" "}
                    </span>
                  ))}
                </div>
                <h3 className="group-detail-title">{selected.name}</h3>
              </div>
              {canEdit && (
                <button
                  className="btn danger small"
                  disabled={pending}
                  onClick={() => {
                    if (!window.confirm(`Delete ${selected.name}${subgroups.length ? " and its subgroups" : ""}?`)) return;
                    run(() => deleteGroupAction(org, selected.slug), () => router.push(base));
                  }}
                >
                  Delete group
                </button>
              )}
            </div>

            <h4 className="settings-subtitle">Members ({selectedMembers.length})</h4>
            {selectedMembers.length === 0 ? (
              <p className="subtle">No direct members.</p>
            ) : (
              <ul className="token-list">
                {selectedMembers.map((m) => (
                  <li key={m.userId} className="token-row">
                    <div className="token-meta">
                      <span className="token-name">{m.displayName || m.username}</span>
                      <span className="subtle">{m.email}</span>
                    </div>
                    {canEdit && (
                      <button className="btn ghost danger" disabled={pending} onClick={() => run(() => removeGroupMemberAction(org, selected.slug, m.userId))}>
                        Remove
                      </button>
                    )}
                  </li>
                ))}
              </ul>
            )}
            {canEdit && (
              <form
                className="group-inline-form"
                onSubmit={(e) => {
                  e.preventDefault();
                  if (addUser) run(() => addGroupMemberAction(org, selected.slug, addUser), () => setAddUser(""));
                }}
              >
                <select className="ob-input" value={addUser} onChange={(e) => setAddUser(e.target.value)}>
                  <option value="">Add a member…</option>
                  {members
                    .filter((m) => !memberIds.has(m.userId))
                    .map((m) => (
                      <option key={m.userId} value={m.userId}>
                        {m.displayName || m.username} ({m.email})
                      </option>
                    ))}
                </select>
                <button className="btn" type="submit" disabled={pending || !addUser}>
                  Add
                </button>
              </form>
            )}

            <h4 className="settings-subtitle">Project access</h4>
            {selectedProjects.length === 0 ? (
              <p className="subtle">This group isn&rsquo;t granted any project yet.</p>
            ) : (
              <ul className="token-list">
                {selectedProjects.map((p) => (
                  <li key={p.project} className="token-row">
                    <div className="token-meta">
                      <span className="token-name">{projects.find((x) => x.slug === p.project)?.name ?? p.project}</span>
                      <span className="subtle mono">{p.project}</span>
                    </div>
                    {canEdit ? (
                      <span className="group-row-actions">
                        <select
                          className="ob-input"
                          value={p.role}
                          disabled={pending}
                          onChange={(e) => run(() => setGroupProjectRoleAction(org, selected.slug, p.project, e.target.value))}
                        >
                          <option value="member">member</option>
                          <option value="owner">owner</option>
                        </select>
                        <button className="btn ghost danger" disabled={pending} onClick={() => run(() => removeGroupProjectRoleAction(org, selected.slug, p.project))}>
                          Remove
                        </button>
                      </span>
                    ) : (
                      <span className="badge">{p.role}</span>
                    )}
                  </li>
                ))}
              </ul>
            )}
            {canEdit && (
              <form
                className="group-inline-form"
                onSubmit={(e) => {
                  e.preventDefault();
                  if (grantProject) run(() => setGroupProjectRoleAction(org, selected.slug, grantProject, grantRole), () => setGrantProject(""));
                }}
              >
                <select className="ob-input" value={grantProject} onChange={(e) => setGrantProject(e.target.value)}>
                  <option value="">Grant access to a project…</option>
                  {projects
                    .filter((p) => !granted.has(p.slug))
                    .map((p) => (
                      <option key={p.slug} value={p.slug}>
                        {p.name}
                      </option>
                    ))}
                </select>
                <select className="ob-input" value={grantRole} onChange={(e) => setGrantRole(e.target.value)}>
                  <option value="member">member</option>
                  <option value="owner">owner</option>
                </select>
                <button className="btn" type="submit" disabled={pending || !grantProject}>
                  Grant
                </button>
              </form>
            )}
            {selectedProjects.length > 0 && subgroups.length > 0 && (
              <p className="subtle">Members of its subgroups inherit this access.</p>
            )}

            <h4 className="settings-subtitle">Subgroups</h4>
            {subgroups.length === 0 ? (
              <p className="subtle">None.</p>
            ) : (
              <p>
                {subgroups.map((g) => (
                  <Link key={g.id} className="badge" href={`${base}?group=${encodeURIComponent(g.slug)}`}>
                    {g.name}
                  </Link>
                ))}
              </p>
            )}
            {canEdit && selected.depth < MAX_DEPTH && (
              <form
                className="group-inline-form"
                onSubmit={(e) => {
                  e.preventDefault();
                  create(subName, selected.slug, () => setSubName(""));
                }}
              >
                <input className="ob-input" placeholder={`New subgroup of ${selected.name}`} value={subName} onChange={(e) => setSubName(e.target.value)} />
                <button className="btn" type="submit" disabled={pending || !subName.trim()}>
                  Add subgroup
                </button>
              </form>
            )}
          </>
        )}
      </div>
    </div>
  );
}
