import {
  getEnvironments,
  getEnvironmentSecrets,
  getProjectEnvironmentPolicies,
  getProjectPolicies,
  getProjectSecrets,
} from "../../lib/api";
import type {
  BranchPolicy,
  Environment,
  EnvironmentPolicy,
  PipelineSecret,
  Project,
} from "../../lib/api";
import { EnvironmentManager } from "../environment/EnvironmentManager";
import { EnvironmentPolicyManager } from "../policy/EnvironmentPolicyManager";
import { PolicyManager } from "../policy/PolicyManager";
import { SecretsManager } from "../secret/SecretsManager";

// This module holds the project-scoped governance settings — branch policies,
// environments, secrets, environment secrets, and environment policies — as a
// single reusable unit. It is rendered both on a project's own settings page and
// on an individual user's personal-project tab under /settings, so the two stay
// identical. fetchProjectGovernance gathers the data; ProjectGovernanceSettings
// renders it.

// ProjectGovernance is the full data set the governance sections render from.
export type ProjectGovernance = {
  project: Project;
  branchPolicies: BranchPolicy[];
  branchInherited: BranchPolicy[];
  envPolicies: EnvironmentPolicy[];
  envInherited: EnvironmentPolicy[];
  environments: Environment[];
  projectSecrets: PipelineSecret[];
  environmentSecrets: { env: Environment; secrets: PipelineSecret[] }[];
};

export type GovernanceResult =
  | { ok: true; data: ProjectGovernance }
  | { ok: false; status: number; message: string };

// fetchProjectGovernance loads every governance resource for a project. The
// branch-policy call doubles as the access check: a 403/404 there short-circuits
// with an error the caller can surface (banner or notFound). The remaining calls
// degrade to empty lists so one failing section never blanks the whole page.
export async function fetchProjectGovernance(
  token: string,
  project: string,
): Promise<GovernanceResult> {
  const res = await getProjectPolicies(token, project);
  if (!res.ok) {
    return { ok: false, status: res.status, message: res.message };
  }
  const { project: proj, policies, inherited } = res.data;

  const [envRes, environmentsRes, secretsRes] = await Promise.all([
    getProjectEnvironmentPolicies(token, project),
    getEnvironments(token, project),
    getProjectSecrets(token, project),
  ]);

  const environments = environmentsRes.ok ? environmentsRes.data.environments : [];

  // Environment secrets are per-environment; fetch each in parallel. Environment
  // counts are small (capped), so this stays a handful of requests.
  const environmentSecrets = await Promise.all(
    environments.map(async (env) => {
      const r = await getEnvironmentSecrets(token, project, env.slug);
      return { env, secrets: r.ok ? r.data.secrets : [] };
    }),
  );

  return {
    ok: true,
    data: {
      project: proj,
      branchPolicies: policies,
      branchInherited: inherited,
      envPolicies: envRes.ok ? envRes.data.policies : [],
      envInherited: envRes.ok ? envRes.data.inherited : [],
      environments,
      projectSecrets: secretsRes.ok ? secretsRes.data.secrets : [],
      environmentSecrets,
    },
  };
}

// GovernancePolicies renders the policy sections — branch policies and
// environment policies — from pre-fetched data. Split out from secrets so the
// settings page can surface them under a dedicated Policies tab.
export function GovernancePolicies({ data }: { data: ProjectGovernance }) {
  const {
    project,
    branchPolicies,
    branchInherited,
    envPolicies,
    envInherited,
    environments,
  } = data;

  return (
    <>
      <section className="settings-section settings-card">
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
          policies={branchPolicies}
          inherited={branchInherited}
          canLock
          canEdit
        />
      </section>

      <section className="settings-section settings-card">
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
          environments={environments}
          canLock
          canEdit
        />
      </section>
    </>
  );
}

// GovernanceSecretsEnvironments renders the environments and secrets sections
// (project secrets plus each environment's secrets) from pre-fetched data.
export function GovernanceSecretsEnvironments({
  data,
}: {
  data: ProjectGovernance;
}) {
  const { project, environments, projectSecrets, environmentSecrets } = data;

  return (
    <>
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

      <section className="settings-section settings-card">
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
          secrets={projectSecrets}
        />
      </section>

      {environmentSecrets.length > 0 && (
        <section className="settings-section settings-card">
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
    </>
  );
}

// ProjectGovernanceSettings renders the full governance surface — policies then
// secrets and environments — from pre-fetched data. Used on a project's own
// settings page, where everything lives on one page. The individual /settings
// tabs render GovernancePolicies and GovernanceSecretsEnvironments separately.
export function ProjectGovernanceSettings({ data }: { data: ProjectGovernance }) {
  return (
    <>
      <GovernancePolicies data={data} />
      <GovernanceSecretsEnvironments data={data} />
    </>
  );
}
