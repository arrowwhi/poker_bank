import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// base: './' — чтобы сборку можно было положить под любым путём (например /app/ за Go-сервером).
export default defineConfig({
  base: './',
  plugins: [react()],
  server: { host: true, port: 5173 },
});
