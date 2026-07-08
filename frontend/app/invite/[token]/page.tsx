"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";

import { useQuillAuth } from "@/components/auth/context";
import { AppTile } from "@/components/icons/AppMarks";

// Invite-accept page. The token in the URL is the bearer secret: a signed-in user
// who opens the link is added to the organization. It sits outside the app shell
// so the "no projects -> onboarding" redirect can't hijack an invitee, and it
// uses the shared auth context to attach the caller's token. On success it sends
// the user to the org's settings; if they aren't signed in yet, it points them at
// sign-in and returns here afterward.
export default function AcceptInvitePage() {
  const params = useParams();
  const token = String(params.token ?? "");
  const { getToken } = useQuillAuth();
  const [status, setStatus] = useState<"working" | "error">("working");
  const [message, setMessage] = useState("Joining the organization…");

  useEffect(() => {
    let cancelled = false;
    getToken().then(async (authToken) => {
      if (!authToken) {
        const back = encodeURIComponent(`/invite/${token}`);
        window.location.href = `/sign-in?redirect_url=${back}`;
        return;
      }
      try {
        const res = await fetch(`/api/backend/invites/${token}/accept`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            Authorization: `Bearer ${authToken}`,
          },
          body: "{}",
        });
        if (cancelled) return;
        if (!res.ok) {
          const body = (await res.json().catch(() => null)) as { message?: string } | null;
          setStatus("error");
          setMessage(body?.message ?? "This invitation is no longer valid.");
          return;
        }
        const body = (await res.json()) as { slug?: string };
        window.location.href = body.slug ? `/orgs/${body.slug}/settings` : "/";
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
  }, [getToken, token]);

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
          <h1 className="ob-title">
            {status === "error" ? "Invitation problem" : "Accepting invitation"}
          </h1>
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
