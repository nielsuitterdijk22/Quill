import { redirect } from "next/navigation";

import { ZitadelSignInButton } from "../../components/auth/ZitadelSignInButton";
import { getSession } from "../../lib/session";

export default async function SignInPage() {
  // Don't strand an already-signed-in user on the sign-in screen: if a valid
  // session resolves, send them into the app instead of showing the button.
  if (await getSession()) redirect("/");
  return <ZitadelSignInButton label="Sign in" />;
}
