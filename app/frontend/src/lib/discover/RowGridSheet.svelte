<!-- See all for an Explore row: every card the row has in a grid, reading the row's later pages as the grid scrolls down. -->
<script lang="ts">
	import Sheet from '#lib/ui/Sheet.svelte';
	import PosterCard from '#lib/ui/PosterCard.svelte';
	import { SkCard } from '#lib/ui/sk/index.ts';
	import type { ExploreCard } from '#lib/ui/title-cards.ts';
	import type { RowPager } from './row-pager.svelte.ts';

	let {
		open = $bindable(false),
		title,
		subtitle,
		pager,
		href
	}: {
		open?: boolean;
		title: string;
		subtitle?: string;
		pager: RowPager;
		href: (c: ExploreCard) => string | undefined;
	} = $props();

	let sentinel = $state<HTMLElement>();
	// The next page loads while the end of the grid is within a screen of view.
	$effect(() => {
		if (!open || !sentinel) return;
		const io = new IntersectionObserver(([e]) => e.isIntersecting && void pager.load(), { rootMargin: '0px 0px 100% 0px' });
		io.observe(sentinel);
		return () => io.disconnect();
	});
	// A short first page leaves the sentinel in view: keep reading until the grid fills or the row ends.
	$effect(() => {
		if (!open || !sentinel || pager.busy || !pager.hasMore) return;
		void pager.cards.length;
		const r = sentinel.getBoundingClientRect();
		if (r.top < innerHeight * 2) void pager.load();
	});
</script>

<Sheet bind:open {title} description={subtitle} size="xl">
	<div class="grid" data-testid="row-grid">
		{#each pager.cards as c (c.key)}
			<PosterCard title={c.name} href={href(c)} image={c.image} kind={c.kind} subtitle={c.subtitle} badges={c.badges} fresh={c.fresh} />
		{/each}
		{#if pager.hasMore}
			{#each Array.from({ length: 6 }, (_, i) => i) as i (i)}<div class="sk" aria-hidden="true"><SkCard /></div>{/each}
		{/if}
	</div>
	<div class="end" bind:this={sentinel} aria-hidden="true"></div>
</Sheet>

<style>
	.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 16px 12px; }
	@media (max-width: 767px) { .grid { grid-template-columns: repeat(auto-fill, minmax(104px, 1fr)); gap: 12px 8px; } }
	.end { height: 1px; }
</style>
