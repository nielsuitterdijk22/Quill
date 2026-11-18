import { VerifyEmail } from "../components/auth/VerifyEmail";

// Landing page of the verification email: confirms the address, then sends the
// person on to sign in (which starts the normal Quill login).
export default function VerifyEmailPage({ searchParams }: { searchParams: { userID?: string; code?: string } }) {
  return <VerifyEmail userID={searchParams.userID ?? ""} code={searchParams.code ?? ""} />;
}
