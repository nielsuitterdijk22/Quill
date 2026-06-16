"use server";

import { redirect } from "next/navigation";

import { triggerPipelineRun } from "../../../../../../lib/api";
import { getToken } from "../../../../../../lib/session";

export type TriggerState = { error?: string };

// triggerRunAction runs a workflow manually, then redirects to the new run's
// detail page. The org and repo slugs are bound from the route params.
export async function triggerRunAction(
  org: string,
  repo: string,
  _prev: TriggerState,
  formData: FormData,
): Promise<TriggerState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const workflow = String(formData.get("workflow") ?? "").trim();
  const ref = String(formData.get("ref") ?? "").trim();
  if (!workflow) return { error: "Choose a workflow to run." };

  const result = await triggerPipelineRun(token, org, repo, { workflow, ref });
  if (!result.ok) return { error: result.error };

  const run = result.data.run;
  redirect(
    `/orgs/${org}/repos/${repo}/pipelines/runs/${run.runNumber}?workflow=${encodeURIComponent(workflow)}`,
  );
}
