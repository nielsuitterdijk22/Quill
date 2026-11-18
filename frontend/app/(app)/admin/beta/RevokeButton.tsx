"use client";

import { useState, useTransition } from "react";

import { revokeBetaInviteAction } from "./actions";

export function RevokeButton({ id }: { id: string }) {
  const [error, setError] = useState<string | null>(null);
  const [pending, start] = useTransition();
  return (
    <span>
      <button
        className="btn danger"
        type="button"
        disabled={pending}
        onClick={() =>
          start(async () => {
            const res = await revokeBetaInviteAction(id);
            if (!res.ok) setError(res.error);
          })
        }
      >
        {pending ? "Revoking…" : "Revoke"}
      </button>
      {error && <span className="subtle"> {error}</span>}
    </span>
  );
}
