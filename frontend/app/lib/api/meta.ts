import { API_BASE } from "./client";
import type { Meta } from "./types";

export async function getMeta(): Promise<Meta | null> {
  try {
    const res = await fetch(`${API_BASE}/api/v1/meta`, { cache: "no-store" });
    if (!res.ok) return null;
    return (await res.json()) as Meta;
  } catch {
    return null;
  }
}
