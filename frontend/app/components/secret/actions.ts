"use server";

import { revalidatePath } from "next/cache";

import {
  setProjectSecret,
  deleteProjectSecret,
  setRepoSecret,
  deleteRepoSecret,
  setEnvironmentSecret,
  deleteEnvironmentSecret,
} from "../../lib/api";
import { getToken } from "../../lib/session";

// Server actions backing the SecretsManager. Secrets are managed at three scopes
// — project, repository, and environment — so each action is bound to a target
// describing the scope and its slugs. Writes require a project admin (enforced by
// the backend); the actions surface its errors as form state.

export type SecretTarget =
  | { scope: "project"; project: string }
  | { scope: "repo"; project: string; repo: string }
  | { scope: "environment"; project: string; env: string };

export type SecretFormState = { error?: string; ok?: boolean };

// settingsPath is where the manager is rendered, so writes revalidate the right
// route: repo secrets live on the repo settings page, project and environment
// secrets on the project settings page.
function settingsPath(target: SecretTarget): string {
  if (target.scope === "repo") {
    return `/projects/${target.project}/repos/${target.repo}/settings`;
  }
  return `/projects/${target.project}/settings`;
}

// setSecretAction creates or replaces a secret. The value is write-only: the
// backend stores it encrypted and never returns it.
export async function setSecretAction(
  target: SecretTarget,
  _prev: SecretFormState,
  formData: FormData,
): Promise<SecretFormState> {
  const token = await getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const name = String(formData.get("name") ?? "").trim();
  if (!name) return { error: "Enter a secret name, e.g. API_TOKEN." };
  const value = String(formData.get("value") ?? "");
  if (!value) return { error: "Enter a value for the secret." };

  let res: { ok: true } | { ok: false; error: string };
  switch (target.scope) {
    case "project":
      res = await setProjectSecret(token, target.project, name, value);
      break;
    case "repo":
      res = await setRepoSecret(token, target.project, target.repo, name, value);
      break;
    case "environment":
      res = await setEnvironmentSecret(token, target.project, target.env, name, value);
      break;
  }
  if (!res.ok) return { error: res.error };

  revalidatePath(settingsPath(target));
  return { ok: true };
}

// deleteSecretAction removes a secret by name from the bound scope.
export async function deleteSecretAction(
  target: SecretTarget,
  _prev: SecretFormState,
  formData: FormData,
): Promise<SecretFormState> {
  const token = await getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const name = String(formData.get("name") ?? "").trim();
  if (!name) return { error: "Missing secret name." };

  let res: { ok: true } | { ok: false; error: string };
  switch (target.scope) {
    case "project":
      res = await deleteProjectSecret(token, target.project, name);
      break;
    case "repo":
      res = await deleteRepoSecret(token, target.project, target.repo, name);
      break;
    case "environment":
      res = await deleteEnvironmentSecret(token, target.project, target.env, name);
      break;
  }
  if (!res.ok) return { error: res.error };

  revalidatePath(settingsPath(target));
  return { ok: true };
}
