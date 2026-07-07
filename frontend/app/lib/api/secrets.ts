import { authGet, sendData, deleteResource } from "./client";
import type { PipelineSecret, SecretsResult, DataResult, Result } from "./types";

// Pipeline-secret API client. Secrets are managed at three scopes — project,
// repository, and environment — and are write-only: the list endpoints return
// names and timestamps only, never values. Every call requires a project admin
// (enforced by the backend).

// ---- project-scoped secrets ------------------------------------------------

export function getProjectSecrets(
  token: string,
  project: string,
): Promise<Result<SecretsResult>> {
  return authGet<SecretsResult>(token, `/api/v1/projects/${project}/secrets`);
}

export function setProjectSecret(
  token: string,
  project: string,
  name: string,
  value: string,
): Promise<DataResult<PipelineSecret>> {
  return sendData<PipelineSecret>(
    token,
    "PUT",
    `/api/v1/projects/${project}/secrets/${encodeURIComponent(name)}`,
    { value },
  );
}

export function deleteProjectSecret(
  token: string,
  project: string,
  name: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(
    token,
    `/api/v1/projects/${project}/secrets/${encodeURIComponent(name)}`,
  );
}

// ---- repository-scoped secrets ---------------------------------------------

export function getRepoSecrets(
  token: string,
  project: string,
  repo: string,
): Promise<Result<SecretsResult>> {
  return authGet<SecretsResult>(
    token,
    `/api/v1/projects/${project}/repos/${repo}/secrets`,
  );
}

export function setRepoSecret(
  token: string,
  project: string,
  repo: string,
  name: string,
  value: string,
): Promise<DataResult<PipelineSecret>> {
  return sendData<PipelineSecret>(
    token,
    "PUT",
    `/api/v1/projects/${project}/repos/${repo}/secrets/${encodeURIComponent(name)}`,
    { value },
  );
}

export function deleteRepoSecret(
  token: string,
  project: string,
  repo: string,
  name: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(
    token,
    `/api/v1/projects/${project}/repos/${repo}/secrets/${encodeURIComponent(name)}`,
  );
}

// ---- environment-scoped secrets --------------------------------------------

export function getEnvironmentSecrets(
  token: string,
  project: string,
  env: string,
): Promise<Result<SecretsResult>> {
  return authGet<SecretsResult>(
    token,
    `/api/v1/projects/${project}/environments/${env}/secrets`,
  );
}

export function setEnvironmentSecret(
  token: string,
  project: string,
  env: string,
  name: string,
  value: string,
): Promise<DataResult<PipelineSecret>> {
  return sendData<PipelineSecret>(
    token,
    "PUT",
    `/api/v1/projects/${project}/environments/${env}/secrets/${encodeURIComponent(name)}`,
    { value },
  );
}

export function deleteEnvironmentSecret(
  token: string,
  project: string,
  env: string,
  name: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(
    token,
    `/api/v1/projects/${project}/environments/${env}/secrets/${encodeURIComponent(name)}`,
  );
}
