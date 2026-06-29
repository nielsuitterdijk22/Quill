"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { useQuillAuth } from "@/components/auth/context";

type CancelButtonProps = {
  project: string;
  repo: string;
  number: number;
};

export function CancelButton({ project, repo, number }: CancelButtonProps) {
  const router = useRouter();
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  const { getToken } = useQuillAuth();

  function cancel() {
    setError(null);
    startTransition(async () => {
      try {
        const token = await getToken();
        const res = await fetch(
          `/api/backend/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}/pipelines/runs/${number}/cancel`,
          {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              ...(token ? { Authorization: `Bearer ${token}` } : {}),
            },
            body: "{}",
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
      {error && <div className="form-error" style={{ marginTop: 8 }}>{error}</div>}
      <button
        type="button"
        className="btn"
        onClick={cancel}
        disabled={pending}
      >
        {pending ? "Cancelling…" : "✕ Cancel run"}
      </button>
    </>
  );
}
