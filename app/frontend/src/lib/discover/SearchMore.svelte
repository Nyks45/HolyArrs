<!-- Search results past the first page, as grid items after it: read as the grid's end comes near. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { api } from '#lib/core/api.ts';
	import type { Card } from './search-cards.svelte.ts';
	import { newResults, SEARCH_PAGE_LAST, searchMorePath, type SearchMoreAnswer } from './search-more.ts';

	let {
		kind,
		q,
		have,
		card
	}: {
		kind: 'movie' | 'show';
		/** The words the first page was searched with. */
		q: string;
		/** The first page's results. */
		have: readonly Card[];
		card: Snippet<[Card]>;
	} = $props();

	let extra = $state<Card[]>([]);
	let more = $state(true);
	let busy = $state(false);
	let page = 2;
	let forQ = '';
	const gone = new AbortController();
	$effect(() => () => gone.abort());

	// A new search starts its own pages.
	$effect(() => {
		const k = `${kind}|${q}`;
		if (k === forQ) return;
		forQ = k;
		extra = [];
		page = 2;
		more = true;
	});

	/** Reads the next page; says whether it read one. */
	async function load(): Promise<boolean> {
		if (busy || !more || !q) return false;
		const k = forQ;
		busy = true;
		try {
			const a = await api.get<SearchMoreAnswer>(searchMorePath(kind, q, page), gone.signal);
			if (k !== forQ) return false;
			page++;
			extra = [...extra, ...newResults([...have, ...extra], a.items)];
			more = a.more && page <= SEARCH_PAGE_LAST;
			return true;
		} catch {
			if (k === forQ) more = false;
			return false;
		} finally {
			busy = false;
		}
	}

	// The end staying in view (a short page, or a scroll to the bottom) keeps reading, a page at a time.
	function sentinel(node: HTMLElement) {
		const pump = () => void load().then((read) => read && requestAnimationFrame(() => node.isConnected && node.getBoundingClientRect().top < innerHeight * 2.5 && pump()));
		const io = new IntersectionObserver(([e]) => e.isIntersecting && pump(), { rootMargin: '0px 0px 150% 0px' });
		io.observe(node);
		return { destroy: () => io.disconnect() };
	}
</script>

{#each extra as c (c.key)}<li>{@render card(c)}</li>{/each}
{#if more && have.length >= 10}<li class="end" use:sentinel aria-hidden="true"></li>{/if}

<style>
	.end { grid-column: 1 / -1; height: 1px; }
</style>
