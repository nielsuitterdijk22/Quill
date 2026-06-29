"use client";

import { useState, useTransition } from "react";
import { useQuillAuth } from "@/components/auth/context";

type ReRunButtonProps = {
  project: string;
  repo: string;
  workflowPath: string;
  gitRef: string;
};

export function ReRunButton({ project, repo, workflowPath, gitRef }: ReRunButtonProps) {
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  const { getToken } = useQuillAuth();

  function reRun() {
    setError(null);
    startTransition(async () => {
      try {
        const token = await getToken();
        const res = await fetch(
          `/api/backend/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}/pipelines`,
          {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              ...(token ? { Authorization: `Bearer ${token}` } : {}),
            },
            body: JSON.stringify({ workflow: workflowPath, ref: gitRef }),
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
