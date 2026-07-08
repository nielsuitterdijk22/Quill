"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";

import type { OrgInvite, OrgMember } from "../../lib/api";
import {
  inviteMemberAction,
  removeMemberAction,
  revokeInviteAction,
  setMemberRoleAction,
} from "./actions";

// OrgMembers is the client surface for an organization's roster and pending
// invitations. Admins can invite by email (the returned accept link is shown to
// copy — and, when Zitadel is configured, also emailed), change a member's role,
// remove members, and revoke invites. Plain members get a read-only roster.
export function OrgMembers({
  org,
  canEdit,
  members,
  invites,
}: {
  org: string;
  canEdit: boolean;
  members: OrgMember[];
  invites: OrgInvite[];
}) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState("member");
  const [inviteLink, setInviteLink] = useState<string | null>(null);
  const [emailed, setEmailed] = useState(false);
  const [copied, setCopied] = useState(false);

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

  function submitInvite(e: React.FormEvent) {
    e.preventDefault();
    if (!email.trim()) {
      setError("Enter an email address.");
      return;
    }
    setError(null);
    setInviteLink(null);
    startTransition(async () => {
      const res = await inviteMemberAction(org, email, role);
      if (!res.ok) {
        setError(res.error);
        return;
      }
      setInviteLink(`${window.location.origin}/invite/${res.token}`);
      setEmailed(res.emailedByIdp);
      setEmail("");
      setCopied(false);
      router.refresh();
    });
  }

  function copyLink() {
    if (!inviteLink) return;
    navigator.clipboard?.writeText(inviteLink).then(
      () => {
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
      },
      () => {},
    );
  }

  return (
    <div className="org-members">
      {error && <div className="form-error">{error}</div>}

      {canEdit && (
        <form className="org-invite-form" onSubmit={submitInvite}>
          <input
            type="email"
            className="ob-input"
            placeholder="teammate@company.com"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
          <select value={role} onChange={(e) => setRole(e.target.value)}>
            <option value="member">Member</option>
            <option value="admin">Admin</option>
          </select>
          <button className="btn primary" type="submit" disabled={pending}>
            {pending ? "Sending…" : "Invite"}
          </button>
        </form>
      )}

      {inviteLink && (
        <div className="org-invite-link banner">
          <div className="subtle">
            {emailed
              ? "Invitation emailed. You can also share this link:"
              : "Share this invite link (it's shown once):"}
          </div>
          <div className="org-invite-link-row">
            <code className="mono">{inviteLink}</code>
            <button type="button" className="btn ghost small" onClick={copyLink}>
              {copied ? "Copied" : "Copy"}
            </button>
          </div>
        </div>
      )}

      <table className="policy-table">
        <thead>
          <tr>
            <th>Member</th>
            <th>Role</th>
            {canEdit && <th />}
          </tr>
        </thead>
        <tbody>
          {members.map((m) => (
            <tr key={m.userId}>
              <td>
                <div>{m.displayName || m.username}</div>
                <div className="subtle mono">{m.email}</div>
              </td>
              <td>
                {canEdit ? (
                  <select
                    value={m.role}
                    disabled={pending}
                    onChange={(e) =>
                      run(() => setMemberRoleAction(org, m.userId, e.target.value))
                    }
                  >
                    <option value="member">Member</option>
                    <option value="admin">Admin</option>
                  </select>
                ) : (
                  <span className="badge accent">{m.role}</span>
                )}
              </td>
              {canEdit && (
                <td className="policy-row-actions">
                  <button
                    type="button"
                    className="btn danger small"
                    disabled={pending}
                    onClick={() => run(() => removeMemberAction(org, m.userId))}
                  >
                    Remove
                  </button>
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>

      {canEdit && invites.length > 0 && (
        <>
          <h3 className="settings-subtitle">Pending invitations</h3>
          <table className="policy-table">
            <thead>
              <tr>
                <th>Email</th>
                <th>Role</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {invites.map((iv) => (
                <tr key={iv.id}>
                  <td className="mono">{iv.email}</td>
                  <td>
                    <span className="badge accent">{iv.role}</span>
                  </td>
                  <td className="policy-row-actions">
                    <button
                      type="button"
                      className="btn ghost small"
                      disabled={pending}
                      onClick={() => run(() => revokeInviteAction(org, iv.id))}
                    >
                      Revoke
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </div>
  );
}
