import { apiGet } from '$lib/api/client';
import type { SnapshotDiff, SnapshotSummary, TimelineEvent } from '$lib/api/types';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ fetch, url }) => {
	const [snapsRes, eventsRes] = await Promise.all([
		apiGet<{ snapshots: SnapshotSummary[] }>('/api/v1/timemachine/snapshots?limit=200', { fetch }),
		apiGet<{ events: TimelineEvent[] }>('/api/v1/timemachine/events?limit=200', { fetch })
	]);

	const snaps = snapsRes.snapshots ?? [];
	let fromParam = url.searchParams.get('from') || '';
	let toParam = url.searchParams.get('to') || '';
	if (!toParam && snaps.length > 0) toParam = String(snaps[0].id);
	if (!fromParam && snaps.length > 1) fromParam = String(snaps[1].id);
	else if (!fromParam && snaps.length === 1) fromParam = String(snaps[0].id);

	let diff: SnapshotDiff | null = null;
	if (fromParam && toParam) {
		try {
			diff = await apiGet<SnapshotDiff>(
				`/api/v1/timemachine/diff?from=${encodeURIComponent(fromParam)}&to=${encodeURIComponent(toParam)}`,
				{ fetch }
			);
		} catch {
			diff = null;
		}
	}

	return {
		snapshots: snaps,
		events: eventsRes.events ?? [],
		fromParam,
		toParam,
		diff
	};
};
