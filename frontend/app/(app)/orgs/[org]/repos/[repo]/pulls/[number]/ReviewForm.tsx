"use client";

import { useEffect, useRef } from "react";
import { useFormState, useFormStatus } from "react-dom";

import { reviewAction, type ReviewActionState } from "./actions";

const initialState: ReviewActionState = {};

function ReviewButtons() {
  const { pending } = useFormStatus();
  return (
    <div className="review-actions">
      <button
        className="btn"
        type="submit"
        name="event"
        value="COMMENT"
        disabled={pending}
      >
        Comment
      </button>
      <button
        className="btn danger"
        type="submit"
        name="event"
        value="REQUEST_CHANGES"
        disabled={pending}
      >
        Request changes
      </button>
      <button
        className="btn primary"
        type="submit"
        name="event"
        value="APPROVED"
        disabled={pending}
      >
        Approve
      </button>
    </div>
  );
}

// ReviewForm lets a member submit a review (approve, request changes, or
// comment) on an open pull request. The chosen verdict is carried by the
// submit button's value.
export function ReviewForm({
  org,
  repo,
  number,
}: {
  org: string;
  repo: string;
  number: number;
}) {
  const action = reviewAction.bind(null, org, repo, number);
  const [state, formAction] = useFormState(action, initialState);
  const formRef = useRef<HTMLFormElement>(null);

  useEffect(() => {
    if (state.ok) formRef.current?.reset();
  }, [state.ok]);

  return (
    <form className="review-form" action={formAction} ref={formRef}>
      <div className="review-form-head">
        <strong>Review changes</strong>
        <span className="subtle">
          Approve to satisfy the branch policy, or request changes to block the
          merge.
        </span>
      </div>
      {state.error && <div className="form-error">{state.error}</div>}
      <textarea
        name="body"
        rows={3}
        aria-label="Review comment"
        placeholder="Leave a review comment (optional unless commenting)…"
      />
      <ReviewButtons />
    </form>
  );
}
