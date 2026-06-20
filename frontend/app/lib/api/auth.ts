import { API_BASE, authGet, postAuth, postData, sendData, deleteResource } from "./client";
import type {
  AuthResult,
  RegisterInput,
  User,
  DataResult,
  Result,
  GitCredential,
  GitTokenSummary,
  SSHKey,
} from "./types";

export function login(username: string, password: string): Promise<AuthResult> {
  return postAuth("/api/v1/auth/login", { username, password });
}

export function register(input: RegisterInput): Promise<AuthResult> {
  return postAuth("/api/v1/auth/register", input);
}

export async function fetchMe(token: string): Promise<User | null> {
  try {
    const res = await fetch(`${API_BASE}/api/v1/auth/me`, {
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    });
    if (!res.ok) return null;
    return (await res.json()) as User;
  } catch {
    return null;
  }
}

export function updateProfile(token: string, input: { displayName: string }): Promise<DataResult<User>> {
  return sendData<User>(token, "PATCH", "/api/v1/auth/me", input);
}

export function updateEmail(token: string, email: string): Promise<DataResult<User>> {
  return sendData<User>(token, "PATCH", "/api/v1/auth/me/email", { email });
}

export async function changePassword(
  token: string,
  currentPassword: string,
  newPassword: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    const res = await fetch(`${API_BASE}/api/v1/auth/me/password`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
      body: JSON.stringify({ currentPassword, newPassword }),
      cache: "no-store",
    });
    if (!res.ok) {
      const data = (await res.json().catch(() => null)) as { message?: string } | null;
      return { ok: false, error: data?.message || `Request failed (${res.status}).` };
    }
    return { ok: true };
  } catch {
    return { ok: false, error: "Can't reach the Quill backend." };
  }
}

export function deleteMyAccount(token: string): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(token, "/api/v1/auth/me");
}

export function createGitToken(token: string, name?: string): Promise<DataResult<GitCredential>> {
  return postData<GitCredential>(token, "/api/v1/me/git-token", { name: name ?? "" });
}

export async function listGitTokens(token: string): Promise<Result<GitTokenSummary[]>> {
  return authGet<GitTokenSummary[]>(token, "/api/v1/me/git-tokens");
}

export function revokeGitToken(
  token: string,
  id: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(token, `/api/v1/me/git-tokens/${id}`);
}

export async function listSSHKeys(token: string): Promise<Result<SSHKey[]>> {
  const res = await authGet<{ keys: SSHKey[] }>(token, "/api/v1/me/ssh-keys");
  if (!res.ok) return res;
  return { ok: true, data: res.data.keys };
}

export function addSSHKey(token: string, title: string, key: string): Promise<DataResult<SSHKey>> {
  return postData<SSHKey>(token, "/api/v1/me/ssh-keys", { title, key });
}

export function deleteSSHKey(
  token: string,
  id: number,
): Promise<{ ok: true } | { ok: false; error: string }> {
  return deleteResource(token, `/api/v1/me/ssh-keys/${id}`);
}
