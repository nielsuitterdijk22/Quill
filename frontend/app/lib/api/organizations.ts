import { API_BASE } from "./client";

// Organization is an org-kind tenant the signed-in user belongs to, with their
// role in it. Distinct from a Project: an org owns projects and adds member, SSO,
// and org-wide policy management.
export type Organization = {
  slug: string;
  name: string;
  role: string; // 'admin' | 'member'
};

// listOrgs returns the organizations the authenticated user belongs to. Degrades
// to an empty list on any error so nav never blanks the shell.
export async function listOrgs(token: string): Promise<Organization[]> {
  try {
    const res = await fetch(`${API_BASE}/api/v1/orgs`, {
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    });
    if (!res.ok) return [];
    const data = (await res.json()) as { organizations?: Organization[] };
    return data.organizations ?? [];
  } catch {
    return [];
  }
}
