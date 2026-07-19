import type { NextConfig } from "next";

// The care API is proxied same-origin via app/api/care/[...path]/route.ts
// (CARE_API_URL env, default http://localhost:8088).
const nextConfig: NextConfig = {};

export default nextConfig;
