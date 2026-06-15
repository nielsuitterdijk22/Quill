"use client";

import Link from "next/link";
import { useFormState, useFormStatus } from "react-dom";

import { createRepoAction, type CreateState } from "./actions";

const initialState: CreateState = {};

function SubmitButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Creating…" : "Create repository"}
    </button>
  );
}

export function NewRepoForm({ org }: { org: string }) {
  // Bind the org slug into the server action.
  const action = createRepoAction.bind(null, org);
  const [state, formAction] = useFormState(action, initialState);

  return (
    <div className="panel form-narrow">
      <h2>New repository</h2>
      <div className="readme-body">
        {state.error && <div className="form-error">{state.error}</div>}
        <form action={formAction}>
          <label className="field">
            <span>Name</span>
            <input
              name="name"
              autoFocus
              required
              placeholder="Widget Service"
            />
          </label>
          <label className="field">
            <span>Slug</span>
            <input name="slug" placeholder="widget" pattern="[A-Za-z0-9._\-]+" />
          </label>
          <p className="hint">
            Leave blank to derive it from the name. The git repository is created
            in Forgejo with a README and a <span className="mono">main</span>{" "}
            branch.
          </p>
          <label className="field">
            <span>Description</span>
            <textarea name="description" rows={3} placeholder="Optional" />
          </label>
          <label className="field">
            <span>Visibility</span>
            <select name="visibility" defaultValue="private">
              <option value="private">Private</option>
              <option value="internal">Internal</option>
              <option value="public">Public</option>
            </select>
          </label>
          <div className="form-actions">
            <SubmitButton />
            <Link className="btn ghost" href={`/orgs/${org}`}>
              Cancel
            </Link>
          </div>
        </form>
      </div>
    </div>
  );
}
