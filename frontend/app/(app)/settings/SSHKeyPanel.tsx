"use client";

import { useState } from "react";

import type { SSHKey } from "../../lib/api";

async function fetchSSHKeys(): Promise<SSHKey[]> {
  const res = await fetch("/api/backend/me/ssh-keys", {
    credentials: "include",
    cache: "no-store",
  });
  if (!res.ok) return [];
  const data = (await res.json()) as { keys?: SSHKey[] };
  return data.keys ?? [];
}

async function postSSHKey(
  title: string,
  key: string,
): Promise<{ ok: true; data: SSHKey } | { ok: false; error: string }> {
  try {
    const res = await fetch("/api/backend/me/ssh-keys", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ title, key }),
    });
    const body = (await res.json().catch(() => null)) as
      | (SSHKey & { message?: string })
      | null;
    if (!res.ok) {
      return { ok: false, error: body?.message ?? `Error ${res.status}` };
    }
    return { ok: true, data: body as SSHKey };
  } catch {
    return { ok: false, error: "Could not reach the backend." };
  }
}

async function removeSSHKey(
  id: number,
): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    const res = await fetch(`/api/backend/me/ssh-keys/${id}`, {
      method: "DELETE",
      credentials: "include",
    });
    if (!res.ok) {
      const body = (await res.json().catch(() => null)) as {
        message?: string;
      } | null;
      return { ok: false, error: body?.message ?? `Error ${res.status}` };
    }
    return { ok: true };
  } catch {
    return { ok: false, error: "Could not reach the backend." };
  }
}

export function SSHKeyPanel({ keys: initialKeys }: { keys: SSHKey[] }) {
  const [keys, setKeys] = useState<SSHKey[]>(initialKeys);
  const [title, setTitle] = useState("");
  const [keyText, setKeyText] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [deleting, setDeleting] = useState<number | null>(null);

  async function handleAdd() {
    setError(null);
    setBusy(true);
    const res = await postSSHKey(title.trim(), keyText.trim());
    setBusy(false);
    if (res.ok) {
      setKeys((prev) => [...prev, res.data]);
      setTitle("");
      setKeyText("");
    } else {
      setError(res.error);
    }
  }

  async function handleRemove(id: number) {
    setError(null);
    setDeleting(id);
    const res = await removeSSHKey(id);
    setDeleting(null);
    if (res.ok) {
      setKeys((prev) => prev.filter((k) => k.id !== id));
    } else {
      setError(res.error);
    }
  }

  return (
    <div className="panel form-narrow">
      <h2>SSH keys</h2>
      <div className="readme-body">
        <p className="subtle">
          Add your public SSH keys to clone and push without a password.
          Paste the contents of{" "}
          <span className="mono">~/.ssh/id_ed25519.pub</span> (or equivalent)
          below.
        </p>
        {error && <div className="form-error">{error}</div>}

        <div className="ssh-key-form">
          <input
            type="text"
            className="token-name-input"
            placeholder="Title (e.g. laptop)"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            aria-label="Key title"
            maxLength={100}
          />
          <textarea
            className="ssh-key-textarea"
            placeholder="ssh-ed25519 AAAA…"
            value={keyText}
            onChange={(e) => setKeyText(e.target.value)}
            aria-label="Public key"
            rows={3}
            spellCheck={false}
          />
          <button
            type="button"
            className="btn primary"
            onClick={handleAdd}
            disabled={busy || !title.trim() || !keyText.trim()}
          >
            {busy ? "Adding…" : "Add SSH key"}
          </button>
        </div>

        {keys.length > 0 && (
          <ul className="token-list">
            {keys.map((k) => (
              <li key={k.id} className="token-row">
                <div className="token-meta">
                  <span className="token-name">{k.title}</span>
                  {k.fingerprint && (
                    <span className="subtle mono">{k.fingerprint}</span>
                  )}
                </div>
                <button
                  type="button"
                  className="btn ghost danger"
                  onClick={() => handleRemove(k.id)}
                  disabled={deleting === k.id}
                >
                  {deleting === k.id ? "Removing…" : "Remove"}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
