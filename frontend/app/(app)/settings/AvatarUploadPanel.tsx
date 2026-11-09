"use client";

import { useRef, useState } from "react";

// AvatarUploadPanel manages the user's profile picture: upload (PNG/JPEG/WebP/
// GIF, max 2 MiB — enforced by the backend), preview, and remove. The panels
// talk to the backend through the auth-aware /api/backend proxy as raw image
// bytes with the file's content type.
export function AvatarUploadPanel({
  userId,
  hasAvatar,
  name,
}: {
  userId: string;
  hasAvatar: boolean;
  name: string;
}) {
  const fileRef = useRef<HTMLInputElement>(null);
  const [pending, setPending] = useState(false);
  const [present, setPresent] = useState(hasAvatar);
  const [notice, setNotice] = useState<string | null>(null);
  // Cache-busting key so a re-upload immediately shows the new picture.
  const [version, setVersion] = useState(0);

  async function upload(file: File) {
    setPending(true);
    setNotice(null);
    try {
      const res = await fetch("/api/backend/me/avatar", {
        method: "PUT",
        headers: { "Content-Type": file.type },
        body: await file.arrayBuffer(),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as {
          message?: string;
        } | null;
        setNotice(body?.message ?? "Upload failed. Please try again.");
        return;
      }
      setPresent(true);
      setVersion((v) => v + 1);
    } catch {
      setNotice("Upload failed. Please try again.");
    } finally {
      setPending(false);
      if (fileRef.current) fileRef.current.value = "";
    }
  }

  async function remove() {
    setPending(true);
    setNotice(null);
    try {
      const res = await fetch("/api/backend/me/avatar", { method: "DELETE" });
      if (!res.ok) {
        setNotice("Could not remove the picture. Please try again.");
        return;
      }
      setPresent(false);
    } catch {
      setNotice("Could not remove the picture. Please try again.");
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="panel form-narrow">
      <h2>Profile picture</h2>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 16,
          padding: "16px",
        }}
      >
        {present ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            className="profile-avatar"
            src={`/api/backend/users/${encodeURIComponent(userId)}/avatar?v=${version}`}
            alt={name}
          />
        ) : (
          <div className="profile-avatar profile-avatar--fallback">?</div>
        )}
        <div style={{ display: "grid", gap: 8 }}>
          <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
            <input
              ref={fileRef}
              type="file"
              accept="image/png,image/jpeg,image/webp,image/gif"
              disabled={pending}
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (f) void upload(f);
              }}
            />
            {present && (
              <button
                className="btn danger"
                type="button"
                disabled={pending}
                onClick={() => void remove()}
              >
                Remove
              </button>
            )}
          </div>
          {notice && <p className="subtle">{notice}</p>}
        </div>
      </div>
    </div>
  );
}
