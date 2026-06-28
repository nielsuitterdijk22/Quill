"use client";

import { useEffect, useState, useTransition } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useAuth } from "@clerk/nextjs";

type GitHubRepo = {
  id: number;
  name: string;
  fullName: string;
  description: string;
  private: boolean;
  cloneUrl: string;
};

type Step = "choose" | "import" | "importing" | "done";

function IconUser() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="12" cy="8" r="4" />
      <path d="M4 20c0-4 3.582-7 8-7s8 3 8 7" />
    </svg>
  );
}

function IconBuilding() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <rect x="2" y="7" width="9" height="14" rx="1.5" />
      <rect x="13" y="2" width="9" height="19" rx="1.5" />
      <path d="M5 11h3M5 15h3M16 6h3M16 10h3M16 14h3M16 18h3" />
    </svg>
  );
}

function IconGitHub() {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor">
      <path d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.166 6.839 9.489.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.604-3.369-1.341-3.369-1.341-.454-1.155-1.11-1.463-1.11-1.463-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.578 9.578 0 0 1 12 6.836a9.59 9.59 0 0 1 2.504.337c1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.202 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.743 0 .267.18.579.688.481C19.138 20.163 22 16.418 22 12c0-5.523-4.477-10-10-10z" />
    </svg>
  );
}

function IconCheck() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
      <polyline points="20 6 9 17 4 12" />
    </svg>
  );
}

function IconSpinner() {
  return (
    <svg className="ob-spinner" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
      <circle cx="12" cy="12" r="10" strokeOpacity="0.2" />
      <path d="M12 2a10 10 0 0 1 10 10" />
    </svg>
  );
}

