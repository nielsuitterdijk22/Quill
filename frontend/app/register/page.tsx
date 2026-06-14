import { redirect } from "next/navigation";

import { getSession } from "../lib/session";
import { RegisterForm } from "./RegisterForm";

// RegisterPage sits outside the (app) group. Already-authenticated visitors are
// redirected to the dashboard.
export default async function RegisterPage() {
  const user = await getSession();
  if (user) redirect("/");
  return <RegisterForm />;
}
