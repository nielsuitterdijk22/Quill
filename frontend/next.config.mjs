// Derive the Zitadel origin from the configured issuer so the CSP can allow the
// browser's OIDC discovery fetch (used on RP-initiated logout) without
// hardcoding the instance host.
function zitadelOrigin() {
  const issuer = process.env.NEXT_PUBLIC_ZITADEL_ISSUER ?? "";
  if (!issuer) return null;
  try {
    return new URL(issuer).origin;
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
    const zitadel = zitadelOrigin();
    const zitadelSrc = zitadel ? ` ${zitadel}` : "";
    const csp = [
      "default-src 'self'",
      // Next.js injects inline scripts for hydration; unsafe-inline is required
      // until nonce-based CSP is wired through the App Router.
      `script-src 'self' 'unsafe-inline' 'unsafe-eval'`,
      "style-src 'self' 'unsafe-inline'",
      `img-src 'self' data: blob:`,
      "font-src 'self' data:",
      `worker-src 'self' blob:`,
      // Zitadel origin is allowed for the OIDC discovery fetch on logout.
      `connect-src 'self'${zitadelSrc}`,
      "frame-ancestors 'none'",
      "base-uri 'self'",
      // Sign-in / logout redirect the browser to Zitadel's authorize / end-session
      // endpoints, which are cross-origin form/navigation targets.
      `form-action 'self'${zitadelSrc}`,
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
