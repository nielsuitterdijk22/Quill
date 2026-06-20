import { authGet, postData, sendData, deleteResource } from "./client";
import type {
  BranchPolicy,
  BranchPolicyInput,
  PoliciesResult,
  ProjectPoliciesResult,
  TenantPoliciesResult,
  EnvironmentPolicy,
  EnvironmentPolicyInput,
  EnvironmentPoliciesResult,
  ProjectEnvironmentPoliciesResult,
  TenantEnvironmentPoliciesResult,
  Environment,
  EnvironmentsResult,
  CreateEnvironmentInput,
  UpdateEnvironmentInput,
  Result,
  DataResult,
} from "./types";

// ---- repo branch policies --------------------------------------------------

export function getBranchPolicies(
  token: string,
  project: string,
  repo: string,
): Promise<Result<PoliciesResult>> {
  return authGet<PoliciesResult>(token, `/api/v1/projects/${project}/repos/${repo}/policies`);
}

export function setBranchPolicy(
  token: string,
  project: string,
  repo: string,
  input: BranchPolicyInput,
): Promise<DataResult<{ policy: BranchPolicy }>> {
  return sendData(token, "PUT", `/api/v1/projects/${project}/repos/${repo}/policies`, input);
}

export function deleteBranchPolicy(
  token: string,
  project: string,
  repo: string,
  pattern: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(
    token,
    `/api/v1/projects/${project}/repos/${repo}/policies?pattern=${encodeURIComponent(pattern)}`,
  );
}

// ---- project branch policies -----------------------------------------------

export function getProjectPolicies(
  token: string,
  project: string,
): Promise<Result<ProjectPoliciesResult>> {
  return authGet<ProjectPoliciesResult>(token, `/api/v1/projects/${project}/policies`);
}

export function setProjectPolicy(
  token: string,
  project: string,
  input: BranchPolicyInput,
): Promise<DataResult<{ policy: BranchPolicy }>> {
  return sendData(token, "PUT", `/api/v1/projects/${project}/policies`, input);
}

export function deleteProjectPolicy(
  token: string,
  project: string,
  pattern: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(
    token,
    `/api/v1/projects/${project}/policies?pattern=${encodeURIComponent(pattern)}`,
  );
}

// ---- tenant branch policies (admins only) ----------------------------------

export function getTenantPolicies(
  token: string,
  tenant: string,
): Promise<Result<TenantPoliciesResult>> {
  return authGet<TenantPoliciesResult>(token, `/api/v1/tenants/${tenant}/policies`);
}

export function setTenantPolicy(
  token: string,
  tenant: string,
  input: BranchPolicyInput,
): Promise<DataResult<{ policy: BranchPolicy }>> {
  return sendData(token, "PUT", `/api/v1/tenants/${tenant}/policies`, input);
}

export function deleteTenantPolicy(
  token: string,
  tenant: string,
  pattern: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(
    token,
    `/api/v1/tenants/${tenant}/policies?pattern=${encodeURIComponent(pattern)}`,
  );
}

// ---- repo environment policies ---------------------------------------------

export function getEnvironmentPolicies(
  token: string,
  project: string,
  repo: string,
): Promise<Result<EnvironmentPoliciesResult>> {
  return authGet<EnvironmentPoliciesResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/environment-policies`,
  );
}

export function setEnvironmentPolicy(
  token: string,
  project: string,
  repo: string,
  input: EnvironmentPolicyInput,
): Promise<DataResult<{ policy: EnvironmentPolicy }>> {
  return sendData(token, "PUT", `/api/v1/projects/${project}/repos/${repo}/environment-policies`, input);
}

export function deleteEnvironmentPolicy(
  token: string,
  project: string,
  repo: string,
  pattern: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(
    token,
    `/api/v1/projects/${project}/repos/${repo}/environment-policies?pattern=${encodeURIComponent(pattern)}`,
  );
}

// ---- project environment policies -----------------------------------------

export function getProjectEnvironmentPolicies(
  token: string,
  project: string,
): Promise<Result<ProjectEnvironmentPoliciesResult>> {
  return authGet<ProjectEnvironmentPoliciesResult>(
    token,
    `/api/v1/projects/${project}/environment-policies`,
  );
}

export function setProjectEnvironmentPolicy(
  token: string,
  project: string,
  input: EnvironmentPolicyInput,
): Promise<DataResult<{ policy: EnvironmentPolicy }>> {
  return sendData(token, "PUT", `/api/v1/projects/${project}/environment-policies`, input);
}

export function deleteProjectEnvironmentPolicy(
  token: string,
  project: string,
  pattern: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(
    token,
    `/api/v1/projects/${project}/environment-policies?pattern=${encodeURIComponent(pattern)}`,
  );
}

// ---- tenant environment policies (admins only) -----------------------------

export function getTenantEnvironmentPolicies(
  token: string,
  tenant: string,
): Promise<Result<TenantEnvironmentPoliciesResult>> {
  return authGet<TenantEnvironmentPoliciesResult>(
    token,
    `/api/v1/tenants/${tenant}/environment-policies`,
  );
}

export function setTenantEnvironmentPolicy(
  token: string,
  tenant: string,
  input: EnvironmentPolicyInput,
): Promise<DataResult<{ policy: EnvironmentPolicy }>> {
  return sendData(token, "PUT", `/api/v1/tenants/${tenant}/environment-policies`, input);
}

export function deleteTenantEnvironmentPolicy(
  token: string,
  tenant: string,
  pattern: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(
    token,
    `/api/v1/tenants/${tenant}/environment-policies?pattern=${encodeURIComponent(pattern)}`,
  );
}

// ---- environments ----------------------------------------------------------

export function getEnvironments(
  token: string,
  project: string,
): Promise<Result<EnvironmentsResult>> {
  return authGet<EnvironmentsResult>(token, `/api/v1/projects/${project}/environments`);
}

export function createEnvironment(
  token: string,
  project: string,
  input: CreateEnvironmentInput,
): Promise<DataResult<Environment>> {
  return postData(token, `/api/v1/projects/${project}/environments`, input);
}

export function updateEnvironment(
  token: string,
  project: string,
  env: string,
  input: UpdateEnvironmentInput,
): Promise<DataResult<Environment>> {
  return sendData(token, "PATCH", `/api/v1/projects/${project}/environments/${env}`, input);
}

export function deleteEnvironment(
  token: string,
  project: string,
  env: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(token, `/api/v1/projects/${project}/environments/${env}`);
}
