"use client";

import { useState } from "react";
import { useFormState, useFormStatus } from "react-dom";

import type { BranchPolicy } from "../../lib/api";
import {
  deletePolicyAction,
  savePolicyAction,
  type PolicyFormState,
  type PolicyTarget,
} from "./actions";

const initial: PolicyFormState = {};

function SaveButton({ editing }: { editing: boolean }) {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Saving…" : editing ? "Update policy" : "Add policy"}
    </button>
  );
}

function DeleteButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn danger small" type="submit" disabled={pending}>
      {pending ? "Removing…" : "Delete"}
    </button>
  );
}

// DeletePolicyForm is a one-button form that removes a single policy at the
// component's scope.
function DeletePolicyForm({
  target,
  pattern,
}: {
  target: PolicyTarget;
  pattern: string;
}) {
  const action = deletePolicyAction.bind(null, target);
  const [, formAction] = useFormState(action, initial);
  return (
    <form action={formAction} className="inline-form">
      <input type="hidden" name="pattern" value={pattern} />
      <DeleteButton />
    </form>
  );
}

function flags(p: BranchPolicy): string[] {
  const out: string[] = [];
  if (p.requirePullRequest) out.push("PR required");
  out.push(
    `${p.requiredApprovals} approval${p.requiredApprovals === 1 ? "" : "s"}`,
  );
  if (p.blockForcePush) out.push("No force-push");
  if (p.dismissStaleApprovals) out.push("Dismiss stale");
  if (p.requireUpToDate) out.push("Up-to-date");
  if (p.requireStatusChecks) out.push("CI required");
  return out;
}

function FlagTags({ policy }: { policy: BranchPolicy }) {
  return (
    <div className="policy-flags">
      {flags(policy).map((f) => (
        <span className="tag" key={f}>
          {f}
        </span>
      ))}
    </div>
  );
}

// InheritedTable lists policies a scope receives from broader scopes. They are
// read-only here; each is edited where it is declared.
function InheritedTable({ policies }: { policies: BranchPolicy[] }) {
  return (
    <table className="policy-table">
      <thead>
        <tr>
          <th>Branch pattern</th>
          <th>Protections</th>
          <th>Inherited from</th>
        </tr>
      </thead>
      <tbody>
        {policies.map((p) => (
          <tr key={`${p.scope}:${p.pattern}`}>
            <td className="mono">{p.pattern}</td>
            <td>
              <FlagTags policy={p} />
            </td>
            <td>
              <span className="badge accent">{p.scope}</span>
              {p.locked && (
                <span className="badge amber policy-lock">🔒 locked</span>
              )}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

// PolicyManager renders the branch policies a scope owns (editable) plus the
// ones it inherits from broader scopes (read-only). target selects the scope so
// the same component backs repo, project, and tenant settings. canLock enables
// the "lock" toggle, which only makes sense at project and tenant scope.
export function PolicyManager({
  target,
  policies,
  inherited = [],
  branchNames = [],
  defaultBranch = "main",
  canLock = false,
}: {
  target: PolicyTarget;
  policies: BranchPolicy[];
  inherited?: BranchPolicy[];
  branchNames?: string[];
  defaultBranch?: string;
  canLock?: boolean;
}) {
  const action = savePolicyAction.bind(null, target);
  const [state, formAction] = useFormState(action, initial);
  const [editing, setEditing] = useState<BranchPolicy | null>(null);

  const blank: BranchPolicy = {
    pattern: defaultBranch,
    requiredApprovals: 1,
    requirePullRequest: true,
    blockForcePush: true,
    dismissStaleApprovals: false,
    requireUpToDate: false,
    requireStatusChecks: false,
    locked: false,
    updatedAt: "",
  };
  const current = editing ?? blank;
  const formKey = editing ? editing.pattern : "new";

  return (
    <div className="policy-area">
      {inherited.length > 0 && (
        <div className="policy-inherited">
          <p className="subtle">
            Inherited rules apply here and cannot be edited at this level. A
            locked rule is a floor — you may add stricter policies, never weaker
            ones.
          </p>
          <InheritedTable policies={inherited} />
        </div>
      )}

      {policies.length > 0 ? (
        <table className="policy-table">
          <thead>
            <tr>
              <th>Branch pattern</th>
              <th>Protections</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {policies.map((p) => (
              <tr key={p.pattern}>
                <td className="mono">
                  {p.pattern}
                  {p.locked && (
                    <span className="badge amber policy-lock">🔒 locked</span>
                  )}
                </td>
                <td>
                  <FlagTags policy={p} />
                </td>
                <td className="policy-row-actions">
                  <button
                    type="button"
                    className="btn ghost small"
                    onClick={() => setEditing(p)}
                  >
                    Edit
                  </button>
                  <DeletePolicyForm target={target} pattern={p.pattern} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <div className="empty">
          No branch policies yet. Add one below to require reviews before
          merging.
        </div>
      )}

      <div className="panel form-narrow policy-form-panel">
        <h3>{editing ? `Edit ${editing.pattern}` : "Add a branch policy"}</h3>
        {state.error && <div className="form-error">{state.error}</div>}
        <form action={formAction} key={formKey}>
          <label className="field">
            <span>Branch pattern</span>
            <input
              name="pattern"
              defaultValue={current.pattern}
              readOnly={!!editing}
              list="branch-name-list"
              placeholder="main or release/*"
              required
            />
            <datalist id="branch-name-list">
              {branchNames.map((n) => (
                <option key={n} value={n} />
              ))}
            </datalist>
          </label>
          <label className="field">
            <span>Required approvals</span>
            <input
              name="requiredApprovals"
              type="number"
              min={0}
              defaultValue={current.requiredApprovals}
            />
          </label>
          <label className="check">
            <input
              type="checkbox"
              name="requirePullRequest"
              defaultChecked={current.requirePullRequest}
            />
            <span>Require a pull request before merging</span>
          </label>
          <label className="check">
            <input
              type="checkbox"
              name="blockForcePush"
              defaultChecked={current.blockForcePush}
            />
            <span>Block force-pushes to matching branches</span>
          </label>
          <label className="check">
            <input
              type="checkbox"
              name="dismissStaleApprovals"
              defaultChecked={current.dismissStaleApprovals}
            />
            <span>Dismiss stale approvals when new commits are pushed</span>
          </label>
          <label className="check">
            <input
              type="checkbox"
              name="requireUpToDate"
              defaultChecked={current.requireUpToDate}
            />
            <span>Require the branch to be up to date before merging</span>
          </label>
          <label className="check">
            <input
              type="checkbox"
              name="requireStatusChecks"
              defaultChecked={current.requireStatusChecks}
            />
            <span>Require CI pipeline checks to pass before merging</span>
          </label>
          {canLock && (
            <label className="check">
              <input
                type="checkbox"
                name="locked"
                defaultChecked={current.locked}
              />
              <span>
                Lock this rule so narrower scopes can only make it stricter
              </span>
            </label>
          )}
          <div className="form-actions">
            <SaveButton editing={!!editing} />
            {editing && (
              <button
                type="button"
                className="btn ghost"
                onClick={() => setEditing(null)}
              >
                Cancel
              </button>
            )}
          </div>
        </form>
      </div>
    </div>
  );
}
