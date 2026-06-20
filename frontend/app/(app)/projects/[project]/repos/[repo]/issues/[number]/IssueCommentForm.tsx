"use client";

import { useState, useTransition, useRef } from "react";
import { useRouter } from "next/navigation";

export function IssueCommentForm({
  project,
  repo,
  number,
}: {
  project: string;
  repo: string;
  number: number;
}) {
  const router = useRouter();
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  const ref = useRef<HTMLTextAreaElement>(null);

  function handleSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const body = ref.current?.value.trim() ?? "";
    if (!body) return;
    setError(null);
    startTransition(async () => {
      try {
        const res = await fetch(
          `/api/backend/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}/issues/${number}/comments`,
          {
            method: "POST",
            credentials: "include",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ body }),
          },
        );
        if (!res.ok) {
          const data = (await res.json().catch(() => null)) as { message?: string } | null;
          setError(data?.message ?? `Error ${res.status}`);
          return;
        }
        if (ref.current) ref.current.value = "";
        router.refresh();
      } catch {
        setError("Could not reach the backend.");
      }
    });
  }

  return (
    <div className="panel">
      <h3>Leave a comment</h3>
      {error && <div className="form-error">{error}</div>}
      <form onSubmit={handleSubmit}>
        <textarea
          ref={ref}
          rows={5}
          placeholder="Write a comment…"
          required
        />
        <div className="form-actions">
          <button className="btn primary" type="submit" disabled={pending}>
            {pending ? "Posting…" : "Comment"}
          </button>
        </div>
      </form>
    </div>
  );
}
