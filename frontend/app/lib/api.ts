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
