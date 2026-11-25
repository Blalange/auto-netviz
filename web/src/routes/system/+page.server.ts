import { apiGet } from '$lib/api/client';
import type { CollectorInfo, Issue } from '$lib/api/types';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ fetch, url }) => {
	const at = url.searchParams.get('at');
	const [colRes, issRes] = await Promise.all([
		apiGet<{ collectors: CollectorInfo[] }>('/api/v1/system/collectors', { fetch }),
		apiGet<{ issues: Issue[] }>('/api/v1/system/issues', { fetch, at })
	]);
	return { at, collectors: colRes.collectors ?? [], issues: issRes.issues ?? [] };
};
