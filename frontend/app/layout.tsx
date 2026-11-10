import "./globals.css";
import type { Metadata } from "next";
import { GeistSans } from "geist/font/sans";
import { GeistMono } from "geist/font/mono";

import { AuthProvider } from "./components/auth/AuthProvider";

// Geist Sans/Mono (Creem's type family), self-hosted at build time via
// next/font — no runtime requests to any font CDN, matching the no-telemetry
// stance. Falls back to the system stack via the CSS variable defaults.
// The geist package ships the font files and serves them self-hosted via
// next/font/local — no runtime requests (no-telemetry stance kept).

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
    <html lang="en" className={`${GeistSans.variable} ${GeistMono.variable}`}>
      <body>
        <AuthProvider>{children}</AuthProvider>
      </body>
    </html>
  );
}
