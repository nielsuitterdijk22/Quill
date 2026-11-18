"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { useFormState, useFormStatus } from "react-dom";

import { startUpgradeAction } from "../lib/actions";

type PlanId = "starter" | "pro" | "team";

const PLANS: {
  id: PlanId;
  name: string;
  price: string;
  per: string;
  tagline: string;
  badge?: string;
  features: string[];
}[] = [
  {
    id: "starter",
    name: "Starter",
    price: "€4",
    per: "/ month",
    tagline: "For side projects and solo work.",
    features: ["Unlimited repositories", "5 GB git storage", "100 CI minutes / month", "Private repositories", "SBOM export"],
  },
  {
    id: "pro",
    name: "Pro",
    price: "€9",
    per: "/ month",
    tagline: "For serious personal projects.",
    badge: "Most popular",
    features: [
      "Everything in Starter",
      "10 GB git storage + 1 GB LFS",
      "500 CI minutes / month",
      "Self-hosted runners",
      "Priority support",
    ],
  },
  {
    id: "team",
    name: "Team",
    price: "€11",
    per: "/ user / month",
    tagline: "For organizations.",
    features: ["Everything in Pro", "Unlimited storage and CI minutes", "SSO and SCIM", "Members, groups and org policies", "Audit log"],
  },
];

function Cta({ label }: { label: string }) {
  const { pending } = useFormStatus();
  return (
    <button className="btn gold plan-cta" type="submit" disabled={pending}>
      {pending ? "Opening checkout…" : label}
    </button>
  );
}

// PlanPicker: three selectable plan cards and one call-to-action that follows the
// selection. Starter/Pro go to Creem checkout (server action, bearer attached
// server-side); Team continues into onboarding to create the organization.
export function PlanPicker() {
  const [selected, setSelected] = useState<PlanId>("pro");
  const [state, action] = useFormState(startUpgradeAction, {});
  useEffect(() => {
    if (state.url) window.location.href = state.url;
  }, [state.url]);
  const plan = PLANS.find((p) => p.id === selected)!;

  return (
    <div className="plan-picker">
      <div className="plan-grid" role="radiogroup" aria-label="Plan">
        {PLANS.map((p) => (
          <button
            key={p.id}
            type="button"
            role="radio"
            aria-checked={selected === p.id}
            className={`plan-card${selected === p.id ? " plan-card--active" : ""}`}
            onClick={() => setSelected(p.id)}
          >
            {p.badge && <span className="plan-badge">{p.badge}</span>}
            <span className="plan-radio" aria-hidden="true" />
            <span className="plan-name">{p.name}</span>
            <span className="plan-price">
              {p.price}
              <small>{p.per}</small>
            </span>
            <span className="plan-tagline">{p.tagline}</span>
            <ul className="plan-features">
              {p.features.map((f) => (
                <li key={f}>{f}</li>
              ))}
            </ul>
          </button>
        ))}
      </div>

      {selected === "team" ? (
        <Link className="btn gold plan-cta" href="/onboarding">
          Set up a team
        </Link>
      ) : (
        <form action={action}>
          <input type="hidden" name="endpoint" value="/me/billing/checkout" />
          <input type="hidden" name="plan" value={selected} />
          <Cta label={`Continue with ${plan.name} · ${plan.price} / month`} />
        </form>
      )}
      {state.error && <p className="plan-error">{state.error}</p>}
      <p className="plan-fine">VAT included · cancel any time · secure checkout by Creem</p>
    </div>
  );
}
