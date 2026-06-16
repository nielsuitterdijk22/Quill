"use client";

import Link from "next/link";
import { useFormState, useFormStatus } from "react-dom";

import { createProjectAction, type CreateState } from "./actions";

const initialState: CreateState = {};

function SubmitButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Creating…" : "Create project"}
    </button>
  );
}

export function NewProjectForm() {
  const [state, formAction] = useFormState(createProjectAction, initialState);

  return (
    <div className="panel form-narrow">
      <h2>New project</h2>
      <div className="readme-body">
        {state.error && <div className="form-error">{state.error}</div>}
        <form action={formAction}>
          <label className="field">
            <span>Name</span>
            <input name="name" autoFocus required placeholder="Acme Corp" />
          </label>
          <label className="field">
            <span>Slug</span>
            <input
              name="slug"
              placeholder="acme"
              pattern="[A-Za-z0-9._\-]+"
            />
          </label>
          <p className="hint">
            Used in URLs and mirrored to Forgejo. Leave blank to derive it from
            the name.
          </p>
          <label className="field">
            <span>Description</span>
            <textarea name="description" rows={3} placeholder="Optional" />
          </label>
          <div className="form-actions">
            <SubmitButton />
            <Link className="btn ghost" href="/projects">
              Cancel
            </Link>
          </div>
        </form>
      </div>
    </div>
  );
}
