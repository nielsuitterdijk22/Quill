import { requireSession } from "../lib/session";

export default async function OnboardingLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  await requireSession();
  return (
    <div className="onboarding-shell">
      <div className="brand" style={{ padding: "2rem" }}>
        <span className="dot" /> Quill
      </div>
      {children}
    </div>
  );
}
