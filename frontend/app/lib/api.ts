// Server-side API client for the Quill backend.
//
// Browser code should call the rewrite at /api/backend/* (see next.config.mjs);
// server components use QUILL_API_BASE_URL directly. Keep all backend response
// types defined here so pages stay decoupled from fetch details.

const API_BASE = process.env.QUILL_API_BASE_URL || "http://localhost:8080";

export type Meta = {
  name: string;
  version: string;
  env: string;
};

export type User = {
  id: string;
  username: string;
  email: string;
  displayName: string;
  isAdmin: boolean;
  isActive: boolean;
  createdAt: string;
};

export type AuthOk = { ok: true; token: string; user: User };
export type AuthErr = { ok: false; error: string };
export type AuthResult = AuthOk | AuthErr;

export type RegisterInput = {
  username: string;
  email: string;
  displayName?: string;
  password: string;
};

// getMeta fetches backend metadata. Returns null if the backend is unreachable
// so pages can render a degraded state instead of crashing.
export async function getMeta(): Promise<Meta | null> {
  try {
    const res = await fetch(`${API_BASE}/api/v1/meta`, { cache: "no-store" });
    if (!res.ok) return null;
    return (await res.json()) as Meta;
  } catch {
    return null;
  }
}

// login exchanges credentials for a token + user. Network and auth failures are
// returned as { ok: false } so callers can surface a friendly message.
export async function login(
  username: string,
  password: string,
): Promise<AuthResult> {
  return postAuth("/api/v1/auth/login", { username, password });
}

// register creates an account and returns a token + user (the first account
// created becomes an admin).
export async function register(input: RegisterInput): Promise<AuthResult> {
  return postAuth("/api/v1/auth/register", input);
}

// fetchMe resolves the current user for a token, or null if it is missing/invalid.
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

async function postAuth(path: string, body: unknown): Promise<AuthResult> {
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
