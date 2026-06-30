import { SignUp } from "@clerk/nextjs";

import { ZitadelSignInButton } from "../../components/auth/ZitadelSignInButton";
import { isZitadel } from "../../lib/auth-provider";

export default function SignUpPage() {
  // Zitadel registration is handled by the IdP (and SSO/SCIM in orgs), so sign-up
  // routes to the same hosted login.
  if (isZitadel) {
    return <ZitadelSignInButton label="Continue" />;
  }
  return (
    <div className="auth-page">
      <SignUp />
    </div>
  );
}
