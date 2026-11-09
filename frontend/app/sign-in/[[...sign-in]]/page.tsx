import { redirect } from "next/navigation";

import { ZitadelSignInButton } from "../../components/auth/ZitadelSignInButton";
import { getSession } from "../../lib/session";

// SignInPage is the single re-auth surface. ?stale=1 arrives from the app
// layout when a session exists but its Zitadel access token no longer
// verifies — the copy makes clear why the user landed here mid-session
// instead of silently offering a button.
export default async function SignInPage({
  searchParams,
}: {
  searchParams: { stale?: string };
}) {
  // Don't strand an already-signed-in user on the sign-in screen: if a valid
  // session resolves, send them into the app instead of showing the button.
  if (await getSession()) redirect("/");
  return (
    <>
      {searchParams?.stale === "1" && (
        <p className="subtle" style={{ textAlign: "center", maxWidth: 420, margin: "0 auto 12px" }}>
          Your session expired — sign in again to continue where you left off.
        </p>
      )}
      <ZitadelSignInButton label="Sign in" />
    </>
  );
}
