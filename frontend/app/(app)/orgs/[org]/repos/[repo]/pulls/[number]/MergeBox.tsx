"use client";

import { useState } from "react";
import { useFormState, useFormStatus } from "react-dom";

import type { PolicyGate } from "../../../../../../../lib/api";
import { mergeAction, type MergeState } from "./actions";

const initialState: MergeState = {};

const METHODS: { value: string; label: string; hint: string }[] = [
  {
    value: "merge",
    label: "Create a merge commit",
    hint: "All commits from this branch are added to the base via a merge commit.",
  },
  {
    value: "squash",
    label: "Squash and merge",
    hint: "The commits are combined into one commit on the base branch.",
  },
  {
    value: "rebase",
    label: "Rebase and merge",
    hint: "The commits are rebased and added individually onto the base branch.",
  },
];

// MergeOption is one strategy in the merge popup; submitting it merges with that
// method. The form's method field carries the chosen strategy.
function MergeOption({
  value,
  label,
  hint,
}: {
  value: string;
  label: string;
  hint: string;
}) {
  const { pending } = useFormStatus();
  return (
    <button
      className="merge-option"
      type="submit"
      name="method"
      value={value}
      disabled={pending}
    >
      <span className="merge-option-label">{label}</span>
      <span className="merge-option-hint">{hint}</span>
    </button>
  );
}

// GateRow summarizes the branch policy verdict above the merge controls.
function GateRow({ gate }: { gate: PolicyGate }) {
  if (!gate.applies) return null;
  const cls = gate.blocked ? "warn" : "ok";
  const detail = gate.blocked
    ? gate.reason
    : `${gate.approvals} of ${gate.requiredApprovals} required approvals`;
  return (
    <div className={`gate-row ${cls}`}>
      <span className={`merge-dot ${cls}`} />
      <span>
        <strong>
          {gate.blocked ? "Merging is blocked" : "Branch policy satisfied"}
        </strong>
        <span className="subtle">
          {" "}
          · {detail} on <span className="mono">{gate.pattern}</span>
        </span>
      </span>
    </div>
  );
}

// MergeBox lets a member merge an open PR. The merge control sits top-right; the
// button opens a popup to pick the merge strategy. When a branch policy blocks
// the merge, the button is disabled and the reason is shown (the backend enforces
// the gate regardless).
export function MergeBox({
  org,
  repo,
  number,
  mergeable,
  gate,
}: {
  org: string;
  repo: string;
  number: number;
  mergeable: boolean;
  gate: PolicyGate;
}) {
  const action = mergeAction.bind(null, org, repo, number);
  const [state, formAction] = useFormState(action, initialState);
  const [open, setOpen] = useState(false);
  const blocked = gate.applies && gate.blocked;

  return (
    <div className="panel merge-box">
      <div className="merge-bar">
        <div className="merge-status">
          <GateRow gate={gate} />
          <div className="merge-head">
            <span className={`merge-dot ${mergeable ? "ok" : "warn"}`} />
            <strong>
              {mergeable
                ? "This branch has no conflicts with the base branch."
                : "This pull request may have conflicts."}
            </strong>
          </div>
        </div>
        <div className="merge-actions">
          <button
            className="btn primary"
            type="button"
            disabled={blocked}
            aria-expanded={open}
            aria-haspopup="menu"
            onClick={() => setOpen((o) => !o)}
          >
            Merge pull request ▾
          </button>
          {open && !blocked && (
            <>
              <button
                type="button"
                className="merge-backdrop"
                aria-label="Close merge menu"
                onClick={() => setOpen(false)}
              />
              <form className="merge-menu" action={formAction} role="menu">
                {METHODS.map((m) => (
                  <MergeOption
                    key={m.value}
                    value={m.value}
                    label={m.label}
                    hint={m.hint}
                  />
                ))}
              </form>
            </>
          )}
        </div>
      </div>
      {state.error && <div className="form-error">{state.error}</div>}
    </div>
  );
}
