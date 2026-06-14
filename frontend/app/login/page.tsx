import { redirect } from "next/navigation";

import { getSession } from "../lib/session";
import { LoginForm } from "./LoginForm";

// LoginPage sits outside the (app) group so it renders without the authenticated
// shell. Already-authenticated visitors are bounced to the dashboard.
export default async function LoginPage() {
  const user = await getSession();
  if (user) redirect("/");
  return <LoginForm />;
}
