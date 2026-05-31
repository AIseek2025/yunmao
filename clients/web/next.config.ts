import type { NextConfig } from "next";

const cfg: NextConfig = {
  reactStrictMode: true,
  output: "standalone",
  env: {
    NEXT_PUBLIC_API_BASE: process.env.NEXT_PUBLIC_API_BASE ?? "http://localhost:18000",
    NEXT_PUBLIC_WS_BASE: process.env.NEXT_PUBLIC_WS_BASE ?? "ws://localhost:18007",
    NEXT_PUBLIC_MEDIA_BASE: process.env.NEXT_PUBLIC_MEDIA_BASE ?? "http://localhost:18004",
  },
};

export default cfg;
