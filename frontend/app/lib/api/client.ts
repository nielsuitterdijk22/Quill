// Core HTTP helpers shared across all API domain modules.

export const API_BASE = process.env.QUILL_API_BASE_URL || "http://localhost:8080";

import type { Result, DataResult, MutationResult, AuthResult, User } from "./types";

export async function authGet<T>(token: string, path: string): Promise<Result<T>> {
  try {
    const res = await fetch(`${API_BASE}${path}`, {
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    });
    if (!res.ok) {
      const body = (await res.json().catch(() => null)) as { message?: string } | null;
      return { ok: false, status: res.status, message: body?.message || `Request failed (${res.status}).` };
    }
    return { ok: true, data: (await res.json()) as T };
  } catch {
    return { ok: false, status: 0, message: "Can't reach the Quill backend." };
  }
}

export async function postAuth(path: string, body: unknown): Promise<AuthResult> {
  try {
    const res = await fetch(`${API_BASE}${path}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      cache: "no-store",
    });
    const data = (await res.json().catch(() => null)) as
      | { token?: string; user?: User; message?: string }
      | null;
    if (!res.ok || !data?.token || !data?.user) {
      return { ok: false, error: data?.message || "Authentication failed." };
    }
    return { ok: true, token: data.token, user: data.user };
  } catch {
    return { ok: false, error: "Can't reach the Quill backend." };
  }
}

export async function postData<T>(token: string, path: string, body: unknown): Promise<DataResult<T>> {
  return sendData<T>(token, "POST", path, body);
}

export async function sendData<T>(
  token: string,
  method: "POST" | "PUT" | "PATCH",
  path: string,
  body: unknown,
): Promise<DataResult<T>> {
  try {
    const res = await fetch(`${API_BASE}${path}`, {
      method,
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
      body: JSON.stringify(body),
      cache: "no-store",
    });
    const data = (await res.json().catch(() => null)) as (T & { message?: string }) | null;
    if (!res.ok || !data) {
      return { ok: false, error: data?.message || `Request failed (${res.status}).` };
    }
    return { ok: true, data: data as T };
  } catch {
    return { ok: false, error: "Can't reach the Quill backend." };
  }
}

export async function putResource(
  token: string,
  path: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    const res = await fetch(`${API_BASE}${path}`, {
      method: "PUT",
      headers: { Authorization: `Bearer ${token}` },
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

export async function deleteResource(
  token: string,
  path: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    const res = await fetch(`${API_BASE}${path}`, {
      method: "DELETE",
      headers: { Authorization: `Bearer ${token}` },
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

export async function postNoContent(
  token: string,
  path: string,
  body: unknown,
): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    const res = await fetch(`${API_BASE}${path}`, {
      method: "POST",
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
      body: JSON.stringify(body),
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

export async function sendNoContent(
  token: string,
  method: "PATCH" | "PUT",
  path: string,
  body: unknown,
): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    const res = await fetch(`${API_BASE}${path}`, {
      method,
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
      body: JSON.stringify(body),
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

export async function postCreate(
  token: string,
  path: string,
  body: unknown,
): Promise<MutationResult> {
  try {
    const res = await fetch(`${API_BASE}${path}`, {
      method: "POST",
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
      body: JSON.stringify(body),
      cache: "no-store",
    });
    const data = (await res.json().catch(() => null)) as { slug?: string; message?: string } | null;
    if (!res.ok || !data?.slug) {
      return { ok: false, error: data?.message || `Request failed (${res.status}).` };
    }
    return { ok: true, slug: data.slug };
  } catch {
    return { ok: false, error: "Can't reach the Quill backend." };
  }
}
