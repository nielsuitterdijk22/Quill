import { requireSession } from "../lib/session";

export default async function OnboardingLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  await requireSession();
  return <div className="onboarding-shell">{children}</div>;
}
