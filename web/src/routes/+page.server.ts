import { apiGet } from '$lib/api/client';
import type { CollectorInfo, Site, SnapshotCounts, SnapshotSummary, TimelineEvent } from '$lib/api/types';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ fetch, url }) => {
	const at = url.searchParams.get('at');
	const data = await apiGet<{
		snapshot: SnapshotSummary | null;
		counts: SnapshotCounts;
		status_breakdown: Record<string, number>;
		role_breakdown: Record<string, number>;
		issue_breakdown: Record<string, number>;
		sites: Site[];
		collectors: CollectorInfo[];
		recent_events: TimelineEvent[];
	}>('/api/v1/overview', { fetch, at });
	return { at, overview: data };
};
