import { SignIn } from "@clerk/nextjs";

import { ZitadelSignInButton } from "../../components/auth/ZitadelSignInButton";
import { isZitadel } from "../../lib/auth-provider";

export default function SignInPage() {
  if (isZitadel) {
    return <ZitadelSignInButton label="Sign in" />;
  }
  return (
    <div className="auth-page">
      <SignIn />
    </div>
  );
}
