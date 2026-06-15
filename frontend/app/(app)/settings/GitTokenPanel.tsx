"use client";

import { useState, useTransition } from "react";

import { generateGitTokenAction } from "../../components/clone-actions";

function CopyButton({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      className="btn ghost copy-btn"
      aria-label={label}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(value);
          setCopied(true);
          setTimeout(() => setCopied(false), 1500);
        } catch {
          /* clipboard unavailable; ignore */
        }
      }}
    >
      {copied ? "Copied" : "Copy"}
    </button>
  );
}

// GitTokenPanel mints a one-time personal git access token for HTTPS clone/push.
// The token is shown exactly once; Quill never stores or re-displays it.
export function GitTokenPanel() {
  const [cred, setCred] = useState<{ username: string; token: string } | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();

  function generate() {
    setError(null);
    startTransition(async () => {
      const res = await generateGitTokenAction();
      if (res.ok) setCred({ username: res.username, token: res.token });
      else setError(res.error);
    });
  }

  return (
    <div className="panel form-narrow">
      <h2>Git access token</h2>
      <div className="readme-body">
        {!cred ? (
          <>
            <p className="subtle">
              Generate a personal access token to use as your git password when
              cloning or pushing over HTTPS.
            </p>
            {error && <div className="form-error">{error}</div>}
            <div className="form-actions">
              <button
                type="button"
                className="btn primary"
                onClick={generate}
                disabled={pending}
              >
                {pending ? "Generating…" : "Generate access token"}
              </button>
            </div>
          </>
        ) : (
          <>
            <p className="hint">Copy this now — it won&rsquo;t be shown again.</p>
            <span className="clone-label">Username</span>
            <div className="clone-field">
              <input readOnly value={cred.username} aria-label="Git username" />
              <CopyButton value={cred.username} label="Copy username" />
            </div>
            <span className="clone-label">Access token</span>
            <div className="clone-field">
              <input
                readOnly
                value={cred.token}
                aria-label="Git access token"
              />
              <CopyButton value={cred.token} label="Copy access token" />
            </div>
          </>
        )}
      </div>
    </div>
  );
}
