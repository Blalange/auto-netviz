import { type ClassValue, clsx } from 'clsx';
import { twMerge } from 'tailwind-merge';

export function cn(...inputs: ClassValue[]) {
	return twMerge(clsx(inputs));
}

export function formatTime(iso?: string | null): string {
	if (!iso) return '—';
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return iso;
	return d.toLocaleString(undefined, {
		year: 'numeric',
		month: 'short',
		day: '2-digit',
		hour: '2-digit',
		minute: '2-digit',
		second: '2-digit'
	});
}

export function formatRelative(iso?: string | null): string {
	if (!iso) return 'never';
	const d = new Date(iso);
	const diffSec = Math.floor((Date.now() - d.getTime()) / 1000);
	if (diffSec < 10) return 'just now';
	if (diffSec < 60) return `${diffSec}s ago`;
	const diffMin = Math.floor(diffSec / 60);
	if (diffMin < 60) return `${diffMin}m ago`;
	const diffHr = Math.floor(diffMin / 60);
	if (diffHr < 24) return `${diffHr}h ago`;
	const diffDay = Math.floor(diffHr / 24);
	return `${diffDay}d ago`;
}

export function statusColorClass(status?: string): string {
	switch ((status || '').toLowerCase()) {
		case 'active':
		case 'online':
		case 'connected':
		case 'ok':
			return 'bg-emerald-500';
		case 'offline':
		case 'failed':
		case 'error':
			return 'bg-rose-500';
		case 'degraded':
		case 'planned':
		case 'staged':
		case 'in-stock':
			return 'bg-amber-500';
		default:
			return 'bg-slate-400';
	}
}

export function sourceBadgeClass(src: string): string {
	switch (src) {
		case 'manual':
			return 'bg-slate-900 text-slate-50 dark:bg-slate-100 dark:text-slate-900 border-transparent';
		case 'unifi':
			return 'bg-sky-500/15 text-sky-700 dark:text-sky-300 border-sky-500/30';
		case 'snmp':
			return 'bg-cyan-500/15 text-cyan-700 dark:text-cyan-300 border-cyan-500/30';
		case 'lldp':
			return 'bg-indigo-500/15 text-indigo-700 dark:text-indigo-300 border-indigo-500/30';
		case 'redfish':
			return 'bg-rose-500/15 text-rose-700 dark:text-rose-300 border-rose-500/30';
		case 'ipmi':
			return 'bg-orange-500/15 text-orange-700 dark:text-orange-300 border-orange-500/30';
		case 'hostos':
			return 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-300 border-emerald-500/30';
		case 'kubernetes':
			return 'bg-blue-500/15 text-blue-700 dark:text-blue-300 border-blue-500/30';
		case 'discovery':
			return 'bg-purple-500/15 text-purple-700 dark:text-purple-300 border-purple-500/30';
		default:
			return 'bg-muted text-muted-foreground border-border';
	}
}

export function withAtParam(href: string, at: string | null | undefined): string {
	if (!at) return href;
	const sep = href.includes('?') ? '&' : '?';
	return `${href}${sep}at=${encodeURIComponent(at)}`;
}