export default function OnboardingPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { getToken } = useAuth();

  const [step, setStep] = useState<Step>(
    searchParams.get("step") === "import" ? "import" : "choose",
  );
  const [accountType, setAccountType] = useState<"individual" | "org" | null>(null);
  const [orgSlug, setOrgSlug] = useState("");
  const [orgName, setOrgName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();

  const [ghRepos, setGhRepos] = useState<GitHubRepo[]>([]);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [loadingRepos, setLoadingRepos] = useState(false);
  const [importResults, setImportResults] = useState<{ name: string; ok: boolean; error?: string }[]>([]);
  const [personalProjectSlug, setPersonalProjectSlug] = useState<string | null>(null);

  useEffect(() => {
    if (step !== "import") return;
    setLoadingRepos(true);
    getToken().then((token) => {
      fetch("/api/backend/import/github/repos", {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      })
        .then((r) => r.json())
        .then((body: { repos?: GitHubRepo[] }) => {
          const repos = body.repos ?? [];
          setGhRepos(repos);
          setSelected(new Set(repos.map((r) => r.id)));
        })
        .catch(() => setError("Could not load your GitHub repositories."))
        .finally(() => setLoadingRepos(false));
    });
  }, [step, getToken]);

  useEffect(() => {
    if (step !== "import") return;
    getToken().then(async (token) => {
      const res = await fetch("/api/backend/me/projects", {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      });
      if (!res.ok) return;
      const body = (await res.json()) as { projects?: { slug: string; isPersonal: boolean }[] };
      const personal = body.projects?.find((p) => p.isPersonal);
      if (personal) setPersonalProjectSlug(personal.slug);
    });
  }, [step, getToken]);

  function handleChoose(type: "individual" | "org") {
    setError(null);
    if (type === "org") {
      setAccountType("org");
      return;
    }
    setAccountType("individual");
    startTransition(async () => {
      const token = await getToken();
      const res = await fetch("/api/backend/me/personal-project", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as { message?: string } | null;
        setError(body?.message ?? "Could not create your personal workspace.");
        return;
      }
      const body = (await res.json()) as { slug?: string };
      setPersonalProjectSlug(body.slug ?? null);
      setStep("import");
    });
  }

  function createOrgProject() {
    if (!orgSlug.trim()) {
      setError("Slug is required.");
      return;
    }
    startTransition(async () => {
      const token = await getToken();
      const res = await fetch("/api/backend/projects", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: JSON.stringify({
          slug: orgSlug.trim(),
          name: orgName.trim() || orgSlug.trim(),
          description: "",
        }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as { message?: string } | null;
        setError(body?.message ?? "Could not create organization.");
        return;
      }
      setStep("import");
    });
  }

  function connectGitHub() {
    window.location.href = "/api/backend/auth/github";
  }

  function toggleRepo(id: number) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function toggleAll() {
    if (selected.size === ghRepos.length) setSelected(new Set());
    else setSelected(new Set(ghRepos.map((r) => r.id)));
  }

  function doImport() {
    const toImport = ghRepos.filter((r) => selected.has(r.id));
    if (toImport.length === 0) {
      router.push("/repositories");
      return;
    }
    const slug = personalProjectSlug;
    if (!slug) {
      setError("Could not determine your project. Please refresh.");
      return;
    }
    setStep("importing");
    getToken().then(async (token) => {
      const res = await fetch("/api/backend/import/github", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: JSON.stringify({
          projectSlug: slug,
          repos: toImport.map((r) => ({
            name: r.name,
            cloneUrl: r.cloneUrl,
            description: r.description,
            private: r.private,
          })),
        }),
      });
      const body = (await res.json().catch(() => null)) as {
        results?: { name: string; ok: boolean; error?: string }[];
      } | null;
      setImportResults(body?.results ?? []);
      setStep("done");
    });
  }

  // ── choose ──────────────────────────────────────────────────────────────────

  if (step === "choose") {
    return (
      <div className="ob-shell">
        <header className="ob-header">
          <div className="ob-brand">
            <span className="dot" />
            Quill
          </div>
          <span className="ob-step-label">Step 1 of 2</span>
        </header>

        <main className="ob-main">
          <div className="ob-headline">
            <h1 className="ob-title">How will you use Quill?</h1>
            <p className="ob-sub">Pick a setup — you can always add more workspaces later.</p>
          </div>

          {error && <div className="ob-error">{error}</div>}

          <div className="ob-cards">
            {/* Individual */}
            <button
              className={`ob-card${accountType === "individual" ? " ob-card--active" : ""}`}
              onClick={() => handleChoose("individual")}
              disabled={pending && accountType === "individual"}
            >
              <div className="ob-card-icon ob-card-icon--indigo">
                <IconUser />
              </div>
              <div className="ob-card-body">
                <strong className="ob-card-title">Individual</strong>
                <p className="ob-card-desc">
                  Personal namespace under <em>@your-username</em> — great for solo projects, open-source, or side hustles.
                </p>
                <ul className="ob-card-bullets">
                  <li>Repos at <code>@you/project-name</code></li>
                  <li>Public &amp; private repositories</li>
                  <li>Import from GitHub in seconds</li>
                </ul>
              </div>
              <div className="ob-card-footer">
                {pending && accountType === "individual" ? (
                  <span className="ob-card-cta ob-card-cta--loading">
                    <IconSpinner /> Setting up…
                  </span>
                ) : (
                  <span className="ob-card-cta">Get started →</span>
                )}
              </div>
            </button>

            {/* Organization */}
            <button
              className={`ob-card${accountType === "org" ? " ob-card--active" : ""}`}
              onClick={() => handleChoose("org")}
              disabled={pending}
            >
              <div className="ob-card-icon ob-card-icon--teal">
                <IconBuilding />
              </div>
              <div className="ob-card-body">
                <strong className="ob-card-title">Organization</strong>
                <p className="ob-card-desc">
                  A shared workspace for a team or company — custom slug, shared access, unified policies.
                </p>
                <ul className="ob-card-bullets">
                  <li>Custom org slug</li>
                  <li>Role-based member access</li>
                  <li>Org-wide branch policies</li>
                </ul>
              </div>
              <div className="ob-card-footer">
                <span className="ob-card-cta">Configure →</span>
              </div>
            </button>
          </div>

          {accountType === "org" && (
            <div className="ob-org-form">
              <h2 className="ob-org-form-title">Set up your organization</h2>
              <div className="ob-field">
                <label className="ob-label">Slug <span className="ob-label-hint">— becomes part of every URL</span></label>
                <input
                  className="ob-input"
                  value={orgSlug}
                  onChange={(e) => setOrgSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))}
                  placeholder="acme"
                  autoFocus
                  onKeyDown={(e) => e.key === "Enter" && createOrgProject()}
                />
                {orgSlug && (
                  <span className="ob-preview">
                    Repos will live at <code>{orgSlug}/repo-name</code>
                  </span>
                )}
              </div>
              <div className="ob-field">
                <label className="ob-label">Display name <span className="ob-label-hint">— optional</span></label>
                <input
                  className="ob-input"
                  value={orgName}
                  onChange={(e) => setOrgName(e.target.value)}
                  placeholder="Acme Inc."
                  onKeyDown={(e) => e.key === "Enter" && createOrgProject()}
                />
              </div>
              <button className="ob-btn-primary" onClick={createOrgProject} disabled={pending}>
                {pending ? <><IconSpinner /> Creating…</> : "Create workspace →"}
              </button>
            </div>
          )}
        </main>
      </div>
    );
  }

  // ── import ──────────────────────────────────────────────────────────────────

  if (step === "import") {
    return (
      <div className="ob-shell">
        <header className="ob-header">
          <div className="ob-brand">
            <span className="dot" />
            Quill
          </div>
          <span className="ob-step-label">Step 2 of 2</span>
        </header>

        <main className="ob-main ob-main--narrow">
          <div className="ob-headline">
            <h1 className="ob-title">Bring your repos</h1>
            <p className="ob-sub">Import from GitHub — or skip and create repos manually.</p>
          </div>

          {error && <div className="ob-error">{error}</div>}

          {!loadingRepos && ghRepos.length === 0 && (
            <div className="ob-gh-connect">
              <div className="ob-gh-icon">
                <IconGitHub />
              </div>
              <h2 className="ob-gh-title">Connect GitHub</h2>
              <p className="ob-gh-desc">
                Authorize Quill to read your repos. We'll mirror them into Quill — your GitHub stays untouched.
              </p>
              <button className="ob-btn-gh" onClick={connectGitHub}>
                <IconGitHub />
                Continue with GitHub
              </button>
              <button className="ob-btn-skip" onClick={() => router.push("/repositories")}>
                Skip for now →
              </button>
            </div>
          )}

          {loadingRepos && (
            <div className="ob-loading">
              <IconSpinner />
              <span>Loading your repositories…</span>
            </div>
          )}

          {ghRepos.length > 0 && (
            <div className="ob-repo-section">
              <div className="ob-repo-toolbar">
                <label className="ob-check-all">
                  <input
                    type="checkbox"
                    checked={selected.size === ghRepos.length}
                    onChange={toggleAll}
                  />
                  Select all
                </label>
                <span className="ob-repo-count">{selected.size} / {ghRepos.length} selected</span>
              </div>

              <div className="ob-repo-list">
                {ghRepos.map((repo) => (
                  <label
                    key={repo.id}
                    className={`ob-repo-row${selected.has(repo.id) ? " ob-repo-row--checked" : ""}`}
                  >
                    <input
                      type="checkbox"
                      checked={selected.has(repo.id)}
                      onChange={() => toggleRepo(repo.id)}
                    />
                    <div className="ob-repo-info">
                      <span className="ob-repo-name">{repo.fullName}</span>
                      {repo.description && (
                        <span className="ob-repo-desc">{repo.description}</span>
                      )}
                    </div>
                    {repo.private && <span className="ob-repo-badge">private</span>}
                  </label>
                ))}
              </div>

              <div className="ob-import-actions">
                <button className="ob-btn-primary" onClick={doImport}>
                  Import {selected.size > 0 ? `${selected.size} repo${selected.size !== 1 ? "s" : ""}` : "selected"}
                </button>
                <button className="ob-btn-ghost" onClick={() => router.push("/repositories")}>
                  Skip
                </button>
              </div>
            </div>
          )}
        </main>
      </div>
    );
  }

  // ── importing ────────────────────────────────────────────────────────────────

  if (step === "importing") {
    return (
      <div className="ob-shell">
        <header className="ob-header">
          <div className="ob-brand">
            <span className="dot" />
            Quill
          </div>
        </header>
        <main className="ob-main ob-main--narrow ob-main--centered">
          <div className="ob-progress">
            <div className="ob-progress-spinner">
              <IconSpinner />
            </div>
            <h1 className="ob-title">Importing…</h1>
            <p className="ob-sub">Mirroring your repositories from GitHub. This can take a minute for large repos.</p>
          </div>
        </main>
      </div>
    );
  }

  // ── done ─────────────────────────────────────────────────────────────────────

  const failed = importResults.filter((r) => !r.ok);
  const succeeded = importResults.filter((r) => r.ok);

  return (
    <div className="ob-shell">
      <header className="ob-header">
        <div className="ob-brand">
          <span className="dot" />
          Quill
        </div>
      </header>
      <main className="ob-main ob-main--narrow ob-main--centered">
        <div className="ob-done">
          <div className="ob-done-icon">
            <IconCheck />
          </div>
          <h1 className="ob-title">You&apos;re all set!</h1>
          {succeeded.length > 0 && (
            <p className="ob-sub">
              Successfully imported {succeeded.length} repo{succeeded.length !== 1 ? "s" : ""}.
            </p>
          )}
          {failed.length > 0 && (
            <div className="ob-error ob-error--list">
              <strong>{failed.length} repo{failed.length > 1 ? "s" : ""} failed:</strong>
              <ul>
                {failed.map((r) => (
                  <li key={r.name}>{r.name}{r.error ? ` — ${r.error}` : ""}</li>
                ))}
              </ul>
            </div>
          )}
          <button className="ob-btn-primary" onClick={() => router.push("/repositories")}>
            Go to repositories →
          </button>
        </div>
      </main>
    </div>
  );
}
