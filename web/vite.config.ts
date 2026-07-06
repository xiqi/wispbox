import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { resolve } from "node:path";

// wispbox frontend: three apps, one build.
//   /            index.html   webmail
//   /admin       admin.html   admin console
//   /setup       setup.html   first-run wizard
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    rollupOptions: {
      input: {
        mail: resolve(__dirname, "index.html"),
        admin: resolve(__dirname, "admin.html"),
        setup: resolve(__dirname, "setup.html"),
      },
    },
  },
  server: {
    // During `npm run dev`, proxy API calls to a locally running
    // `wispboxd serve --dev` (which also serves the built frontend, but the
    // Vite dev server gives HMR).
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
});
