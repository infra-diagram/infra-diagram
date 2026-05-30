import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";

// El backend Go corre en :8080. En dev, Vite proxea las rutas de la API
// para que el cliente use rutas relativas y no haya CORS.
// En prod, el build se sirve embebido por el mismo binario (go:embed) -> mismo origen.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    proxy: {
      "/health": "http://localhost:8080",
      "/insights": "http://localhost:8080",
    },
  },
  // Build hacia ../cmd/api/web_dist para embeber con go:embed (ver README).
  build: {
    outDir: "dist",
    emptyOutDir: true,
    // El visor de grafo (cytoscape + fcose + dagre) es un chunk lazy intencional.
    chunkSizeWarningLimit: 700,
  },
});
