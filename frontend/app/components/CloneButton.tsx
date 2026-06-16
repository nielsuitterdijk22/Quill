"use client";

import { useState, useTransition } from "react";

import { generateGitTokenAction } from "./clone-actions";

// authUrl injects basic-auth credentials into a clone URL so the example can be
// copied and used directly: https://user:token@host/owner/repo.git
function authUrl(httpUrl: string, username: string, token: string): string {
  const sep = httpUrl.indexOf("://");
  if (sep === -1) return httpUrl;
  const scheme = httpUrl.slice(0, sep);
  const rest = httpUrl.slice(sep + 3);
  return `${scheme}://${encodeURIComponent(username)}:${token}@${rest}`;
}

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

// CloneButton reveals a repository's HTTPS clone URL and can mint a personal git
// access token (shown once) for pushing over HTTPS.
export function CloneButton({ httpUrl }: { httpUrl: string }) {
  const [open, setOpen] = useState(false);
  const [cred, setCred] = useState<{ username: string; token: string } | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();

  function generate() {
    setError(null);
    startTransition(async () => {
      const res = await generateGitTokenAction("clone over HTTPS");
      if (res.ok) setCred({ username: res.username, token: res.token });
      else setError(res.error);
    });
  }

  return (
    <div className="clone-wrap">
      <button
        type="button"
        className="btn primary clone-toggle"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        Clone
      </button>
      {open && (
        <div className="clone-pop" role="dialog" aria-label="Clone repository">
          <div className="clone-pop-head">Clone over HTTPS</div>
          <div className="clone-field">
            <input readOnly value={httpUrl} aria-label="HTTPS clone URL" />
            <CopyButton value={httpUrl} label="Copy clone URL" />
          </div>

          {!cred ? (
            <div className="clone-token-cta">
              <p className="subtle">
                Need to push? Generate a personal access token to use as your git
                password.
              </p>
              {error && <div className="form-error">{error}</div>}
              <button
                type="button"
                className="btn"
                onClick={generate}
                disabled={pending}
              >
                {pending ? "Generating…" : "Generate access token"}
              </button>
            </div>
          ) : (
            <div className="clone-token-out">
              <p className="hint">
                Copy this now — it won&rsquo;t be shown again.
              </p>
              <div className="clone-field">
                <input
                  readOnly
                  value={cred.token}
                  aria-label="Git access token"
                />
                <CopyButton value={cred.token} label="Copy access token" />
              </div>
              <span className="clone-label">Authenticated URL</span>
              <div className="clone-field">
                <input
                  readOnly
                  value={authUrl(httpUrl, cred.username, cred.token)}
                  aria-label="Authenticated clone URL"
                />
                <CopyButton
                  value={authUrl(httpUrl, cred.username, cred.token)}
                  label="Copy authenticated URL"
                />
              </div>
              <p className="subtle">
                Pushes to protected branches are rejected — open a pull request
                instead.
              </p>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
