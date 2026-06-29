"use client";

import { useState, useTransition } from "react";
import { useQuillAuth } from "@/components/auth/context";

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
  const { getToken } = useQuillAuth();

  function toggle() {
    const next = !starred;
    startTransition(async () => {
      const token = await getToken();
      const res = await fetch(
        `/api/backend/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}/star`,
        {
          method: next ? "PUT" : "DELETE",
          headers: token ? { Authorization: `Bearer ${token}` } : {},
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
