import { browser } from '$app/environment';

export function getApiOrigin(): string {
	if (browser) return '';
	// SSR calls talk directly to the Go backend container/process
	const proc = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process;
	return proc?.env?.BACKEND_INTERNAL_URL || 'http://127.0.0.1:8080';
}

export async function apiGet<T>(
	path: string,
	opts: { fetch?: typeof fetch; at?: string | null } = {}
): Promise<T> {
	const fetcher = opts.fetch ?? fetch;
	const base = getApiOrigin();
	let fullPath = path;
	if (opts.at && !fullPath.includes('at=')) {
		const sep = fullPath.includes('?') ? '&' : '?';
		fullPath = `${fullPath}${sep}at=${encodeURIComponent(opts.at)}`;
	}
	const res = await fetcher(`${base}${fullPath}`);
	if (!res.ok) {
		const text = await res.text().catch(() => '');
		throw new Error(`API ${res.status} ${fullPath}: ${text}`);
	}
	return (await res.json()) as T;
}
