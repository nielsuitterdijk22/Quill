"use client";

import { useState } from "react";
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
// create, edit, and delete them. Environments are ranked to express a promotion
// ladder (lower deploys first); environment policies reference them by slug.
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
  const [editing, setEditing] = useState<Environment | null>(null);

  const blank: Environment = {
    id: "",
    slug: "",
    name: "",
    description: "",
    rank: environments.length,
    createdAt: "",
    updatedAt: "",
  };
  const current = editing ?? blank;
  const formAction = editing ? updateFormAction : createFormAction;
  const state = editing ? updateState : createState;
  const formKey = editing ? editing.slug : "new";

  return (
    <div className="policy-area">
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
                    onClick={() => setEditing(e)}
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

      <div className="panel form-narrow policy-form-panel">
        <h3>
          {editing ? `Edit ${editing.slug}` : "Add an environment"}
        </h3>
        {state.error && <div className="form-error">{state.error}</div>}
        <form action={formAction} key={formKey}>
          {editing && <input type="hidden" name="slug" value={editing.slug} />}
          {!editing && (
            <label className="field">
              <span>Slug</span>
              <input
                name="slug"
                defaultValue={current.slug}
                placeholder="production"
                required
              />
            </label>
          )}
          <label className="field">
            <span>Display name</span>
            <input
              name="name"
              defaultValue={current.name}
              placeholder="Production"
            />
          </label>
          <label className="field">
            <span>Rank (promotion order, lower deploys first)</span>
            <input
              name="rank"
              type="number"
              min={0}
              defaultValue={current.rank}
            />
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
