import { authGet, sendData } from "./client";
import type { DataResult } from "./types";

// Pipeline egress controls: an organization (or personal account) policy —
// "allowlist" or "allow_all" — inherited by every repository, which can add
// hosts of its own. `*.example.com` matches subdomains.
export type EgressMode = "allowlist" | "allow_all";
export type EgressGroup = { name: string; hosts: string[] };

export type TenantEgress = {
  tenant: { slug: string; name: string; personal: boolean };
  mode: EgressMode;
  hosts: string[];
  customized: boolean;
  defaults: EgressGroup[];
  required: string[];
  canEdit: boolean;
};

export type RepoEgress = { inherited: TenantEgress; hosts: string[] };

export async function getTenantEgress(token: string, tenant: string | null): Promise<TenantEgress | null> {
  const path = tenant ? `/api/v1/tenants/${encodeURIComponent(tenant)}/egress` : "/api/v1/me/egress";
  const res = await authGet<TenantEgress>(token, path);
  return res.ok ? res.data : null;
}

// setTenantEgress replaces the policy; hosts null resets the allowlist to
// the defaults.
export function setTenantEgress(
  token: string,
  tenant: string | null,
  mode: EgressMode,
  hosts: string[] | null,
): Promise<DataResult<TenantEgress>> {
  const path = tenant ? `/api/v1/tenants/${encodeURIComponent(tenant)}/egress` : "/api/v1/me/egress";
  return sendData<TenantEgress>(token, "PUT", path, { mode, hosts });
}

export async function getRepoEgress(token: string, project: string, repo: string): Promise<RepoEgress | null> {
  const res = await authGet<RepoEgress>(
    token,
    `/api/v1/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}/egress`,
  );
  return res.ok ? res.data : null;
}

export function setRepoEgress(token: string, project: string, repo: string, hosts: string[]): Promise<DataResult<RepoEgress>> {
  return sendData<RepoEgress>(
    token,
    "PUT",
    `/api/v1/projects/${encodeURIComponent(project)}/repos/${encodeURIComponent(repo)}/egress`,
    { hosts },
  );
}
