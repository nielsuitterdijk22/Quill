"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";

export function IssueActions({
  project,
  repo,
  number,
  state,
}: {
  project: string;
  repo: string;
  number: number;
  state: string;
}) {
  const router = useRouter();
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();

  function toggleState() {
    const newState = state === "open" ? "closed" : "open";
    setError(null);
    startTransition(async () => {
      try {
        const res = await fetch(
          `/api/backend/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}/issues/${number}`,
          {
            method: "PATCH",
            credentials: "include",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ state: newState }),
          },
        );
        if (!res.ok) {
          const data = (await res.json().catch(() => null)) as { message?: string } | null;
          setError(data?.message ?? `Error ${res.status}`);
          return;
        }
        router.refresh();
      } catch {
        setError("Could not reach the backend.");
      }
    });
  }

  return (
    <>
      {error && <div className="form-error">{error}</div>}
      <button
        type="button"
        className="btn"
        onClick={toggleState}
        disabled={pending}
      >
        {pending
          ? "Updating…"
          : state === "open"
          ? "✓ Close issue"
          : "◍ Reopen issue"}
      </button>
    </>
  );
}
