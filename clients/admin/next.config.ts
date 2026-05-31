import type { NextConfig } from "next";

const cfg: NextConfig = {
  reactStrictMode: true,
  output: "standalone",
  basePath: process.env.YUNMAO_ADMIN_BASE_PATH ?? "",
  env: {
    NEXT_PUBLIC_ADMIN_API_BASE:
      process.env.NEXT_PUBLIC_ADMIN_API_BASE ?? "http://localhost:18006",
    NEXT_PUBLIC_AUTH_API_BASE:
      process.env.NEXT_PUBLIC_AUTH_API_BASE ?? "http://localhost:18000",
  },
};

export default cfg;
