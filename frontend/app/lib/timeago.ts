// timeAgo renders an ISO timestamp as a short relative age ("just now",
// "3h ago", "2d ago"), capped at "on <date>" beyond a week — the GitHub
// profile-list style freshness signal.
export function timeAgo(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const s = Math.floor((Date.now() - d.getTime()) / 1000);
  if (s < 45) return "just now";
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))}m ago`;
  if (s < 86400) return `${Math.round(s / 3600)}h ago`;
  if (s < 7 * 86400) return `${Math.round(s / 86400)}d ago`;
  return `on ${d.toLocaleDateString(undefined, { month: "short", year: "numeric" })}`;
}
