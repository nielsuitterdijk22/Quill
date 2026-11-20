import { redirect } from "next/navigation";

import { ZitadelSignInButton } from "../../components/auth/ZitadelSignInButton";
import { getSession } from "../../lib/session";

// SignInPage is the single re-auth surface. It starts the login straight away
// (one screen fewer); ?stale=1 arrives from the app layout when a session
// exists but its Zitadel access token no longer verifies — the copy says why
// — and ?error=… (a failed attempt) shows the card instead of retrying.
export default async function SignInPage({
  searchParams,
}: {
  searchParams: { stale?: string; error?: string; redirect_url?: string; prompt?: string };
}) {
  // Don't strand an already-signed-in user on the sign-in screen: if a valid
  // session resolves, send them into the app instead of showing the button.
  // redirect_url (set by /beta/[code] and /invite/[token]) is where to land after
  // login; only same-site paths are honoured so it can't become an open redirect.
  const raw = searchParams?.redirect_url ?? "";
  const returnTo = raw.startsWith("/") && !raw.startsWith("//") && !raw.startsWith("/\\") ? raw : "/";
  if (await getSession()) redirect(returnTo);
  // An Auth.js error (?error=…) means the last attempt failed: show the card
  // with the reason and a button instead of auto-starting again (no loops).
  const failed = Boolean(searchParams?.error);
  const notice = failed
    ? "Sign-in didn't complete. Try again — if it keeps happening, contact your administrator."
    : searchParams?.stale === "1"
      ? "Your session expired — signing you in again…"
      : undefined;
  return (
    <ZitadelSignInButton
      label="Sign in"
      auto={!failed}
      notice={notice}
      callbackUrl={returnTo}
      chooseAccount={searchParams?.prompt === "select_account"}
    />
  );
}
