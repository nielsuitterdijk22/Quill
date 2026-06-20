"use client";

import { useState, useTransition } from "react";

import type { GitTokenSummary } from "../../lib/api";
import {
  generateGitTokenAction,
  revokeGitTokenAction,
} from "../../components/clone-actions";

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

function formatDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

// GitTokenPanel mints personal git access tokens for HTTPS clone/push and lists
// the user's outstanding tokens so they can revoke them. A freshly minted token's
// secret is shown exactly once; Quill stores only each token's metadata.
export function GitTokenPanel({
  tokens,
  loadError,
}: {
  tokens: GitTokenSummary[];
  loadError?: string;
}) {
  const [cred, setCred] = useState<{ username: string; token: string } | null>(
    null,
  );
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  const [revoking, setRevoking] = useState<string | null>(null);

  const trimmedName = name.trim();
  const isDuplicateName = trimmedName !== "" && tokens.some(
    (t) => t.name.toLowerCase() === trimmedName.toLowerCase(),
  );

  function generate() {
    setError(null);
    setCred(null);
    startTransition(async () => {
      const res = await generateGitTokenAction(trimmedName);
      if (res.ok) {
        setCred({ username: res.username, token: res.token });
        setName("");
      } else {
        setError(res.error);
      }
    });
  }

  function revoke(id: string) {
    setError(null);
    setRevoking(id);
    startTransition(async () => {
      const res = await revokeGitTokenAction(id);
      setRevoking(null);
      if (!res.ok) setError(res.error);
    });
  }

  return (
    <div className="panel form-narrow">
      <h2>Git access tokens</h2>
      <div className="readme-body">
        <p className="subtle">
          Generate a personal access token to use as your git password when
          cloning or pushing over HTTPS.
        </p>
        {loadError && (
          <div className="form-error">
            Could not load tokens: {loadError}
          </div>
        )}
        {error && <div className="form-error">{error}</div>}

        {cred && (
          <div className="token-reveal">
            <p className="hint">Copy this now &mdash; it won&rsquo;t be shown again.</p>
            <span className="clone-label">Username</span>
            <div className="clone-field">
              <input readOnly value={cred.username} aria-label="Git username" />
              <CopyButton value={cred.username} label="Copy username" />
            </div>
            <span className="clone-label">Access token</span>
            <div className="clone-field">
              <input readOnly value={cred.token} aria-label="Git access token" />
              <CopyButton value={cred.token} label="Copy access token" />
            </div>
          </div>
        )}

        {isDuplicateName && (
          <div className="form-error">
            A token named &ldquo;{trimmedName}&rdquo; already exists. Choose a different name.
          </div>
        )}
        <div className="token-create">
          <input
            type="text"
            className="token-name-input"
            placeholder="Token name (e.g. laptop)"
            value={name}
            onChange={(e) => setName(e.target.value)}
            aria-label="Token name"
            maxLength={100}
          />
          <button
            type="button"
            className="btn primary"
            onClick={generate}
            disabled={pending || isDuplicateName}
          >
            {pending ? "Generating\u2026" : "Generate token"}
          </button>
        </div>

        {tokens.length > 0 && (
          <ul className="token-list">
            {tokens.map((t) => (
              <li key={t.id} className="token-row">
                <div className="token-meta">
                  <span className="token-name">{t.name}</span>
                  {t.createdAt && (
                    <span className="subtle">Created {formatDate(t.createdAt)}</span>
                  )}
                </div>
                <button
                  type="button"
                  className="btn ghost danger"
                  onClick={() => revoke(t.id)}
                  disabled={pending && revoking === t.id}
                >
                  {revoking === t.id ? "Revoking\u2026" : "Revoke"}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
