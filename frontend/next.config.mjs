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
    return [
      {
        source: "/api/backend/:path*",
        destination: `${api}/api/v1/:path*`,
      },
    ];
  },
  async headers() {
    const csp = [
      "default-src 'self'",
      // Next.js injects inline scripts for hydration; unsafe-inline is required
      // until nonce-based CSP is wired through the App Router.
      "script-src 'self' 'unsafe-inline' 'unsafe-eval'",
      "style-src 'self' 'unsafe-inline'",
      "img-src 'self' data: blob:",
      "font-src 'self' data:",
      "connect-src 'self'",
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
