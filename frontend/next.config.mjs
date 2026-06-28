// Derive the Clerk Frontend API hostname from the publishable key so the CSP
// is correct for any Clerk instance without hardcoding the domain.
function clerkHost() {
  const key = process.env.NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY ?? "";
  const b64 = key.replace(/^pk_(test|live)_/, "");
  if (!b64) return null;
  try {
    return Buffer.from(b64, "base64").toString("utf8").replace(/\$$/, "");
  } catch {
    return null;
  }
}

/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  // Emit a self-contained server bundle for small production images.
  output: "standalone",
  async rewrites() {
    // Proxy browser calls to the Go backend so the frontend never needs CORS or
    // a public API URL baked in at build time. Server components can also call
    // the backend directly via QUILL_API_BASE_URL.
    const api = process.env.QUILL_API_BASE_URL || "http://localhost:8080";
    // Top-level app routes that must not be treated as owner namespaces.
    const reserved =
      "projects|settings|admin|repositories|pulls|pipelines|sign-in|sign-up|login|register|api";
    return [
      {
        source: "/api/backend/:path*",
        destination: `${api}/api/v1/:path*`,
      },
      // Short namespace URL: /{owner}/{repo}/sub-path → /projects/{owner}/repos/{repo}/sub-path
      // Lets /{owner}/{repo}/... resolve via the existing /projects/[project]/repos/[repo]/...
      // pages without duplicating every page file.
      {
        source: `/:owner((?!${reserved})[^/]+)/:repo/:path+`,
        destination: "/projects/:owner/repos/:repo/:path*",
      },
      // Short namespace URL root: /{owner}/{repo} (no trailing path)
      {
        source: `/:owner((?!${reserved})[^/]+)/:repo`,
        destination: "/projects/:owner/repos/:repo",
      },
    ];
  },
  async headers() {
    const clerk = clerkHost();
    const clerkSrc = clerk ? ` https://${clerk}` : "";
    const csp = [
      "default-src 'self'",
      // Next.js injects inline scripts for hydration; unsafe-inline is required
      // until nonce-based CSP is wired through the App Router.
      `script-src 'self' 'unsafe-inline' 'unsafe-eval'${clerkSrc} https://challenges.cloudflare.com`,
      "style-src 'self' 'unsafe-inline'",
      `img-src 'self' data: blob: https://img.clerk.com${clerkSrc}`,
      "font-src 'self' data:",
      `worker-src 'self' blob:`,
      `connect-src 'self'${clerkSrc} https://clerk-telemetry.com https://challenges.cloudflare.com`,
      `frame-src${clerkSrc} https://challenges.cloudflare.com`,
      "frame-ancestors 'none'",
      "base-uri 'self'",
      "form-action 'self'",
    ].join("; ");
    return [
      {
        source: "/(.*)",
        headers: [
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "X-Frame-Options", value: "DENY" },
          { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
          { key: "Content-Security-Policy", value: csp },
        ],
      },
    ];
  },
};

export default nextConfig;
