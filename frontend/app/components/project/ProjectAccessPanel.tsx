"use client";

import { useState, useTransition } from "react";

import type { ProjectAccess } from "../../lib/api";
import {
  removeGroupAction,
  removeUserAction,
  setGroupRoleAction,
  setUserRoleAction,
} from "./accessActions";

const ROLES = ["owner", "member", "read"] as const;

// ProjectAccessPanel lists who can reach a project — people and groups — and
// lets an owner change roles. Organization membership alone grants nothing.
export function ProjectAccessPanel({
  project,
  access,
}: {
  project: string;
  access: ProjectAccess;
}) {
  const [pending, start] = useTransition();
  const [error, setError] = useState("");
  const [username, setUsername] = useState("");
  const [userRole, setUserRole] = useState("member");
  const [group, setGroup] = useState("");
  const [groupRole, setGroupRole] = useState("member");

  const run = (
    fn: () => Promise<{ ok: true } | { ok: false; error: string }>,
    after?: () => void,
  ) =>
    start(async () => {
      setError("");
      const res = await fn();
      if (!res.ok) setError(res.error);
      else after?.();
    });

  return (
    <div>
      {error && <div className="form-error">{error}</div>}

      <h3 className="settings-subtitle">People</h3>
      <ul className="token-list">
        {access.users.map((u) => (
          <li key={u.userId} className="token-row">
            <div className="token-meta">
              <span className="token-name">{u.displayName || u.username}</span>
              <span className="subtle mono">{u.username}</span>
            </div>
            <span className="group-row-actions">
              <select
                className="ob-input"
                value={u.role}
                disabled={pending}
                onChange={(e) => run(() => setUserRoleAction(project, u.username, e.target.value))}
              >
                {ROLES.map((r) => (
                  <option key={r} value={r}>{r}</option>
                ))}
              </select>
              <button className="btn ghost danger" disabled={pending} onClick={() => run(() => removeUserAction(project, u.userId))}>
                Remove
              </button>
            </span>
          </li>
        ))}
      </ul>
      <form
        className="group-inline-form"
        onSubmit={(e) => {
          e.preventDefault();
          if (username.trim()) run(() => setUserRoleAction(project, username, userRole), () => setUsername(""));
        }}
      >
        <input className="ob-input" placeholder="Username of an organization member" value={username} onChange={(e) => setUsername(e.target.value)} />
        <select className="ob-input" value={userRole} onChange={(e) => setUserRole(e.target.value)}>
          {ROLES.map((r) => (
            <option key={r} value={r}>{r}</option>
          ))}
        </select>
        <button className="btn primary" disabled={pending || !username.trim()}>Grant access</button>
      </form>

      <h3 className="settings-subtitle">Groups</h3>
      {access.groups.length === 0 ? (
        <p className="subtle">No group has access.</p>
      ) : (
        <ul className="token-list">
          {access.groups.map((g) => (
            <li key={g.groupId} className="token-row">
              <div className="token-meta">
                <span className="token-name">{g.name}</span>
                <span className="subtle mono">{g.slug}</span>
              </div>
              <span className="group-row-actions">
                <select
                  className="ob-input"
                  value={g.role}
                  disabled={pending}
                  onChange={(e) => run(() => setGroupRoleAction(project, g.slug, e.target.value))}
                >
                  {ROLES.map((r) => (
                    <option key={r} value={r}>{r}</option>
                  ))}
                </select>
                <button className="btn ghost danger" disabled={pending} onClick={() => run(() => removeGroupAction(project, g.slug))}>
                  Remove
                </button>
              </span>
            </li>
          ))}
        </ul>
      )}
      <form
        className="group-inline-form"
        onSubmit={(e) => {
          e.preventDefault();
          if (group.trim()) run(() => setGroupRoleAction(project, group, groupRole), () => setGroup(""));
        }}
      >
        <input className="ob-input" placeholder="Group slug" value={group} onChange={(e) => setGroup(e.target.value)} />
        <select className="ob-input" value={groupRole} onChange={(e) => setGroupRole(e.target.value)}>
          {ROLES.map((r) => (
            <option key={r} value={r}>{r}</option>
          ))}
        </select>
        <button className="btn primary" disabled={pending || !group.trim()}>Grant access</button>
      </form>
    </div>
  );
}
