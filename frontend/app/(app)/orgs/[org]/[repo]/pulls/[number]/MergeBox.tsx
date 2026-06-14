"use client";

import { useFormState, useFormStatus } from "react-dom";

import type { PolicyGate } from "../../../../../../lib/api";
import { mergeAction, type MergeState } from "./actions";

const initialState: MergeState = {};

function MergeButton({ blocked }: { blocked: boolean }) {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending || blocked}>
      {pending ? "Merging…" : "Merge pull request"}
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

// MergeBox lets a member merge an open PR with a chosen strategy. When a branch
// policy blocks the merge, the button is disabled and the reason is shown (the
// backend enforces the gate regardless).
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
  const blocked = gate.applies && gate.blocked;

  return (
    <div className="panel merge-box">
      <GateRow gate={gate} />
      <div className="merge-head">
        <span className={`merge-dot ${mergeable ? "ok" : "warn"}`} />
        <strong>
          {mergeable
            ? "This branch has no conflicts with the base branch."
            : "This pull request may have conflicts."}
        </strong>
      </div>
      {state.error && <div className="form-error">{state.error}</div>}
      <form className="merge-form" action={formAction}>
        <select name="method" defaultValue="merge">
          <option value="merge">Create a merge commit</option>
          <option value="squash">Squash and merge</option>
          <option value="rebase">Rebase and merge</option>
        </select>
        <MergeButton blocked={blocked} />
      </form>
    </div>
  );
}
