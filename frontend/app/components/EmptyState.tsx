import Link from "next/link";

import { Icon } from "./Icon";

// EmptyState is the app-wide empty pattern (Creem-style): a small accent
// marker, a one-line title, an optional supporting line, and an optional
// primary CTA. Every list that can be empty should end up here — a dead-end
// "No X yet" line is the biggest perceived-fragility signal in the review.
export function EmptyState({
  title,
  body,
  cta,
  icon = "◆",
}: {
  title: string;
  body?: string;
  cta?: { href: string; label: string };
  icon?: string;
}) {
  return (
    <div className="empty-state">
      <span className="empty-state-icon"><Icon name={icon} size={14} /></span>
      <div className="empty-state-title">{title}</div>
      {body && <div className="empty-state-body">{body}</div>}
      {cta && (
        <Link className="btn primary empty-state-cta" href={cta.href}>
          {cta.label}
        </Link>
      )}
    </div>
  );
}
