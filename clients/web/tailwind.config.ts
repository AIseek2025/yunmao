import type { Config } from "tailwindcss";

const config: Config = {
  content: [
    "./src/app/**/*.{ts,tsx}",
    "./src/components/**/*.{ts,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        brand: {
          DEFAULT: "#ff7849",
          50: "#fff3ec",
          500: "#ff7849",
          700: "#cc5021",
        },
      },
    },
  },
};

export default config;
