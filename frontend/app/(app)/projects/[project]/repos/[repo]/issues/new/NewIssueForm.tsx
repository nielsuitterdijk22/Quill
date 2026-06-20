"use client";

import Link from "next/link";
import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";

import { repoBase } from "../../../../../../../components/repo";

export function NewIssueForm({
  project,
  repo,
}: {
  project: string;
  repo: string;
}) {
  const base = repoBase(project, repo);
  const router = useRouter();
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();

  function handleSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = e.currentTarget;
    const title = (form.elements.namedItem("title") as HTMLInputElement).value.trim();
    const body = (form.elements.namedItem("body") as HTMLTextAreaElement).value.trim();
    if (!title) {
      setError("Title is required.");
      return;
    }
    setError(null);
    startTransition(async () => {
      try {
        const res = await fetch(
          `/api/backend/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}/issues`,
          {
            method: "POST",
            credentials: "include",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ title, body }),
          },
        );
        const data = (await res.json().catch(() => null)) as {
          issue?: { number?: number };
          message?: string;
        } | null;
        if (!res.ok) {
          setError(data?.message ?? `Error ${res.status}`);
          return;
        }
        const num = data?.issue?.number;
        if (num) {
          router.push(`${base}/issues/${num}`);
        } else {
          router.push(`${base}/issues`);
        }
      } catch {
        setError("Could not reach the backend.");
      }
    });
  }

  return (
    <div className="panel form-narrow">
      <h2>New issue</h2>
      {error && <div className="form-error">{error}</div>}
      <form onSubmit={handleSubmit}>
        <label>
          Title
          <input
            name="title"
            type="text"
            placeholder="Short summary of the issue"
            required
            autoFocus
          />
        </label>
        <label>
          Description
          <textarea
            name="body"
            rows={10}
            placeholder="Describe the issue in detail…"
          />
        </label>
        <div className="form-actions">
          <button className="btn primary" type="submit" disabled={pending}>
            {pending ? "Creating…" : "Submit issue"}
          </button>
          <Link className="btn" href={`${base}/issues`}>
            Cancel
          </Link>
        </div>
      </form>
    </div>
  );
}
