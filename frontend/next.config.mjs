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
      "projects|settings|admin|repositories|pulls|pipelines|sbom|sign-in|sign-up|login|register|api";
    return [
      // /api/backend/* is served by the app/api/backend/[...path] route
      // handler (auth-aware proxy). A rewrite here SHADOWS it — Next serves
      // the rewrite instead of the filesystem route, silently bypassing
      // the session injection. Do not reintroduce.
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
    // The live pipeline feed is a WebSocket straight to the api host (the
    // app's proxy route cannot carry an upgrade).
    const wsApi = process.env.NEXT_PUBLIC_API_WS_URL || "ws://localhost:8080";
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
      `connect-src 'self'${zitadelSrc} ${wsApi}`,
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
