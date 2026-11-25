<script lang="ts">
	import '../app.css';
	import type { Snippet } from 'svelte';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { ModeWatcher, toggleMode } from 'mode-watcher';
	import { liveStream } from '$lib/state/live.svelte';
	import { withAtParam } from '$lib/utils';
	import TimeMachineBar from '$lib/components/timemachine/TimeMachineBar.svelte';
	import CommandSearch from '$lib/components/shared/CommandSearch.svelte';
	import Button from '$lib/components/ui/button/Button.svelte';
	import type { LayoutData } from './$types';
	import {
		LayoutDashboard,
		Share2,
		Server,
		HardDrive,
		Cable,
		Network,
		Package,
		Users,
		History,
		Activity,
		Search,
		Sun,
		Moon,
		Lock,
		Menu,
		X
	} from '@lucide/svelte';

	let { data, children }: { data: LayoutData; children: Snippet } = $props();
	let searchOpen = $state(false);
	let mobileNav = $state(false);

	const navItems = [
		{ href: '/', label: 'Dashboard', icon: LayoutDashboard },
		{ href: '/topology', label: 'Topology', icon: Share2 },
		{ href: '/racks', label: 'Racks', icon: HardDrive },
		{ href: '/cabling', label: 'Cabling', icon: Cable },
		{ href: '/devices', label: 'Devices', icon: Server },
		{ href: '/networks', label: 'Networks', icon: Network },
		{ href: '/assets', label: 'Assets', icon: Package },
		{ href: '/ownership', label: 'Ownership', icon: Users },
		{ href: '/timemachine', label: 'Time Machine', icon: History },
		{ href: '/system', label: 'System & Collectors', icon: Activity }
	];

	onMount(() => {
		liveStream.start();
	});

	$effect(() => {
		liveStream.setHistoricalMode(Boolean(data.at && data.at !== 'live'));
	});

	function isActive(href: string): boolean {
		const p = page.url.pathname;
		if (href === '/') return p === '/';
		return p === href || p.startsWith(href + '/');
	}
</script>

<ModeWatcher />
<CommandSearch bind:open={searchOpen} at={data.at} />

<div class="flex min-h-screen bg-background text-foreground">
	<!-- Sidebar -->
	<aside
		class={`fixed inset-y-0 left-0 z-40 w-60 border-r bg-card/95 backdrop-blur-md transition-transform lg:static lg:translate-x-0 ${
			mobileNav ? 'translate-x-0' : '-translate-x-full'
		}`}
	>
		<div class="flex h-full flex-col">
			<div class="flex h-14 items-center justify-between border-b px-4">
				<a href={withAtParam('/', data.at)} class="flex items-center gap-2.5 font-semibold tracking-tight">
					<span class="flex h-7 w-7 items-center justify-center rounded-lg bg-primary text-primary-foreground font-mono text-xs font-bold">
						AN
					</span>
					<span class="text-sm">Auto Netviz</span>
				</a>
				<button type="button" class="lg:hidden p-1 text-muted-foreground" onclick={() => (mobileNav = false)}>
					<X class="h-4 w-4" />
				</button>
			</div>

			<div class="p-3">
				<button
					type="button"
					onclick={() => (searchOpen = true)}
					class="flex w-full items-center justify-between rounded-lg border bg-muted/40 px-3 py-1.5 text-xs text-muted-foreground hover:bg-muted transition-colors cursor-pointer"
				>
					<span class="flex items-center gap-2">
						<Search class="h-3.5 w-3.5" />
						Search...
					</span>
					<kbd class="rounded border bg-background px-1.5 py-0.5 font-mono text-[10px]">⌘K</kbd>
				</button>
			</div>

			<nav class="flex-1 space-y-0.5 px-2.5 overflow-y-auto">
				{#each navItems as item}
					{@const Icon = item.icon}
					<a
						href={withAtParam(item.href, data.at)}
						onclick={() => (mobileNav = false)}
						class={`flex items-center justify-between rounded-lg px-2.5 py-2 text-xs font-medium transition-colors ${
							isActive(item.href)
								? 'bg-primary/12 text-primary'
								: 'text-muted-foreground hover:bg-muted hover:text-foreground'
						}`}
					>
						<span class="flex items-center gap-2.5">
							<Icon class="h-4 w-4" />
							{item.label}
						</span>
						{#if item.href === '/devices' && data.counts.devices > 0}
							<span class="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
								{data.counts.devices}
							</span>
						{:else if item.href === '/system' && data.counts.issues > 0}
							<span class="rounded bg-amber-500/20 px-1.5 py-0.5 font-mono text-[10px] text-amber-600 dark:text-amber-400">
								{data.counts.issues}
							</span>
						{/if}
					</a>
				{/each}
			</nav>

			<div class="border-t p-3 space-y-2">
				<div class="flex items-center justify-between rounded-lg border bg-muted/30 px-2.5 py-2 text-[11px] text-muted-foreground">
					<span class="flex items-center gap-1.5">
						<Lock class="h-3.5 w-3.5 text-emerald-500" />
						Read-only YAML GitOps
					</span>
					<Button variant="ghost" size="icon" class="h-6 w-6" onclick={toggleMode} title="Toggle dark/light theme">
						<Sun class="h-3.5 w-3.5 hidden dark:block" />
						<Moon class="h-3.5 w-3.5 block dark:hidden" />
					</Button>
				</div>
				<div class="flex items-center justify-between px-1 text-[10px] text-muted-foreground font-mono">
					<span>Stream: {liveStream.connected ? 'connected' : 'polling'}</span>
					<span>{data.counts.auto_devices} auto</span>
				</div>
			</div>
		</div>
	</aside>

	<!-- Main column -->
	<div class="flex flex-1 flex-col min-w-0">
		<div class="flex items-center justify-between border-b bg-card px-4 py-2 lg:hidden">
			<button type="button" onclick={() => (mobileNav = true)} class="p-1.5 rounded border">
				<Menu class="h-4 w-4" />
			</button>
			<span class="text-sm font-semibold">Auto Netviz</span>
			<button type="button" onclick={() => (searchOpen = true)} class="p-1.5 rounded border">
				<Search class="h-4 w-4" />
			</button>
		</div>

		<TimeMachineBar snapshots={data.snapshots} activeSnapshot={data.activeSnapshot} at={data.at} />

		<main class="flex-1 p-4 md:p-6">
			{@render children()}
		</main>
	</div>
</div>
