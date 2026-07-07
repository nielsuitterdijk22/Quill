import Link from "next/link";
import { notFound } from "next/navigation";

import {
  getEnvironments,
  getEnvironmentSecrets,
  getProjectEnvironmentPolicies,
  getProjectPolicies,
  getProjectSecrets,
} from "../../../../lib/api";
import { getToken } from "../../../../lib/session";
import { EnvironmentManager } from "../../../../components/environment/EnvironmentManager";
import { EnvironmentPolicyManager } from "../../../../components/policy/EnvironmentPolicyManager";
import { PolicyManager } from "../../../../components/policy/PolicyManager";
import { SecretsManager } from "../../../../components/secret/SecretsManager";

// ProjectSettingsPage manages project-scoped governance. Branch policies set here
// apply to every repository in the project, and a repo may only tighten them.
// The project itself inherits its tenant's policies, shown read-only.
export default async function ProjectSettingsPage({
  params,
}: {
  params: { project: string };
}) {
  const token = await getToken();
  if (!token) notFound();

  const res = await getProjectPolicies(token, params.project);
  if (!res.ok) {
    if (res.status === 404) notFound();
    return (
      <>
        <div className="crumbs">
          <Link href="/projects">Projects</Link> <span>/</span>{" "}
          <Link href={`/projects/${params.project}`}>{params.project}</Link>{" "}
          <span>/</span> <span>Settings</span>
        </div>
        <h1>Project settings</h1>
        <div className="banner">
          {res.status === 403
            ? "You do not have access to this project's settings."
            : res.message}
        </div>
      </>
    );
  }

  const { project, policies, inherited } = res.data;

  const envRes = await getProjectEnvironmentPolicies(token, params.project);
  const envPolicies = envRes.ok ? envRes.data.policies : [];
  const envInherited = envRes.ok ? envRes.data.inherited : [];

  const environmentsRes = await getEnvironments(token, params.project);
  const environments = environmentsRes.ok ? environmentsRes.data.environments : [];

  const secretsRes = await getProjectSecrets(token, params.project);
  const secrets = secretsRes.ok ? secretsRes.data.secrets : [];

  // Environment secrets are per-environment; fetch each in parallel. Environment
  // counts are small (capped), so this stays a handful of requests.
  const environmentSecrets = await Promise.all(
    environments.map(async (env) => {
      const res = await getEnvironmentSecrets(token, params.project, env.slug);
      return { env, secrets: res.ok ? res.data.secrets : [] };
    }),
  );

  return (
    <>
      <div className="crumbs">
        <Link href="/projects">Projects</Link> <span>/</span>{" "}
        <Link href={`/projects/${project.slug}`}>{project.slug}</Link>{" "}
        <span>/</span> <span>Settings</span>
      </div>

      <div className="top">
        <h1>{project.name} settings</h1>
      </div>

      <section className="settings-section">
        <div className="settings-head">
          <h2 className="settings-title">Branch policies</h2>
          <p className="subtle">
            Rules set here apply to every repository in this project. A
            repository may add stricter rules but cannot weaken these. Lock a
            rule to forbid repositories loosening it.
          </p>
        </div>
        <PolicyManager
          target={{ scope: "project", project: project.slug }}
          policies={policies}
          inherited={inherited}
          canLock
          canEdit
        />
      </section>

      <section className="settings-section">
        <div className="settings-head">
          <h2 className="settings-title">Environments</h2>
          <p className="subtle">
            Deployment targets shared by every repository in this project. Rank
            them to express a promotion ladder (lower deploys first, e.g. staging
            then production); environment policies reference them by slug.
          </p>
        </div>
        <EnvironmentManager project={project.slug} environments={environments} />
      </section>

      <section className="settings-section">
        <div className="settings-head">
          <h2 className="settings-title">Secrets</h2>
          <p className="subtle">
            Encrypted values shared by every repository in this project, exposed
            to workflows as ${"{{ secrets.NAME }}"}. A repository or environment
            secret of the same name overrides one set here.
          </p>
        </div>
        <SecretsManager
          target={{ scope: "project", project: project.slug }}
          secrets={secrets}
        />
      </section>

      {environmentSecrets.length > 0 && (
        <section className="settings-section">
          <div className="settings-head">
            <h2 className="settings-title">Environment secrets</h2>
            <p className="subtle">
              Per-environment values that override project and repository secrets
              of the same name when a run targets that environment.
            </p>
          </div>
          {environmentSecrets.map(({ env, secrets: envSecrets }) => (
            <div key={env.slug} className="settings-subsection">
              <h3 className="mono">
                {env.name}
                <span className="sub"> · {env.slug}</span>
              </h3>
              <SecretsManager
                target={{ scope: "environment", project: project.slug, env: env.slug }}
                secrets={envSecrets}
              />
            </div>
          ))}
        </section>
      )}

      <section className="settings-section">
        <div className="settings-head">
          <h2 className="settings-title">Environment policies</h2>
          <p className="subtle">
            Gate deploys for every repository in this project. A repository may
            add stricter gates but cannot weaken these. Lock a gate to forbid
            repositories loosening it.
          </p>
        </div>
        <EnvironmentPolicyManager
          target={{ scope: "project", project: project.slug }}
          policies={envPolicies}
          inherited={envInherited}
          canLock
          canEdit
        />
      </section>
    </>
  );
}
