import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()],
	build: {
		emptyOutDir: true
	},
	server: {
		proxy: {
			// Forward all /api/* requests to the Go backend in development
			'/api': {
				target: 'http://localhost:8000',
				changeOrigin: true
			}
		}
	}
});
