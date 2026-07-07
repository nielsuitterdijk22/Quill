"use client";

import { useState } from "react";
import { useFormState, useFormStatus } from "react-dom";

import type { PipelineSecret } from "../../lib/api";
import {
  setSecretAction,
  deleteSecretAction,
  type SecretTarget,
  type SecretFormState,
} from "./actions";

const initial: SecretFormState = {};

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

// SecretsManager lists a scope's secret names and lets project admins add, rotate,
// and delete them. Secrets are write-only: values are never shown, only their
// names and last-updated time. It is reused at the project, repository, and
// environment scopes via the bound target.
export function SecretsManager({
  target,
  secrets,
}: {
  target: SecretTarget;
  secrets: PipelineSecret[];
}) {
  const action = setSecretAction.bind(null, target);
  const [state, formAction] = useFormState(action, initial);
  // When rotating, the name is fixed and only the value is entered.
  const [rotating, setRotating] = useState<string | null>(null);
  const formKey = rotating ?? "new";

  return (
    <div className="policy-area">
      {secrets.length > 0 ? (
        <table className="policy-table">
          <thead>
            <tr>
              <th>Secret</th>
              <th>Last updated</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {secrets.map((secret) => (
              <tr key={secret.name}>
                <td className="mono">{secret.name}</td>
                <td className="subtle">
                  {new Date(secret.updatedAt).toLocaleString()}
                </td>
                <td className="policy-row-actions">
                  <button
                    type="button"
                    className="btn ghost small"
                    onClick={() => setRotating(secret.name)}
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

      <div className="panel form-narrow policy-form-panel">
        <h3>{rotating ? `Rotate ${rotating}` : "Add a secret"}</h3>
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
            {rotating && (
              <button
                type="button"
                className="btn ghost"
                onClick={() => setRotating(null)}
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
