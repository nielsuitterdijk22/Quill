"use client";

import { useState } from "react";

import type { OrgMember, OrgInvitation } from "../../lib/api";

async function apiPost(
  path: string,
  body: unknown,
): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    const res = await fetch(`/api/backend${path}`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (!res.ok) {
      const data = (await res.json().catch(() => null)) as { message?: string } | null;
      return { ok: false, error: data?.message ?? `Error ${res.status}` };
    }
    return { ok: true };
  } catch {
    return { ok: false, error: "Could not reach the backend." };
  }
}

async function apiDelete(
  path: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    const res = await fetch(`/api/backend${path}`, {
      method: "DELETE",
      credentials: "include",
    });
    if (!res.ok) {
      const data = (await res.json().catch(() => null)) as { message?: string } | null;
      return { ok: false, error: data?.message ?? `Error ${res.status}` };
    }
    return { ok: true };
  } catch {
    return { ok: false, error: "Could not reach the backend." };
  }
}

function roleLabel(role: string): string {
  if (role === "org:admin") return "Admin";
  if (role === "org:member") return "Member";
  return role;
}

export function OrgPanel({
  members: initialMembers,
  invitations: initialInvitations,
  membersError,
  invitationsError,
}: {
  members: OrgMember[];
  invitations: OrgInvitation[];
  membersError?: string;
  invitationsError?: string;
}) {
  const [members, setMembers] = useState<OrgMember[]>(initialMembers);
  const [invitations, setInvitations] = useState<OrgInvitation[]>(initialInvitations);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState("org:member");
  const [inviting, setInviting] = useState(false);
  const [removing, setRemoving] = useState<string | null>(null);
  const [revoking, setRevoking] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  async function handleInvite() {
    setError(null);
    setSuccess(null);
    const trimmed = email.trim();
    if (!trimmed) return;
    setInviting(true);
    const res = await apiPost("/org/members", { email: trimmed, role });
    setInviting(false);
    if (res.ok) {
      setEmail("");
      setSuccess(`Invitation sent to ${trimmed}.`);
      // Append optimistic invitation
      setInvitations((prev) => [
        ...prev,
        { invitationId: `pending-${Date.now()}`, email: trimmed, role, status: "pending" },
      ]);
    } else {
      setError(res.error);
    }
  }

  async function handleRemove(membershipId: string) {
    setError(null);
    setSuccess(null);
    setRemoving(membershipId);
    const res = await apiDelete(`/org/members/${membershipId}`);
    setRemoving(null);
    if (res.ok) {
      setMembers((prev) => prev.filter((m) => m.membershipId !== membershipId));
    } else {
      setError(res.error);
    }
  }

  async function handleRevoke(invitationId: string) {
    setError(null);
    setSuccess(null);
    setRevoking(invitationId);
    const res = await apiDelete(`/org/invitations/${invitationId}`);
    setRevoking(null);
    if (res.ok) {
      setInvitations((prev) => prev.filter((i) => i.invitationId !== invitationId));
    } else {
      setError(res.error);
    }
  }

  return (
    <div className="panel form-narrow">
      <h2>Organisation members</h2>
      <div className="readme-body">
        <p className="subtle">
          Invite teammates to join your organisation. Invited users will receive
          an email from Clerk and join automatically when they sign in.
        </p>

        {error && <div className="form-error">{error}</div>}
        {success && <div className="form-success">{success}</div>}

        <div className="ssh-key-form">
          <input
            type="email"
            className="token-name-input"
            placeholder="teammate@example.com"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            aria-label="Email address to invite"
          />
          <select
            className="token-name-input"
            value={role}
            onChange={(e) => setRole(e.target.value)}
            aria-label="Role"
          >
            <option value="org:member">Member</option>
            <option value="org:admin">Admin</option>
          </select>
          <button
            type="button"
            className="btn primary"
            onClick={handleInvite}
            disabled={inviting || !email.trim()}
          >
            {inviting ? "Sending…" : "Send invite"}
          </button>
        </div>

        {membersError ? (
          <div className="form-error">Could not load members: {membersError}</div>
        ) : members.length === 0 ? (
          <p className="subtle">No members yet.</p>
        ) : (
          <>
            <h3 className="section-subheader">Members</h3>
            <ul className="token-list">
              {members.map((m) => (
                <li key={m.membershipId} className="token-row">
                  <div className="token-meta">
                    <span className="token-name">{m.displayName || m.email}</span>
                    <span className="subtle">
                      {m.email} · {roleLabel(m.role)}
                    </span>
                  </div>
                  <button
                    type="button"
                    className="btn ghost danger"
                    onClick={() => handleRemove(m.membershipId)}
                    disabled={removing === m.membershipId}
                  >
                    {removing === m.membershipId ? "Removing…" : "Remove"}
                  </button>
                </li>
              ))}
            </ul>
          </>
        )}

        {!invitationsError && invitations.length > 0 && (
          <>
            <h3 className="section-subheader">Pending invitations</h3>
            <ul className="token-list">
              {invitations.map((inv) => (
                <li key={inv.invitationId} className="token-row">
                  <div className="token-meta">
                    <span className="token-name">{inv.email}</span>
                    <span className="subtle">{roleLabel(inv.role)}</span>
                  </div>
                  <button
                    type="button"
                    className="btn ghost danger"
                    onClick={() => handleRevoke(inv.invitationId)}
                    disabled={revoking === inv.invitationId || inv.invitationId.startsWith("pending-")}
                  >
                    {revoking === inv.invitationId ? "Revoking…" : "Revoke"}
                  </button>
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
    </div>
  );
}
