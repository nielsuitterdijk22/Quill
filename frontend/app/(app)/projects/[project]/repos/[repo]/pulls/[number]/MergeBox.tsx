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

// GateRow summarizes the branch policy verdict above the merge controls. When the
// composed gate blocks, it lists each scope-tagged denial so the reason is clear.
function GateRow({ gate }: { gate: PolicyGate }) {
  if (!gate.applies) return null;
  const cls = gate.blocked ? "warn" : "ok";
  const denials = gate.denials ?? [];
  return (
    <div className={`gate-row ${cls}`}>
      <span className={`merge-dot ${cls}`} />
      <span>
        <strong>
          {gate.blocked ? "Merging is blocked" : "Branch policy satisfied"}
        </strong>
        {gate.blocked ? (
          denials.length > 0 ? (
            <ul className="gate-denials">
              {denials.map((d, i) => (
                <li key={i}>
                  {d.message}
                  <span className="subtle">
                    {" "}
                    · <span className="mono">{d.scope}</span> policy on{" "}
                    <span className="mono">{d.selector}</span>
                  </span>
                </li>
              ))}
            </ul>
          ) : (
            <span className="subtle"> · {gate.reason}</span>
          )
        ) : (
          <span className="subtle">
            {" "}
            · {gate.approvals} of {gate.requiredApprovals} required approvals on{" "}
            <span className="mono">{gate.pattern}</span>
          </span>
        )}
      </span>
    </div>
  );
}

// ChecksRow shows the CI pipeline status for the PR's head commit when the branch
// policy requires status checks. Absent runs are neutral (CI isn't mandatory until
// at least one run exists); failed runs block; all-pass is green.
function ChecksRow({ gate }: { gate: PolicyGate }) {
  if (!gate.requireStatusChecks) return null;
  const count = gate.checkCount ?? 0;
  const pass = gate.allChecksPass ?? true;
  let cls: string;
  let label: string;
  if (count === 0) {
    cls = "neutral";
    label = "No CI runs recorded for this commit yet";
  } else if (pass) {
    cls = "ok";
    label = `${count} CI check${count === 1 ? "" : "s"} passed`;
  } else {
    cls = "warn";
    label = `CI checks did not all pass (${count} run${count === 1 ? "" : "s"})`;
  }
  return (
    <div className={`gate-row ${cls}`}>
      <span className={`merge-dot ${cls}`} />
      <span>
        <strong>{label}</strong>
      </span>
    </div>
  );
}

// MergeBox lets a member merge an open PR. The merge control sits top-right; the
// button opens a popup to pick the merge strategy. When a branch policy blocks
// the merge, the button is disabled and the reason is shown (the backend enforces
// the gate regardless).
export function MergeBox({
  project,
  repo,
  number,
  mergeable,
  gate,
}: {
  project: string;
  repo: string;
  number: number;
  mergeable: boolean;
  gate: PolicyGate;
}) {
  const action = mergeAction.bind(null, project, repo, number);
  const [state, formAction] = useFormState(action, initialState);
  const [open, setOpen] = useState(false);
  const blocked = gate.applies && gate.blocked;

  return (
    <div className="panel merge-box">
      <div className="merge-bar">
        <div className="merge-status">
          <GateRow gate={gate} />
          <ChecksRow gate={gate} />
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
