"use client";

import { useEffect, useRef, useState, useTransition } from "react";
import { useRouter } from "next/navigation";

type ProjectItem = { slug: string; name: string };

export function ForkButton({
  sourceProject,
  sourceRepo,
}: {
  sourceProject: string;
  sourceRepo: string;
}) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [projects, setProjects] = useState<ProjectItem[]>([]);
  const [targetProject, setTargetProject] = useState("");
  const [slug, setSlug] = useState(sourceRepo);
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  const dialogRef = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    if (!open) return;
    fetch("/api/backend/me/projects", { credentials: "include" })
      .then((r) => r.json())
      .then((data: { projects?: ProjectItem[] }) => {
        const list = data.projects ?? [];
        setProjects(list);
        if (list.length > 0 && !targetProject) {
          setTargetProject(list[0].slug);
        }
      })
      .catch(() => {});
  }, [open]);

  useEffect(() => {
    const el = dialogRef.current;
    if (!el) return;
    if (open) {
      el.showModal();
    } else {
      el.close();
    }
  }, [open]);

  function handleClose() {
    setOpen(false);
    setError(null);
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!targetProject || !slug) return;
    setError(null);
    startTransition(async () => {
      try {
        const res = await fetch(
          `/api/backend/projects/${encodeURIComponent(sourceProject)}/repos/${encodeURIComponent(sourceRepo)}/fork`,
          {
            method: "POST",
            credentials: "include",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ targetProject, slug }),
          },
        );
        const data = (await res.json().catch(() => null)) as {
          repository?: { slug: string };
          message?: string;
        } | null;
        if (!res.ok) {
          setError(data?.message ?? `Error ${res.status}`);
          return;
        }
        const newSlug = data?.repository?.slug ?? slug;
        handleClose();
        router.push(
          `/projects/${encodeURIComponent(targetProject)}/repos/${encodeURIComponent(newSlug)}`,
        );
      } catch {
        setError("Could not reach the backend.");
      }
    });
  }

  return (
    <>
      <button type="button" className="pill" onClick={() => setOpen(true)}>
        Fork
      </button>

      <dialog ref={dialogRef} className="fork-dialog" onClose={handleClose}>
        <form onSubmit={handleSubmit}>
          <h2>Fork repository</h2>
          {error && <div className="form-error">{error}</div>}

          <label className="field">
            <span>Fork into project</span>
            <select
              value={targetProject}
              onChange={(e) => setTargetProject(e.target.value)}
              required
              disabled={pending}
            >
              {projects.map((p) => (
                <option key={p.slug} value={p.slug}>
                  {p.name || p.slug}
                </option>
              ))}
            </select>
          </label>

          <label className="field">
            <span>Repository slug</span>
            <input
              type="text"
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
              required
              pattern="[a-z0-9_-]+"
              title="Lowercase letters, numbers, hyphens, underscores"
              disabled={pending}
            />
          </label>

          <div className="form-actions">
            <button
              type="button"
              className="pill"
              onClick={handleClose}
              disabled={pending}
            >
              Cancel
            </button>
            <button
              type="submit"
              className="btn primary"
              disabled={pending || !targetProject}
            >
              {pending ? "Forking…" : "Fork"}
            </button>
          </div>
        </form>
      </dialog>
    </>
  );
}
