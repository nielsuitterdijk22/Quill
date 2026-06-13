import "./globals.css";
import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Quill",
  description: "Quill — version control for platform teams, built on Forgejo",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
