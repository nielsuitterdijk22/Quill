"use client";

import { useEffect, useState, type ReactNode } from "react";
import { useFormState, useFormStatus } from "react-dom";

export type EditTextState = { error?: string; ok?: boolean; at?: number };

function SaveButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Saving…" : "Save"}
    </button>
  );
}

// EditableDescription shows the opening post of a PR or issue (title in the
// page header, description here) with an Edit toggle: title + description
// in place, saved through the given server action. `children` is the
// rendered description (server-rendered markdown).
export function EditableDescription({
  action,
  title,
  body,
  head,
  children,
}: {
  action: (prev: EditTextState, formData: FormData) => Promise<EditTextState>;
  title: string;
  body: string;
  head: ReactNode;
  children: ReactNode;
}) {
  const [editing, setEditing] = useState(false);
  const [state, formAction] = useFormState(action, {} as EditTextState);

  useEffect(() => {
    if (state.ok) setEditing(false);
  }, [state]);

  return (
    <div className="pr-comment">
      <div className="pr-comment-head">
        {head}
        <span className="spacer" />
        {!editing && (
          <button
            type="button"
            className="btn ghost small"
            onClick={() => setEditing(true)}
          >
            Edit
          </button>
        )}
      </div>
      <div className="pr-comment-body">
        {editing ? (
          <form className="edit-text-form" action={formAction}>
            {state.error && <div className="form-error">{state.error}</div>}
            <label className="field">
              <span>Title</span>
              <input name="title" defaultValue={title} required maxLength={255} />
            </label>
            <label className="field">
              <span>Description</span>
              <textarea
                name="body"
                defaultValue={body}
                rows={Math.min(24, Math.max(8, body.split("\n").length + 2))}
                placeholder="Describe the change. Markdown is supported."
              />
            </label>
            <div className="form-actions">
              <SaveButton />
              <button
                type="button"
                className="btn ghost"
                onClick={() => setEditing(false)}
              >
                Cancel
              </button>
              <span className="subtle">Markdown supported</span>
            </div>
          </form>
        ) : (
          children
        )}
      </div>
    </div>
  );
}
