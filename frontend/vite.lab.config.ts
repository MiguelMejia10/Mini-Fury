import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
// Isolated lab API and a distinct session cookie; never proxies to the main database.
export default defineConfig({plugins:[react()],server:{host:'127.0.0.1',port:5174,strictPort:true,proxy:{'/api':'http://127.0.0.1:18080'}}});
