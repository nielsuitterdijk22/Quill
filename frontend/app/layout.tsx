import "./globals.css";
import type { Metadata } from "next";
import { ClerkProvider } from "@clerk/nextjs";
import { dark } from "@clerk/themes";

export const dynamic = "force-dynamic";

export const metadata: Metadata = {
  title: { default: "Quill", template: "%s · Quill" },
  description: "Quill — self-hosted version control for teams, built on Forgejo. Open source, no telemetry, GDPR-friendly.",
  icons: { icon: "/favicon.svg", shortcut: "/favicon.svg" },
  openGraph: {
    title: "Quill",
    description: "Self-hosted version control — open source, no vendor lock-in.",
    type: "website",
  },
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <ClerkProvider appearance={{ baseTheme: dark }} afterSignOutUrl="/sign-in">
      <html lang="en">
        <body>{children}</body>
      </html>
    </ClerkProvider>
  );
}
