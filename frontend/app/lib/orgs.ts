import { listOrgs } from "./api";
import { getToken } from "./session";

export function setDefaultOrg(org: string) {
  if (globalThis.window !== undefined) {
    localStorage.setItem("defaultOrg", org);
  }
}

export async function getDefaultOrg(): Promise<string | null> {
  let org = null;
  if (globalThis.window !== undefined) {
    org = localStorage.getItem("defaultOrg");
  }
  if (org) return org;

  const token = getToken();
  if (!token) return null;
  const orgs = await listOrgs(token);

  if (orgs.length > 0) {
    org = orgs[0].slug;
    setDefaultOrg(org);
  }
  return org;
}
