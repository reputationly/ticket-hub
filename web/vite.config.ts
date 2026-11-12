import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: { "/api": { target: "http://127.0.0.1:8080", changeOrigin: false } },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    rollupOptions: {
      output: {
        onlyExplicitManualChunks: true,
        manualChunks(id) {
          if (!id.includes("node_modules/")) return;
          const path = id.slice(id.lastIndexOf("node_modules/") + 13);
          if (path.startsWith("antd/es/")) {
            const component = path.split("/")[2].replace(/\..*$/, "");
            if (
              path.includes("/locale/") ||
              [
                "_util",
                "theme",
                "config-provider",
                "form",
                "grid",
                "tooltip",
                "empty",
                "button",
                "space",
                "app",
                "message",
                "modal",
                "notification",
                "skeleton",
                "locale",
              ].includes(component)
            )
              return "antd-core";
            return `antd-${component}`;
          }
          const parts = path.split("/");
          return `vendor-${parts[0].startsWith("@") ? parts.slice(0, 2).join("-") : parts[0]}`;
        },
      },
    },
  },
});
