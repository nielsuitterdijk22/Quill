import { redirect } from "next/navigation";

import { SignUpForm } from "../../components/auth/SignUpForm";
import { getSession } from "../../lib/session";

// Quill's own sign-up form (handle + email + password). Signing in stays on
// Zitadel's hosted pages; the account itself is created through the backend.
export default async function SignUpPage() {
  if (await getSession()) redirect("/");
  return <SignUpForm />;
}
