"use client";

import { useState } from "react";
import { useFormState, useFormStatus } from "react-dom";

import type { EnvironmentPolicy } from "../../lib/api";
import type { PolicyFormState, PolicyTarget } from "./actions";
import {
  deleteEnvironmentPolicyAction,
  saveEnvironmentPolicyAction,
} from "./environmentActions";

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

// DeletePolicyForm is a one-button form that removes a single environment policy
// at the component's scope.
function DeletePolicyForm({
  target,
  pattern,
}: {
  target: PolicyTarget;
  pattern: string;
}) {
  const action = deleteEnvironmentPolicyAction.bind(null, target);
  const [, formAction] = useFormState(action, initial);
  return (
    <form action={formAction} className="inline-form">
      <input type="hidden" name="pattern" value={pattern} />
      <DeleteButton />
    </form>
  );
}

function flags(p: EnvironmentPolicy): string[] {
  const out: string[] = [];
  out.push(
    `${p.requiredApprovals} approval${p.requiredApprovals === 1 ? "" : "s"}`,
  );
  if (p.allowedSourceBranches.length > 0) {
    out.push(`from ${p.allowedSourceBranches.join(", ")}`);
  }
  if (p.requirePreviousEnvironment) {
    out.push(`after ${p.requirePreviousEnvironment}`);
  }
  if (p.requireSuccessfulRun) out.push("Green run");
  if (p.minWaitMinutes > 0) out.push(`Wait ${p.minWaitMinutes}m`);
  return out;
}

function FlagTags({ policy }: { policy: EnvironmentPolicy }) {
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

// InheritedTable lists environment policies a scope receives from broader scopes.
// They are read-only here; each is edited where it is declared.
function InheritedTable({ policies }: { policies: EnvironmentPolicy[] }) {
  return (
    <table className="policy-table">
      <thead>
        <tr>
          <th>Environment</th>
          <th>Gate</th>
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

// EnvironmentPolicyManager renders the environment (deploy-gate) policies a scope
// owns (editable) plus the ones it inherits from broader scopes (read-only).
// target selects the scope so the same component backs repo, project, and tenant
// settings. canLock enables the "lock" toggle, which only makes sense at project
// and tenant scope.
export function EnvironmentPolicyManager({
  target,
  policies,
  inherited = [],
  canLock = false,
}: {
  target: PolicyTarget;
  policies: EnvironmentPolicy[];
  inherited?: EnvironmentPolicy[];
  canLock?: boolean;
}) {
  const action = saveEnvironmentPolicyAction.bind(null, target);
  const [state, formAction] = useFormState(action, initial);
  const [editing, setEditing] = useState<EnvironmentPolicy | null>(null);

  const blank: EnvironmentPolicy = {
    pattern: "production",
    requiredApprovals: 1,
    allowedSourceBranches: [],
    requirePreviousEnvironment: "",
    requireSuccessfulRun: true,
    minWaitMinutes: 0,
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
            Inherited deploy gates apply here and cannot be edited at this level.
            A locked gate is a floor — you may add stricter gates, never weaker
            ones.
          </p>
          <InheritedTable policies={inherited} />
        </div>
      )}

      {policies.length > 0 ? (
        <table className="policy-table">
          <thead>
            <tr>
              <th>Environment</th>
              <th>Gate</th>
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
          No environment policies yet. Add one below to gate deploys with
          approvals, source branches, or a wait window.
        </div>
      )}

      <div className="panel form-narrow policy-form-panel">
        <h3>
          {editing ? `Edit ${editing.pattern}` : "Add an environment policy"}
        </h3>
        {state.error && <div className="form-error">{state.error}</div>}
        <form action={formAction} key={formKey}>
          <label className="field">
            <span>Environment</span>
            <input
              name="pattern"
              defaultValue={current.pattern}
              readOnly={!!editing}
              placeholder="production or prod-*"
              required
            />
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
          <label className="field">
            <span>Allowed source branches</span>
            <input
              name="allowedSourceBranches"
              defaultValue={current.allowedSourceBranches.join(", ")}
              placeholder="main, release/* (blank = any)"
            />
          </label>
          <label className="field">
            <span>Require previous environment</span>
            <input
              name="requirePreviousEnvironment"
              defaultValue={current.requirePreviousEnvironment}
              placeholder="staging (blank = none)"
            />
          </label>
          <label className="field">
            <span>Wait window (minutes)</span>
            <input
              name="minWaitMinutes"
              type="number"
              min={0}
              defaultValue={current.minWaitMinutes}
            />
          </label>
          <label className="check">
            <input
              type="checkbox"
              name="requireSuccessfulRun"
              defaultChecked={current.requireSuccessfulRun}
            />
            <span>Require a successful pipeline run before deploying</span>
          </label>
          {canLock && (
            <label className="check">
              <input
                type="checkbox"
                name="locked"
                defaultChecked={current.locked}
              />
              <span>
                Lock this gate so narrower scopes can only make it stricter
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
