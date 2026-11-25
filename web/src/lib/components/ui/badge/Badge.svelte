<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLAttributes } from 'svelte/elements';
	import { tv, type VariantProps } from 'tailwind-variants';
	import { cn } from '$lib/utils';

	const badgeVariants = tv({
		base: 'inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[11px] font-medium transition-colors',
		variants: {
			variant: {
				default: 'border-transparent bg-primary text-primary-foreground',
				secondary: 'border-transparent bg-secondary text-secondary-foreground',
				destructive: 'border-transparent bg-destructive/15 text-destructive border-destructive/30',
				outline: 'text-foreground border-border',
				success: 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-300 border-emerald-500/30',
				warning: 'bg-amber-500/15 text-amber-700 dark:text-amber-300 border-amber-500/30'
			}
		},
		defaultVariants: {
			variant: 'secondary'
		}
	});

	let {
		variant = 'secondary',
		class: className,
		children,
		...rest
	}: { variant?: VariantProps<typeof badgeVariants>['variant']; class?: string; children?: Snippet } & HTMLAttributes<HTMLSpanElement> = $props();
</script>

<span class={cn(badgeVariants({ variant }), className)} {...rest}>
	{@render children?.()}
</span>
