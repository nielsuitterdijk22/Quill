"use client";

import { useEffect, useRef, useState } from "react";

import type { MyProject } from "../../lib/api";

// ImportGitHubButton links to the GitHub import flow (reused from onboarding).
// When the user belongs to more than one project, it first asks which
// project to import into, since the flow otherwise defaults to the
// personal project.
export function ImportGitHubButton({ projects }: { projects: MyProject[] }) {
  const [open, setOpen] = useState(false);
  const dialogRef = useRef<HTMLDialogElement>(null);

  const personal = projects.find((p) => p.isPersonal);
  const [target, setTarget] = useState(personal?.slug ?? projects[0]?.slug ?? "");

  useEffect(() => {
    const el = dialogRef.current;
    if (!el) return;
    if (open) el.showModal();
    else el.close();
  }, [open]);

  if (projects.length === 0) return null;

  const hrefFor = (slug: string) =>
    `/onboarding?step=import&project=${encodeURIComponent(slug)}`;

  if (projects.length === 1) {
    return (
      <a className="btn ghost" href={hrefFor(projects[0].slug)}>
        Import from GitHub
      </a>
    );
  }

  return (
    <>
      <button type="button" className="btn ghost" onClick={() => setOpen(true)}>
        Import from GitHub
      </button>

      <dialog ref={dialogRef} className="fork-dialog" onClose={() => setOpen(false)}>
        <h2>Import from GitHub</h2>
        <label className="field">
          <span>Import into project</span>
          <select value={target} onChange={(e) => setTarget(e.target.value)}>
            {projects.map((p) => (
              <option key={p.slug} value={p.slug}>
                {p.name || p.slug}
              </option>
            ))}
          </select>
        </label>
        <div className="form-actions">
          <button type="button" className="pill" onClick={() => setOpen(false)}>
            Cancel
          </button>
          <a className="btn primary" href={hrefFor(target)}>
            Continue
          </a>
        </div>
      </dialog>
    </>
  );
}
