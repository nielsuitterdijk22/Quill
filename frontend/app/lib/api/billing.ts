import { authGet } from "./client";
import type { Result } from "./types";

// BillingView mirrors the backend: the tenant's plan, metered usage, and the
// hard limits that apply to the plan (starter/pro/beta; nil = unlimited).
export type BillingView = {
  plan: string;
  usage: {
    repos: number;
    storageBytes: number;
    pipelineMinutes: number;
  };
  limits?: {
    repos?: number;
    storageBytes?: number;
    pipelineMinutes?: number;
  };
  // There is no free tier: "inactive" (never subscribed) and "lapsed" (canceled)
  // are locked until the account subscribes (or, for personal accounts, redeems
  // a beta invite).
  locked?: boolean;
  seats?: number;
  // Seats confirmed on the Creem subscription (Team only; follows membership).
  billedSeats?: number;
};

// getBilling is tenant-admin only on the backend; non-admins get a 403 which
// degrades to { ok: false } here.
export function getOrgBilling(
  token: string,
  org: string,
): Promise<Result<BillingView>> {
  return authGet<BillingView>(token, `/api/v1/orgs/${org}/billing`);
}
