"use client";

import Link from "next/link";
import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";

import { repoBase } from "../../../../../../../components/repo";

// NewIssueForm is a two-pane editor (H5 shell): the ticket draft on the left
// with generous title/body fields, a context rail on the right (what makes a
// good issue + what happens next). Submission keeps the same POST /issues
// {title, body} contract.
export function NewIssueForm({
  project,
  repo,
}: {
  project: string;
  repo: string;
}) {
  const base = repoBase(project, repo);
  const router = useRouter();
  const [title, setTitle] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();

  function handleSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = e.currentTarget;
    const body = (form.elements.namedItem("body") as HTMLTextAreaElement).value.trim();
    const trimmed = title.trim();
    if (!trimmed) {
      setError("A title is required before the issue can be opened.");
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
            body: JSON.stringify({ title: trimmed, body }),
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
        router.push(num ? `${base}/issues/${num}` : `${base}/issues`);
      } catch {
        setError("Could not reach the backend.");
      }
    });
  }

  return (
    <div className="new-issue-grid">
      <form className="panel new-issue-editor" onSubmit={handleSubmit}>
        {error && <div className="form-error">{error}</div>}
        <div className="field">
          <span>Title</span>
          <input
            name="title"
            type="text"
            className="new-issue-title"
            placeholder="Something is broken — one clear sentence"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            autoFocus
            required
          />
        </div>
        <div className="field">
          <span>Description · Markdown supported</span>
          <textarea
            name="body"
            rows={12}
            placeholder={"What happened?\nWhat did you expect?\nSteps to reproduce, logs, screenshots…"}
          />
        </div>
        <div className="new-issue-actions">
          <Link className="btn ghost" href={`${base}/issues`}>
            Cancel
          </Link>
          <button className="btn primary" type="submit" disabled={pending || !title.trim()}>
            {pending ? "Opening issue…" : "Open issue"}
          </button>
        </div>
      </form>

      <aside className="new-issue-rail">
        <div className="panel">
          <h2>What makes a good issue</h2>
          <ul className="new-issue-tips">
            <li>
              <b>One issue, one thing.</b> A separate ticket for each problem keeps
              the fix reviewable.
            </li>
            <li>
              <b>Reproduce it.</b> Steps, expected vs. actual behaviour, and the
              branch or commit where you saw it.
            </li>
            <li>
              <b>Attach evidence.</b> Logs, screenshots or failing test output -
              Markdown formats them.
            </li>
          </ul>
        </div>
        <div className="panel">
          <h2>What happens next</h2>
          <p className="new-issue-note">
            The issue gets a number and shows up in the repository&apos;s Issues list,
            where maintainers can pick it up, comment and link it to pull
            requests.
          </p>
        </div>
      </aside>
    </div>
  );
}
