"use client";

import { useState, useTransition } from "react";

import type { User } from "../../../lib/api";

export function UserRow({ user }: { user: User }) {
  const [active, setActive] = useState(user.isActive);
  const [resetError, setResetError] = useState<string | null>(null);
  const [resetDone, setResetDone] = useState(false);
  const [newPw, setNewPw] = useState("");
  const [showReset, setShowReset] = useState(false);
  const [pendingActive, startActiveTransition] = useTransition();
  const [pendingReset, startResetTransition] = useTransition();

  function toggleActive() {
    startActiveTransition(async () => {
      const res = await fetch(
        `/api/backend/admin/users/${encodeURIComponent(user.username)}/active`,
        {
          method: "PATCH",
          credentials: "include",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ active: !active }),
        },
      );
      if (res.ok) setActive(!active);
    });
  }

  function handleReset(e: React.FormEvent) {
    e.preventDefault();
    setResetError(null);
    setResetDone(false);
    startResetTransition(async () => {
      try {
        const res = await fetch(
          `/api/backend/admin/users/${encodeURIComponent(user.username)}/reset-password`,
          {
            method: "POST",
            credentials: "include",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ newPassword: newPw }),
          },
        );
        if (!res.ok) {
          const data = (await res.json().catch(() => null)) as { message?: string } | null;
          setResetError(data?.message ?? `Error ${res.status}`);
          return;
        }
        setResetDone(true);
        setNewPw("");
        setShowReset(false);
      } catch {
        setResetError("Could not reach the backend.");
      }
    });
  }

  return (
    <div className="row-item" style={{ flexDirection: "column", alignItems: "stretch", gap: 8 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <div style={{ flex: 1 }}>
          <span className="nm">{user.displayName || user.username}</span>
          <span className="sub"> @{user.username} · {user.email}</span>
          {user.isAdmin && <span className="badge success" style={{ marginLeft: 8 }}>admin</span>}
          <span className={`badge ${active ? "success" : "neutral"}`} style={{ marginLeft: 4 }}>
            {active ? "active" : "disabled"}
          </span>
        </div>
        <button
          type="button"
          className="btn"
          onClick={toggleActive}
          disabled={pendingActive}
        >
          {active ? "Disable" : "Enable"}
        </button>
        <button
          type="button"
          className="btn"
          onClick={() => { setShowReset((s) => !s); setResetError(null); setResetDone(false); }}
        >
          Reset password
        </button>
      </div>
      {showReset && (
        <form onSubmit={handleReset} style={{ display: "flex", gap: 8, alignItems: "center" }}>
          <input
            type="password"
            value={newPw}
            onChange={(e) => setNewPw(e.target.value)}
            placeholder="New password"
            required
            minLength={8}
            style={{ flex: 1 }}
          />
          <button className="btn primary" type="submit" disabled={pendingReset}>
            {pendingReset ? "Saving…" : "Set password"}
          </button>
          <button className="btn" type="button" onClick={() => setShowReset(false)}>
            Cancel
          </button>
        </form>
      )}
      {resetError && <div className="form-error">{resetError}</div>}
      {resetDone && <div className="banner" style={{ color: "var(--success)" }}>Password updated.</div>}
    </div>
  );
}
