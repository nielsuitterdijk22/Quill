import { redirect } from "next/navigation";

// Login is now handled by the IdP. Redirect existing bookmarks to /sign-in.
export default function LoginPage() {
  redirect("/sign-in");
}
