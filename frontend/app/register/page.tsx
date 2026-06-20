import { redirect } from "next/navigation";

// Registration is now handled by Clerk. Redirect existing bookmarks to /sign-up.
export default function RegisterPage() {
  redirect("/sign-up");
}
