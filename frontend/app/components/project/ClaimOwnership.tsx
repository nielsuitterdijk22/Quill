"use client";

import { useRouter } from "next/navigation";
import { useState, useTransition } from "react";

import { claimOwnershipAction } from "./accessActions";

// ClaimOwnership is the organization admin's break-glass: it adds them as an
// owner of a project they cannot otherwise open. The action is audited.
export function ClaimOwnership({ project }: { project: string }) {
  const router = useRouter();
  const [pending, start] = useTransition();
  const [error, setError] = useState("");
  return (
    <div>
      {error && <div className="form-error">{error}</div>}
      <button
        className="btn"
        disabled={pending}
        onClick={() => {
          if (!window.confirm("Add yourself as an owner of this project? This is recorded in the audit log.")) return;
          start(async () => {
            const res = await claimOwnershipAction(project);
            if (!res.ok) setError(res.error);
            else router.refresh();
          });
        }}
      >
        I administer this organization — add me as owner
      </button>
    </div>
  );
}
