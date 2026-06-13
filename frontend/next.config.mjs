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
};

export default nextConfig;
