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

  // GitHub import state
  const [ghRepos, setGhRepos] = useState<GitHubRepo[]>([]);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [loadingRepos, setLoadingRepos] = useState(false);
  const [importResults, setImportResults] = useState<{ name: string; ok: boolean; error?: string }[]>([]);
  const [personalProjectSlug, setPersonalProjectSlug] = useState<string | null>(null);

  // When the OAuth callback redirects back to ?step=import, load the repo list.
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

  // Retrieve the personal project slug from the backend for the import step.
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
    setAccountType(type);
    setError(null);
    if (type === "individual") {
      createPersonalProject();
    }
  }

  function createPersonalProject() {
    startTransition(async () => {
      const token = await getToken();
      // The personal project slug is the username — the backend derives it.
      // We POST to /projects with isPersonal: true via a dedicated endpoint.
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
        body: JSON.stringify({ slug: orgSlug.trim(), name: orgName.trim() || orgSlug.trim(), description: "" }),
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

  // --- render ----------------------------------------------------------------

  if (step === "choose") {
    return (
      <div className="onboarding-card">
        <h1>Welcome to Quill</h1>
        <p className="sub">How are you planning to use Quill?</p>
        {error && <div className="form-error">{error}</div>}
        <div className="onboarding-choices">
          <button
            className="onboarding-choice"
            onClick={() => handleChoose("individual")}
            disabled={pending}
          >
            <span className="onboarding-choice-icon">◧</span>
            <strong>Individual</strong>
            <span className="sub">Just me — personal repos under my username</span>
          </button>
          <button
            className="onboarding-choice"
            onClick={() => handleChoose("org")}
            disabled={pending}
          >
            <span className="onboarding-choice-icon">▤</span>
            <strong>Organization</strong>
            <span className="sub">A team or company with shared repos</span>
          </button>
        </div>

        {accountType === "org" && (
          <div className="onboarding-org-form">
            <label className="form-label">
              Organization slug
              <input
                className="form-input"
                value={orgSlug}
                onChange={(e) => setOrgSlug(e.target.value)}
                placeholder="acme"
                autoFocus
              />
            </label>
            <label className="form-label">
              Display name (optional)
              <input
                className="form-input"
                value={orgName}
                onChange={(e) => setOrgName(e.target.value)}
                placeholder="Acme Inc."
              />
            </label>
            <button className="btn primary" onClick={createOrgProject} disabled={pending}>
              {pending ? "Creating…" : "Continue"}
            </button>
          </div>
        )}
      </div>
    );
  }

  if (step === "import") {
    return (
      <div className="onboarding-card">
        <h1>Import from GitHub</h1>
        <p className="sub">Optionally import your existing GitHub repositories.</p>
        {error && <div className="form-error">{error}</div>}

        {ghRepos.length === 0 && !loadingRepos && (
          <div className="onboarding-github-connect">
            <p>Connect your GitHub account to import repositories.</p>
            <button className="btn primary" onClick={connectGitHub}>
              Connect GitHub
            </button>
          </div>
        )}

        {loadingRepos && <div className="empty">Loading your GitHub repositories…</div>}

        {ghRepos.length > 0 && (
          <>
            <div className="repo-toolbar">
              <label className="checkbox-label">
                <input
                  type="checkbox"
                  checked={selected.size === ghRepos.length}
                  onChange={toggleAll}
                />
                Select all ({ghRepos.length})
              </label>
              <span className="spacer" />
              <span className="sub">{selected.size} selected</span>
            </div>
            <div className="panel">
              {ghRepos.map((repo) => (
                <label key={repo.id} className="row-item checkbox-row">
                  <input
                    type="checkbox"
                    checked={selected.has(repo.id)}
                    onChange={() => toggleRepo(repo.id)}
                  />
                  <div className="pr-main">
                    <span className="nm">{repo.fullName}</span>
                    {repo.description && (
                      <span className="sub">{repo.description}</span>
                    )}
                  </div>
                  <span className="spacer" />
                  {repo.private && <span className="tag">private</span>}
                </label>
              ))}
            </div>
            <div className="onboarding-actions">
              <button className="btn primary" onClick={doImport}>
                Import {selected.size > 0 ? `${selected.size} repos` : "selected"}
              </button>
              <button className="btn" onClick={() => router.push("/repositories")}>
                Skip
              </button>
            </div>
          </>
        )}

        {ghRepos.length === 0 && !loadingRepos && (
          <button
            className="btn"
            style={{ marginTop: "1rem" }}
            onClick={() => router.push("/repositories")}
          >
            Skip
          </button>
        )}
      </div>
    );
  }

  if (step === "importing") {
    return (
      <div className="onboarding-card">
        <h1>Importing…</h1>
        <p className="sub">Migrating your repositories from GitHub. This may take a moment.</p>
        <div className="empty">Please wait…</div>
      </div>
    );
  }

  // done
  const failed = importResults.filter((r) => !r.ok);
  const succeeded = importResults.filter((r) => r.ok);
  return (
    <div className="onboarding-card">
      <h1>You&apos;re all set!</h1>
      {succeeded.length > 0 && (
        <p className="sub">Successfully imported {succeeded.length} repositories.</p>
      )}
      {failed.length > 0 && (
        <div className="form-error">
          {failed.length} repo{failed.length > 1 ? "s" : ""} failed to import:{" "}
          {failed.map((r) => r.name).join(", ")}
        </div>
      )}
      <button className="btn primary" onClick={() => router.push("/repositories")}>
        Go to Repositories
      </button>
    </div>
  );
}
