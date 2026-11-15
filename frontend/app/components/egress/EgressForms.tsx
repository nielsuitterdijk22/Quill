"use client";

import { useState } from "react";
import { useFormState, useFormStatus } from "react-dom";

import type { EgressMode, RepoEgress, TenantEgress } from "../../lib/api";
import { saveEgressAction, type EgressFormState, type EgressTarget } from "./actions";

const initial: EgressFormState = {};

function SaveButton({ label }: { label: string }) {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" name="intent" value="save" disabled={pending}>
      {pending ? "Saving…" : label}
    </button>
  );
}

function Status({ state }: { state: EgressFormState }) {
  return (
    <>
      {state.error && <div className="form-error">{state.error}</div>}
      {state.ok && <div className="form-success">{state.ok}</div>}
    </>
  );
}

// AlwaysAllowed lists the hosts the platform adds to every allowlist.
function AlwaysAllowed({ hosts }: { hosts: string[] }) {
  return (
    <p className="hint egress-required">
      Always allowed: {hosts.map((h) => <code key={h}>{h}</code>)} and the registry of the runner image.
    </p>
  );
}

// TenantEgressForm edits an organization's (or a personal account's)
// policy: allow all, or an allowlist every repository inherits.
export function TenantEgressForm({ policy, tenant, path }: { policy: TenantEgress; tenant: string | null; path: string }) {
  const target: EgressTarget = { scope: "tenant", tenant, path };
  const [state, formAction] = useFormState(saveEgressAction.bind(null, target), initial);
  const [mode, setMode] = useState<EgressMode>(policy.mode);
  const disabled = !policy.canEdit;

  return (
    <form action={formAction} className="egress-form">
      <Status state={state} />
      <div className="egress-modes" role="radiogroup" aria-label="Egress mode">
        <label className={`egress-mode${mode === "allowlist" ? " on" : ""}`}>
          <input type="radio" name="mode" value="allowlist" checked={mode === "allowlist"} disabled={disabled} onChange={() => setMode("allowlist")} />
          <span>
            <b>Allowlist</b>
            <span className="subtle">Pipelines reach only the hosts below. Recommended.</span>
          </span>
        </label>
        <label className={`egress-mode${mode === "allow_all" ? " on" : ""}`}>
          <input type="radio" name="mode" value="allow_all" checked={mode === "allow_all"} disabled={disabled} onChange={() => setMode("allow_all")} />
          <span>
            <b>Allow all</b>
            <span className="subtle">No restriction on outbound traffic.</span>
          </span>
        </label>
      </div>

      {mode === "allowlist" ? (
        <>
          <label className="field">
            <span>
              Allowed hosts, one per line{" "}
              {policy.customized ? <span className="tag">customized</span> : <span className="tag">defaults</span>}
            </span>
            <textarea
              key={policy.hosts.join(",")}
              name="hosts"
              rows={12}
              className="mono"
              defaultValue={policy.hosts.join("\n")}
              spellCheck={false}
              disabled={disabled}
            />
          </label>
          <p className="hint">
            <code>*.example.com</code> matches every subdomain of example.com (not example.com itself). HTTP and
            HTTPS only. Repositories can add hosts of their own.
          </p>
          <AlwaysAllowed hosts={policy.required} />
          <details className="egress-defaults">
            <summary>What the defaults cover</summary>
            <dl>
              {policy.defaults.map((g) => (
                <div key={g.name}>
                  <dt>{g.name}</dt>
                  <dd>{g.hosts.join(", ")}</dd>
                </div>
              ))}
            </dl>
          </details>
        </>
      ) : (
        <>
          {/* Keep the stored list when switching to allow all and back. */}
          <input type="hidden" name="hosts" value={policy.hosts.join("\n")} />
          <p className="hint">Repository host lists have no effect while all traffic is allowed.</p>
        </>
      )}

      {!disabled && (
        <div className="form-actions">
          <SaveButton label="Save egress policy" />
          {mode === "allowlist" && policy.customized && (
            <button className="btn" type="submit" name="intent" value="reset">
              Reset to defaults
            </button>
          )}
        </div>
      )}
    </form>
  );
}

// RepoEgressForm shows what a repository inherits and edits its extra hosts.
export function RepoEgressForm({ policy, project, repo }: { policy: RepoEgress; project: string; repo: string }) {
  const path = `/projects/${project}/repos/${repo}/settings`;
  const target: EgressTarget = { scope: "repo", project, repo, path };
  const [state, formAction] = useFormState(saveEgressAction.bind(null, target), initial);
  const inh = policy.inherited;
  // Link to where the inherited policy is edited (org or account settings).
  const where = inh.canEdit ? (
    <a href={inh.tenant.personal ? "/settings#egress" : `/orgs/${inh.tenant.slug}/settings#egress`}>{inh.tenant.name || inh.tenant.slug}</a>
  ) : (
    <b>{inh.tenant.name || inh.tenant.slug}</b>
  );

  return (
    <form action={formAction} className="egress-form">
      <Status state={state} />
      {inh.mode === "allow_all" ? (
        <div className="banner">Pipelines may reach any host: {where} allows all outbound traffic.</div>
      ) : (
        <details className="egress-defaults">
          <summary>
            Inherited from {where}: {inh.hosts.length} hosts{inh.customized ? "" : " (the defaults)"}
          </summary>
          <p className="mono egress-inherited">{inh.hosts.join("  ")}</p>
        </details>
      )}
      <label className="field">
        <span>Additional hosts for this repository, one per line</span>
        <textarea
          key={policy.hosts.join(",")}
          name="hosts"
          rows={4}
          className="mono"
          defaultValue={policy.hosts.join("\n")}
          placeholder={"artifacts.example.com\n*.s3.example-cloud.com"}
          spellCheck={false}
        />
      </label>
      <AlwaysAllowed hosts={inh.required} />
      <div className="form-actions">
        <SaveButton label="Save hosts" />
      </div>
    </form>
  );
}
