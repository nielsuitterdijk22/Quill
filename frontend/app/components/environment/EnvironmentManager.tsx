"use client";

import { useEffect, useRef, useState } from "react";
import { useFormState, useFormStatus } from "react-dom";

import type { Environment } from "../../lib/api";
import {
  createEnvironmentAction,
  deleteEnvironmentAction,
  updateEnvironmentAction,
  type EnvironmentFormState,
} from "./actions";

const initial: EnvironmentFormState = {};

function SaveButton({ editing }: { editing: boolean }) {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Saving…" : editing ? "Update environment" : "Add environment"}
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

// DeleteEnvironmentForm is a one-button form that removes a single environment.
function DeleteEnvironmentForm({
  project,
  slug,
}: {
  project: string;
  slug: string;
}) {
  const action = deleteEnvironmentAction.bind(null, project);
  const [, formAction] = useFormState(action, initial);
  return (
    <form action={formAction} className="inline-form">
      <input type="hidden" name="slug" value={slug} />
      <DeleteButton />
    </form>
  );
}

// EnvironmentManager lists a project's deployment targets and lets project admins
// create, edit, and delete them through a modal — matching the secrets and
// branch-policy managers. Environments are ranked to express a promotion ladder
// (lower deploys first); environment policies reference them by slug.
export function EnvironmentManager({
  project,
  environments,
}: {
  project: string;
  environments: Environment[];
}) {
  const createAction = createEnvironmentAction.bind(null, project);
  const updateAction = updateEnvironmentAction.bind(null, project);
  const [createState, createFormAction] = useFormState(createAction, initial);
  const [updateState, updateFormAction] = useFormState(updateAction, initial);
  // editing is the environment being edited, or null when adding a new one.
  const [editing, setEditing] = useState<Environment | null>(null);
  const dialogRef = useRef<HTMLDialogElement>(null);
  const prevCreate = useRef(createState);
  const prevUpdate = useRef(updateState);

  // Close the modal when a create or update succeeds.
  useEffect(() => {
    if (createState !== prevCreate.current) {
      if (createState.ok) closeModal();
      prevCreate.current = createState;
    }
  }, [createState]);
  useEffect(() => {
    if (updateState !== prevUpdate.current) {
      if (updateState.ok) closeModal();
      prevUpdate.current = updateState;
    }
  }, [updateState]);

  function openAdd() {
    setEditing(null);
    dialogRef.current?.showModal();
  }

  function openEdit(env: Environment) {
    setEditing(env);
    dialogRef.current?.showModal();
  }

  function closeModal() {
    dialogRef.current?.close();
    setEditing(null);
  }

  const formAction = editing ? updateFormAction : createFormAction;
  const state = editing ? updateState : createState;
  // A new environment defaults to the next rank so the promotion ladder grows in
  // order; editing keeps the environment's own values.
  const current = editing ?? {
    slug: "",
    name: "",
    description: "",
    rank: environments.length,
  };
  const formKey = editing ? editing.slug : "new";

  return (
    <div className="policy-area">
      <div className="policy-list-header">
        <button type="button" className="btn ghost small" onClick={openAdd}>
          + Add environment
        </button>
      </div>

      {environments.length > 0 ? (
        <table className="policy-table">
          <thead>
            <tr>
              <th>Rank</th>
              <th>Environment</th>
              <th>Description</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {environments.map((e) => (
              <tr key={e.slug}>
                <td>
                  <span className="badge accent">{e.rank}</span>
                </td>
                <td className="mono">
                  {e.name}
                  <span className="sub"> · {e.slug}</span>
                </td>
                <td>{e.description || <span className="subtle">—</span>}</td>
                <td className="policy-row-actions">
                  <button
                    type="button"
                    className="btn ghost small"
                    onClick={() => openEdit(e)}
                  >
                    Edit
                  </button>
                  <DeleteEnvironmentForm project={project} slug={e.slug} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <div className="empty">
          No environments yet. Add one — e.g. staging then production — to build
          a promotion ladder you can gate with environment policies.
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
          <h3>{editing ? `Edit ${editing.slug}` : "Add an environment"}</h3>
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
          {editing ? (
            <input type="hidden" name="slug" value={editing.slug} />
          ) : (
            <label className="field">
              <span>Slug</span>
              <input
                name="slug"
                defaultValue=""
                placeholder="production"
                autoComplete="off"
                required
              />
            </label>
          )}
          <label className="field">
            <span>Display name</span>
            <input name="name" defaultValue={current.name} placeholder="Production" />
          </label>
          <label className="field">
            <span>Rank (promotion order, lower deploys first)</span>
            <input name="rank" type="number" min={0} defaultValue={current.rank} />
          </label>
          <label className="field">
            <span>Description</span>
            <input
              name="description"
              defaultValue={current.description}
              placeholder="Customer-facing production environment"
            />
          </label>
          <div className="form-actions">
            <SaveButton editing={!!editing} />
            <button type="button" className="btn ghost" onClick={closeModal}>
              Cancel
            </button>
          </div>
        </form>
      </dialog>
    </div>
  );
}
