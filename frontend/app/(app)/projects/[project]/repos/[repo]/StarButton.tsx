"use client";

import { useState, useTransition } from "react";

export function StarButton({
  project,
  repo,
  initialStarred,
  initialCount,
}: {
  project: string;
  repo: string;
  initialStarred: boolean;
  initialCount: number;
}) {
  const [starred, setStarred] = useState(initialStarred);
  const [count, setCount] = useState(initialCount);
  const [pending, startTransition] = useTransition();

  function toggle() {
    const next = !starred;
    startTransition(async () => {
      const res = await fetch(
        `/api/backend/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}/star`,
        {
          method: next ? "PUT" : "DELETE",
          credentials: "include",
        },
      );
      if (res.ok) {
        setStarred(next);
        setCount((c) => c + (next ? 1 : -1));
      }
    });
  }

  return (
    <button
      type="button"
      className={`pill star-btn${starred ? " starred" : ""}`}
      onClick={toggle}
      disabled={pending}
      aria-pressed={starred}
      aria-label={starred ? "Unstar repository" : "Star repository"}
    >
      {starred ? "★" : "☆"} {count > 0 ? count : "Star"}
    </button>
  );
}
