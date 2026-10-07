"use server";

import { provisionSSOTenant, type ProvisionSSOInput } from "../../../lib/api";
import { getToken } from "../../../lib/session";

export type ProvisionResult =
  | { ok: true; slug: string; externalOrgId: string; adminEmails: string[] }
  | { ok: false; error: string };

// provisionSSOTenantAction onboards an SSO customer in one call: create the Quill
// tenant, provision the backing Zitadel org (IdP + ORG_OWNER admin shells +
// external-only login), link them, and seed the admins. The admins receive no
// email — they authenticate through the customer's IdP, and Zitadel auto-links
// them to the pre-created shells by verified email.
export async function provisionSSOTenantAction(
  input: ProvisionSSOInput,
): Promise<ProvisionResult> {
  const token = await getToken();
  if (!token) return { ok: false, error: "Your session has expired. Sign in again." };

  const slug = input.slug.trim();
  const res = await provisionSSOTenant(token, {
    ...input,
    slug,
    name: input.name.trim() || slug,
  });
  if (!res.ok) return { ok: false, error: res.error };
  return { ok: true, slug, externalOrgId: res.data.externalOrgId, adminEmails: input.adminEmails };
}
