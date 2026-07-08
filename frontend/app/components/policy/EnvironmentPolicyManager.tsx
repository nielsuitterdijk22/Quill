"use client";

import { useEffect, useRef, useState } from "react";
import { useFormState, useFormStatus } from "react-dom";

import type { Environment, EnvironmentPolicy } from "../../lib/api";
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

// PatternField couples the selector to the project's defined environments: it
// offers a dropdown of environment slugs plus an "Other (glob pattern)…" escape
// hatch for pattern selectors (e.g. prod-*). It submits the chosen value under
// name="pattern" via a hidden input so either mode works with the plain form
// action. On edit the selector is immutable, so it renders read-only.
function PatternField({
  environments,
  defaultValue,
  editing,
}: {
  environments: Environment[];
  defaultValue: string;
  editing: boolean;
}) {
  const slugs = environments.map((e) => e.slug);
  const startsGlob = defaultValue !== "" && !slugs.includes(defaultValue);
  const [mode, setMode] = useState<"pick" | "glob">(
    environments.length === 0 || startsGlob ? "glob" : "pick",
  );
  const [value, setValue] = useState(
    defaultValue || (slugs.length > 0 ? slugs[0] : ""),
  );

  if (editing) {
    return (
      <label className="field">
        <span>Environment</span>
        <input name="pattern" defaultValue={defaultValue} readOnly />
      </label>
    );
  }

  const glob = mode === "glob" || environments.length === 0;
  return (
    <label className="field">
      <span>Environment</span>
      <input type="hidden" name="pattern" value={value} />
      {environments.length > 0 && (
        <select
          value={glob ? "__glob__" : value}
          onChange={(e) => {
            if (e.target.value === "__glob__") {
              setMode("glob");
            } else {
              setMode("pick");
              setValue(e.target.value);
            }
          }}
        >
          {slugs.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
          <option value="__glob__">Other (glob pattern)…</option>
        </select>
      )}
      {glob && (
        <input
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="production or prod-*"
          required
          autoFocus={environments.length > 0}
        />
      )}
    </label>
  );
}

// PreviousEnvField renders the ordered-promotion prerequisite as a dropdown of
// the project's environments (blank = none). A legacy free-text value that is no
// longer a defined environment is preserved as an extra option.
function PreviousEnvField({
  environments,
  defaultValue,
}: {
  environments: Environment[];
  defaultValue: string;
}) {
  const slugs = environments.map((e) => e.slug);
  if (environments.length === 0) {
    return (
      <label className="field">
        <span>Require previous environment</span>
        <input
          name="requirePreviousEnvironment"
          defaultValue={defaultValue}
          placeholder="staging (blank = none)"
        />
      </label>
    );
  }
  const extra = defaultValue && !slugs.includes(defaultValue) ? [defaultValue] : [];
  return (
    <label className="field">
      <span>Require previous environment</span>
      <select name="requirePreviousEnvironment" defaultValue={defaultValue}>
        <option value="">— none —</option>
        {[...slugs, ...extra].map((s) => (
          <option key={s} value={s}>
            {s}
          </option>
        ))}
      </select>
    </label>
  );
}

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

export function EnvironmentPolicyManager({
  target,
  policies,
  inherited = [],
  environments = [],
  canLock = false,
  canEdit = false,
}: {
  target: PolicyTarget;
  policies: EnvironmentPolicy[];
  inherited?: EnvironmentPolicy[];
  environments?: Environment[];
  canLock?: boolean;
  canEdit?: boolean;
}) {
  const action = saveEnvironmentPolicyAction.bind(null, target);
  const [state, formAction] = useFormState(action, initial);
  const [editing, setEditing] = useState<EnvironmentPolicy | null>(null);
  const dialogRef = useRef<HTMLDialogElement>(null);
  const prevState = useRef(state);

  useEffect(() => {
    if (state !== prevState.current) {
      if (state.ok) {
        dialogRef.current?.close();
        setEditing(null);
      }
      prevState.current = state;
    }
  }, [state]);

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

  function openNew() {
    setEditing(null);
    dialogRef.current?.showModal();
  }

  function openEdit(p: EnvironmentPolicy) {
    setEditing(p);
    dialogRef.current?.showModal();
  }

  function closeModal() {
    dialogRef.current?.close();
    setEditing(null);
  }

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

      {canEdit && (
        <div className="policy-list-header">
          <button type="button" className="btn ghost small" onClick={openNew}>
            + New policy
          </button>
        </div>
      )}

      {policies.length > 0 ? (
        <table className="policy-table">
          <thead>
            <tr>
              <th>Environment</th>
              <th>Gate</th>
              {canEdit && <th />}
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
                {canEdit && (
                  <td className="policy-row-actions">
                    <button
                      type="button"
                      className="btn ghost small"
                      onClick={() => openEdit(p)}
                    >
                      Edit
                    </button>
                    <DeletePolicyForm target={target} pattern={p.pattern} />
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <div className="empty">
          {canEdit
            ? "No environment policies yet. Add one to gate deploys with approvals, source branches, or a wait window."
            : "No environment policies set at this scope."}
        </div>
      )}

      {canEdit && (
      <dialog
        ref={dialogRef}
        className="policy-modal"
        onCancel={closeModal}
        onClick={(e) => { if (e.target === dialogRef.current) closeModal(); }}
      >
        <div className="policy-modal-head">
          <h3>
            {editing ? `Edit "${editing.pattern}"` : "New environment policy"}
          </h3>
          <button
            type="button"
            className="policy-modal-close"
            onClick={closeModal}
            aria-label="Close"
          >
            ✕
          </button>
        </div>
        {state.error && <div className="form-error">{state.error}</div>}
        <form action={formAction} key={formKey}>
          <PatternField
            environments={environments}
            defaultValue={current.pattern}
            editing={!!editing}
          />
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
          <PreviousEnvField
            environments={environments}
            defaultValue={current.requirePreviousEnvironment}
          />
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
            <button
              type="button"
              className="btn ghost"
              onClick={closeModal}
            >
              Cancel
            </button>
          </div>
        </form>
      </dialog>
      )}
    </div>
  );
}
