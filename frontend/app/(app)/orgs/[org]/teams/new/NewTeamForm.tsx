"use client";

import Link from "next/link";
import { useFormState, useFormStatus } from "react-dom";

import { createTeamAction, type CreateTeamState } from "./actions";

const initialState: CreateTeamState = {};

function SubmitButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Creating…" : "Create team"}
    </button>
  );
}

export function NewTeamForm({ org }: { org: string }) {
  const action = createTeamAction.bind(null, org);
  const [state, formAction] = useFormState(action, initialState);

  return (
    <div className="panel form-narrow">
      <h2>New team</h2>
      <div className="readme-body">
        {state.error && <div className="form-error">{state.error}</div>}
        <form action={formAction}>
          <label className="field">
            <span>Name</span>
            <input name="name" autoFocus required placeholder="Platform" />
          </label>
          <label className="field">
            <span>Slug</span>
            <input name="slug" placeholder="platform" pattern="[A-Za-z0-9._\-]+" />
          </label>
          <p className="hint">
            Used in URLs. Leave blank to derive it from the name.
          </p>
          <label className="field">
            <span>Description</span>
            <textarea name="description" rows={3} placeholder="Optional" />
          </label>
          <div className="form-actions">
            <SubmitButton />
            <Link className="btn ghost" href={`/orgs/${org}/teams`}>
              Cancel
            </Link>
          </div>
        </form>
      </div>
    </div>
  );
}
