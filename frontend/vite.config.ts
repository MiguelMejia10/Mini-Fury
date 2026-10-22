import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
// El navegador usa el mismo origen. Solo Vite habla con la API local.
export default defineConfig({ plugins: [react()], server: { proxy: { '/api': 'http://127.0.0.1:8080' } } });
