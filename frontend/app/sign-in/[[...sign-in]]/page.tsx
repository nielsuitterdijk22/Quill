import { redirect } from "next/navigation";

import { ZitadelSignInButton } from "../../components/auth/ZitadelSignInButton";
import { getSession } from "../../lib/session";

// safeRelative only accepts an in-app path ("/…") as the post-login destination —
// never a protocol-relative ("//host") or absolute URL — so ?redirect_url can't be
// abused as an open redirect.
function safeRelative(value: string | string[] | undefined): string {
  const v = Array.isArray(value) ? value[0] : value;
  if (v && v.startsWith("/") && !v.startsWith("//")) return v;
  return "/";
}

export default async function SignInPage({
  searchParams,
}: {
  searchParams: { redirect_url?: string | string[] };
}) {
  // Where to land after signing in: an invitee bounced here carries the invite
  // path in ?redirect_url so accepting completes on return; everyone else goes to
  // the app root.
  const callbackUrl = safeRelative(searchParams.redirect_url);
  // Don't strand an already-signed-in user on the sign-in screen: if a valid
  // session resolves, send them to their destination instead of showing the button.
  if (await getSession()) redirect(callbackUrl);
  return <ZitadelSignInButton label="Sign in" callbackUrl={callbackUrl} />;
}
