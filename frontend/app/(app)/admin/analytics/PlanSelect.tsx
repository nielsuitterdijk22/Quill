"use client";

import { useState, useTransition } from "react";

import { setPlanAction } from "./actions";

// No free tier: personal accounts are inactive (locked), starter, pro, or the
// hand-granted free "beta"; organizations are inactive, team, lapsed, beta or
// vendor plans. "internal" = no billing at all (our own / test orgs).
const PERSONAL = ["inactive", "starter", "pro", "beta", "internal"];
const ORG = ["inactive", "team", "lapsed", "beta", "enterprise", "internal"];

export function PlanSelect({ slug, plan, personal }: { slug: string; plan: string; personal: boolean }) {
  const [value, setValue] = useState(plan);
  const [error, setError] = useState<string | null>(null);
  const [pending, start] = useTransition();
  const options = personal ? PERSONAL : ORG;
  return (
    <span>
      <select
        aria-label={`Plan for ${slug}`}
        value={value}
        disabled={pending}
        onChange={(e) => {
          const next = e.target.value;
          const prev = value;
          setValue(next);
          setError(null);
          start(async () => {
            const res = await setPlanAction(slug, next);
            if (!res.ok) {
              setValue(prev);
              setError(res.error);
            }
          });
        }}
      >
        {(options.includes(plan) ? options : [plan, ...options]).map((p) => (
          <option key={p} value={p}>
            {p}
          </option>
        ))}
      </select>
      {error && <span className="subtle"> {error}</span>}
    </span>
  );
}
