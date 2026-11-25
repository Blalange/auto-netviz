<script lang="ts">
	import { apiGet } from '$lib/api/client';
	import type { Asset, Contact, Device, Owner } from '$lib/api/types';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '$lib/components/ui/table';
	import Badge from '$lib/components/ui/badge/Badge.svelte';
	import { withAtParam } from '$lib/utils';
	import { Users } from '@lucide/svelte';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
	let selectedOwner = $state<Owner | null>(null);
	let ownerDetails = $state<{ devices: Device[]; assets: Asset[]; contacts: Contact[] } | null>(null);

	async function inspectOwner(o: Owner) {
		if (selectedOwner?.id === o.id) {
			selectedOwner = null;
			ownerDetails = null;
			return;
		}
		selectedOwner = o;
		try {
			ownerDetails = await apiGet<{ devices: Device[]; assets: Asset[]; contacts: Contact[] }>(
				`/api/v1/ownership/owners/${encodeURIComponent(o.id)}`,
				{ at: data.at }
			);
		} catch {
			ownerDetails = null;
		}
	}
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-bold tracking-tight">Ownership & Contacts</h1>
		<p class="text-sm text-muted-foreground">
			Teams, owner groups, and technical contacts mapped to infrastructure & assets.
		</p>
	</div>

	<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
		{#each data.owners as owner}
			<button type="button" onclick={() => inspectOwner(owner)} class="text-left cursor-pointer">
				<Card class={`h-full transition-colors ${selectedOwner?.id === owner.id ? 'border-primary ring-1 ring-primary' : 'hover:border-primary/50'}`}>
					<CardHeader class="flex-row items-start justify-between">
						<div>
							<CardTitle class="flex items-center gap-2 text-base">
								<Users class="h-4 w-4 text-primary" />
								{owner.name}
							</CardTitle>
							{#if owner.group}
								<CardDescription>{owner.group}</CardDescription>
							{/if}
						</div>
						<Badge variant="outline" class="font-mono">{owner.device_count ?? 0} dev</Badge>
					</CardHeader>
					<CardContent class="space-y-1 text-xs text-muted-foreground">
						{#if owner.description}<p>{owner.description}</p>{/if}
						{#if owner.email}<div class="font-mono">{owner.email}</div>{/if}
					</CardContent>
				</Card>
			</button>
		{/each}
	</div>

	{#if selectedOwner && ownerDetails}
		<Card>
			<CardHeader>
				<CardTitle>Owned Infrastructure — {selectedOwner.name}</CardTitle>
				<CardDescription>
					{ownerDetails.devices.length} devices · {ownerDetails.assets.length} tracked assets · {ownerDetails.contacts.length} contacts
				</CardDescription>
			</CardHeader>
			<CardContent class="space-y-4">
				<div class="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
					{#each ownerDetails.devices as d}
						<a href={withAtParam(`/devices/${encodeURIComponent(d.id)}`, data.at)} class="rounded-lg border bg-muted/30 p-3 hover:border-primary/50">
							<div class="text-sm font-medium">{d.name}</div>
							<div class="text-xs font-mono text-muted-foreground">{d.role} · {d.primary_ip || d.site}</div>
						</a>
					{/each}
				</div>
			</CardContent>
		</Card>
	{/if}

	<Card>
		<CardHeader>
			<CardTitle>Contacts Directory ({data.contacts.length})</CardTitle>
		</CardHeader>
		<CardContent>
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead>Name</TableHead>
						<TableHead>Role</TableHead>
						<TableHead>Owner</TableHead>
						<TableHead>Email</TableHead>
						<TableHead>Phone</TableHead>
					</TableRow>
				</TableHeader>
				<TableBody>
					{#each data.contacts as c}
						<TableRow>
							<TableCell class="font-medium text-xs">{c.name}</TableCell>
							<TableCell class="font-mono text-xs">{c.role || '—'}</TableCell>
							<TableCell class="text-xs">{c.owner || '—'}</TableCell>
							<TableCell class="font-mono text-xs">{c.email || '—'}</TableCell>
							<TableCell class="font-mono text-xs">{c.phone || '—'}</TableCell>
						</TableRow>
					{/each}
				</TableBody>
			</Table>
		</CardContent>
	</Card>
</div>
