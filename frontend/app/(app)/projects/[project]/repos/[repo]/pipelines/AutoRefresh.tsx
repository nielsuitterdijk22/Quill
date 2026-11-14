"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

// AutoRefresh re-renders the server page every few seconds while `active`
// (some run is queued or running), so list statuses move on their own.
// Paused while the tab is hidden.
export function AutoRefresh({ active, everyMs = 4000 }: { active: boolean; everyMs?: number }) {
  const router = useRouter();
  useEffect(() => {
    if (!active) return;
    const t = setInterval(() => {
      if (document.visibilityState === "visible") router.refresh();
    }, everyMs);
    return () => clearInterval(t);
  }, [active, everyMs, router]);
  return null;
}
