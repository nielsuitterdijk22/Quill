"use client";

import { useState, useTransition } from "react";

type ReRunButtonProps = {
  project: string;
  repo: string;
  workflowPath: string;
  ref: string;
};

export function ReRunButton({ project, repo, workflowPath, ref }: ReRunButtonProps) {
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();

  function reRun() {
    setError(null);
    startTransition(async () => {
      try {
        const res = await fetch(
          `/api/backend/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}/pipelines`,
          {
            method: "POST",
            credentials: "include",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ workflow: workflowPath, ref }),
          },
        );
        const body = (await res.json().catch(() => null)) as {
          run?: { runNumber?: number; workflowPath?: string };
          message?: string;
        } | null;
        if (!res.ok) {
          setError(body?.message ?? `Error ${res.status}`);
          return;
        }
        const newRun = body?.run;
        if (newRun?.runNumber && newRun?.workflowPath) {
          window.location.href = `/projects/${project}/repos/${repo}/pipelines/runs/${newRun.runNumber}?workflow=${encodeURIComponent(newRun.workflowPath)}`;
        } else {
          window.location.href = `/projects/${project}/repos/${repo}/pipelines`;
        }
      } catch {
        setError("Could not reach the backend.");
      }
    });
  }

  return (
    <>
      {error && <div className="form-error" style={{ marginTop: 8 }}>{error}</div>}
      <button
        type="button"
        className="btn"
        onClick={reRun}
        disabled={pending}
      >
        {pending ? "Re-running…" : "↺ Re-run"}
      </button>
    </>
  );
}
