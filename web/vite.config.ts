import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

const backendUrl =
	(globalThis as { process?: { env?: Record<string, string | undefined> } }).process?.env?.BACKEND_INTERNAL_URL ||
	'http://127.0.0.1:8080';

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()],
	server: {
		proxy: {
			'/api': {
				target: backendUrl,
				changeOrigin: true
			},
			'/ws': {
				target: backendUrl,
				ws: true,
				changeOrigin: true
			}
		}
	}
});
