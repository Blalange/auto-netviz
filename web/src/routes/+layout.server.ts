import { apiGet } from '$lib/api/client';
import type { SnapshotCounts, SnapshotSummary } from '$lib/api/types';
import type { LayoutServerLoad } from './$types';

export const load: LayoutServerLoad = async ({ fetch, url }) => {
	const at = url.searchParams.get('at');
	try {
		const [snapsRes, ovRes] = await Promise.all([
			apiGet<{ snapshots: SnapshotSummary[]; total: number }>('/api/v1/timemachine/snapshots?limit=120', { fetch }),
			apiGet<{ snapshot: SnapshotSummary | null; counts: SnapshotCounts; issue_breakdown?: Record<string, number> }>(
				'/api/v1/overview',
				{ fetch, at }
			)
		]);
		return {
			at,
			snapshots: snapsRes.snapshots ?? [],
			activeSnapshot: ovRes.snapshot ?? snapsRes.snapshots?.[0] ?? null,
			counts: ovRes.counts,
			issueBreakdown: ovRes.issue_breakdown ?? {}
		};
	} catch {
		return {
			at,
			snapshots: [] as SnapshotSummary[],
			activeSnapshot: null,
			counts: {
				sites: 0,
				racks: 0,
				devices: 0,
				auto_devices: 0,
				cables: 0,
				vlans: 0,
				prefixes: 0,
				assets: 0,
				owners: 0,
				issues: 0
			},
			issueBreakdown: {} as Record<string, number>
		};
	}
};
