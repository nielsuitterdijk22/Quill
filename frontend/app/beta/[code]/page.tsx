"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";

import { useQuillAuth } from "@/components/auth/context";
import { AppTile } from "@/components/icons/AppMarks";

// Beta-invite page. The code in the URL is the bearer secret: a signed-in user
// who opens the link has their personal account moved to the free beta plan.
// It sits outside the app shell (like /invite/[token]) so the "no projects ->
// onboarding" redirect can't hijack it; signed-out visitors are sent to sign-in
// and return here afterwards.
export default function RedeemBetaPage() {
  const params = useParams();
  const code = String(params.code ?? "");
  const { getToken } = useQuillAuth();
  const [status, setStatus] = useState<"working" | "error">("working");
  const [message, setMessage] = useState("Activating your beta access…");

  useEffect(() => {
    let cancelled = false;
    getToken().then(async (authToken) => {
      if (!authToken) {
        const back = encodeURIComponent(`/beta/${code}`);
        window.location.href = `/sign-in?redirect_url=${back}`;
        return;
      }
      try {
        const res = await fetch("/api/backend/me/billing/beta", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            Authorization: `Bearer ${authToken}`,
          },
          body: JSON.stringify({ code }),
        });
        if (cancelled) return;
        if (!res.ok) {
          const body = (await res.json().catch(() => null)) as { message?: string } | null;
          setStatus("error");
          setMessage(body?.message ?? "This beta invitation is no longer valid.");
          return;
        }
        window.location.href = "/settings?beta=1";
      } catch {
        if (!cancelled) {
          setStatus("error");
          setMessage("Couldn't reach Quill. Please try again.");
        }
      }
    });
    return () => {
      cancelled = true;
    };
  }, [getToken, code]);

  return (
    <div className="ob-shell">
      <header className="ob-header">
        <div className="ob-brand">
          <AppTile app="quill" size={24} />
          Quill
        </div>
      </header>
      <main className="ob-main ob-main--narrow ob-main--centered">
        <div className="ob-headline">
          <h1 className="ob-title">{status === "error" ? "Beta invitation problem" : "Welcome to the beta"}</h1>
          <p className="ob-sub">{message}</p>
          {status === "error" && (
            <button className="ob-btn-primary" onClick={() => (window.location.href = "/")}>
              Go to Quill →
            </button>
          )}
        </div>
      </main>
    </div>
  );
}
