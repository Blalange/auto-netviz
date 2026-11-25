import { apiGet } from '$lib/api/client';
import type { Contact, Owner, OwnerGroup } from '$lib/api/types';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ fetch, url }) => {
	const at = url.searchParams.get('at');
	const [ownersRes, contactsRes] = await Promise.all([
		apiGet<{ owners: Owner[]; owner_groups: OwnerGroup[] }>('/api/v1/ownership/owners', { fetch, at }),
		apiGet<{ contacts: Contact[] }>('/api/v1/ownership/contacts', { fetch, at })
	]);
	return {
		at,
		owners: ownersRes.owners ?? [],
		groups: ownersRes.owner_groups ?? [],
		contacts: contactsRes.contacts ?? []
	};
};
