import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  // Prefer source files when the mini-program has emitted adjacent CommonJS files.
  resolve: { extensions: ['.mjs', '.ts', '.tsx', '.js', '.jsx', '.json'] },
  build: { target: 'es2022', sourcemap: true },
})

