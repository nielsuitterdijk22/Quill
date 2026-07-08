"use client";

import { useEffect, useRef, useState } from "react";
import { useFormState, useFormStatus } from "react-dom";

import type { PipelineSecret } from "../../lib/api";
import {
  setSecretAction,
  deleteSecretAction,
  type SecretTarget,
  type SecretFormState,
} from "./actions";

const initial: SecretFormState = {};

// scopeLabel renders a secret's scope for the table, naming the environment for
// environment-scoped secrets.
function scopeLabel(secret: PipelineSecret): string {
  if (secret.scope === "environment") {
    return `Environment · ${secret.scopeName ?? ""}`;
  }
  if (secret.scope === "repo") return "Repository";
  return "Project";
}

function SaveButton({ rotating }: { rotating: boolean }) {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Saving…" : rotating ? "Update secret" : "Add secret"}
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

// DeleteSecretForm is a one-button form that removes a single secret by name.
function DeleteSecretForm({
  target,
  name,
}: {
  target: SecretTarget;
  name: string;
}) {
  const action = deleteSecretAction.bind(null, target);
  const [, formAction] = useFormState(action, initial);
  return (
    <form action={formAction} className="inline-form">
      <input type="hidden" name="name" value={name} />
      <DeleteButton />
    </form>
  );
}

// InheritedSecretsTable shows the project + environment secrets that also apply
// to a repository's runs, read-only. Managed at their own scope, not here.
function InheritedSecretsTable({ secrets }: { secrets: PipelineSecret[] }) {
  return (
    <table className="policy-table">
      <thead>
        <tr>
          <th>Secret</th>
          <th>Scope</th>
          <th>Last updated</th>
        </tr>
      </thead>
      <tbody>
        {secrets.map((secret) => (
          <tr key={`${secret.scope}:${secret.scopeName ?? ""}:${secret.name}`}>
            <td className="mono">{secret.name}</td>
            <td className="subtle">{scopeLabel(secret)}</td>
            <td className="subtle">
              {new Date(secret.updatedAt).toLocaleString()}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

// SecretsManager lists a scope's secret names and lets project admins add, rotate,
// and delete them through a modal. Secrets are write-only: values are never shown,
// only their names, scope, and last-updated time. It is reused at the project,
// repository, and environment scopes via the bound target. On the repo scope it
// also renders the inherited project/environment secrets read-only.
export function SecretsManager({
  target,
  secrets,
  inherited = [],
}: {
  target: SecretTarget;
  secrets: PipelineSecret[];
  inherited?: PipelineSecret[];
}) {
  const action = setSecretAction.bind(null, target);
  const [state, formAction] = useFormState(action, initial);
  // When rotating, the name is fixed and only the value is entered.
  const [rotating, setRotating] = useState<string | null>(null);
  const dialogRef = useRef<HTMLDialogElement>(null);
  const prevState = useRef(state);

  useEffect(() => {
    if (state !== prevState.current) {
      if (state.ok) {
        dialogRef.current?.close();
        setRotating(null);
      }
      prevState.current = state;
    }
  }, [state]);

  function openAdd() {
    setRotating(null);
    dialogRef.current?.showModal();
  }

  function openRotate(name: string) {
    setRotating(name);
    dialogRef.current?.showModal();
  }

  function closeModal() {
    dialogRef.current?.close();
    setRotating(null);
  }

  const formKey = rotating ?? "new";

  return (
    <div className="policy-area">
      {inherited.length > 0 && (
        <div className="policy-inherited">
          <p className="subtle">
            Inherited from the project and environments. These also apply to this
            repository&apos;s runs and are managed at their own scope. A repository
            secret of the same name overrides them.
          </p>
          <InheritedSecretsTable secrets={inherited} />
        </div>
      )}

      <div className="policy-list-header">
        <button type="button" className="btn ghost small" onClick={openAdd}>
          + Add secret
        </button>
      </div>

      {secrets.length > 0 ? (
        <table className="policy-table">
          <thead>
            <tr>
              <th>Secret</th>
              <th>Scope</th>
              <th>Last updated</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {secrets.map((secret) => (
              <tr key={secret.name}>
                <td className="mono">{secret.name}</td>
                <td className="subtle">{scopeLabel(secret)}</td>
                <td className="subtle">
                  {new Date(secret.updatedAt).toLocaleString()}
                </td>
                <td className="policy-row-actions">
                  <button
                    type="button"
                    className="btn ghost small"
                    onClick={() => openRotate(secret.name)}
                  >
                    Rotate
                  </button>
                  <DeleteSecretForm target={target} name={secret.name} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <div className="empty">
          No secrets yet. Add one to expose it to workflows as{" "}
          <span className="mono">{"${{ secrets.NAME }}"}</span>.
        </div>
      )}

      <dialog
        ref={dialogRef}
        className="policy-modal"
        onCancel={closeModal}
        onClick={(e) => {
          if (e.target === dialogRef.current) closeModal();
        }}
      >
        <div className="policy-modal-head">
          <h3>{rotating ? `Rotate ${rotating}` : "Add a secret"}</h3>
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
          {rotating ? (
            <input type="hidden" name="name" value={rotating} />
          ) : (
            <label className="field">
              <span>Name</span>
              <input
                name="name"
                defaultValue=""
                placeholder="API_TOKEN"
                autoComplete="off"
                required
              />
            </label>
          )}
          <label className="field">
            <span>Value</span>
            <textarea
              name="value"
              defaultValue=""
              placeholder="Paste the secret value"
              autoComplete="off"
              rows={3}
              required
            />
          </label>
          <p className="subtle">
            Values are encrypted at rest and never shown again after saving.
          </p>
          <div className="form-actions">
            <SaveButton rotating={!!rotating} />
            <button type="button" className="btn ghost" onClick={closeModal}>
              Cancel
            </button>
          </div>
        </form>
      </dialog>
    </div>
  );
}
