<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { api } from '#lib/core/api.ts';
	import { t } from '#lib/core/i18n/i18n.svelte.ts';
	import { to } from '#lib/core/paths.ts';
	import { session } from '#lib/core/session.svelte.ts';
	import type { TitleView } from '#lib/core/types.ts';
	import { ownedLibrary } from '#lib/mediafiles/index.ts';
	import { QuickAsk } from '#lib/requests/index.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import { SkRail } from '#lib/ui/sk/index.ts';
	import { EAGER_ROWS } from '#lib/ui/row-window.ts';
	import { ownedIndex, type ExploreCard } from '#lib/ui/title-cards.ts';
	import { audienceOf } from '#lib/ui/title-status.ts';
	import ExploreRowShelf from './ExploreRowShelf.svelte';
	import { headPath, poolRetryDelay, serverRow, type HeadAnswer, type HeadQuery } from './explore-server.ts';
	import { createExploreTail, tailSentinel, type TailRow } from './explore-tail.svelte.ts';
	import { typeHref } from './explore-type.ts';
	import { categoryTitleHref, fetchSubcategories, type CategorySummary } from './subcategories.ts';

	let {
		welcome = false,
		head: given
	}: {
		welcome?: boolean;
		/** The head Home already read (the welcome picks); the section then reads none of its own. */
		head?: HeadAnswer | null;
	} = $props();

	let headRows = $state<TailRow[]>([]);
	let subcats = $state<CategorySummary[]>([]);
	let titles = $state<TitleView[]>([]);
	let loaded = $state(false);
	let flow: ReturnType<typeof QuickAsk> | undefined = $state();
	let timer: ReturnType<typeof setTimeout> | undefined;
	const gone = new AbortController();

	const manage = $derived(session.can('manage_library'));
	const mayAsk = $derived(session.can('ask'));
	const who = $derived(audienceOf(session.can));
	const owned = $derived(ownedIndex(titles));
	const tz = (() => {
		try {
			return Intl.DateTimeFormat().resolvedOptions().timeZone || undefined;
		} catch {
			return undefined;
		}
	})();
	const query = (): HeadQuery => ({ type: 'all', tz });

	// A fresh install shows the rows picked for the person first; everyone gets the whole head, then the endless tail.
	const shown = $derived(welcome ? headRows.filter((r) => r.type === 'because_watched' || r.type === 'because_opened' || r.type === 'subcategory') : headRows);
	let sentinel = $state<HTMLElement>();
	const tail = createExploreTail({ query, signal: gone.signal, head: () => headRows, near: () => !!sentinel && sentinel.getBoundingClientRect().top < innerHeight * 2.5 });
	const rows = $derived([...shown, ...tail.rows]);

	function useHead(a: HeadAnswer) {
		const next = (a.head ?? []).map(serverRow).filter((r) => r.type !== 'top_services' && r.cards.length > 0);
		const same = loaded && next.length === headRows.length && next.every((r, i) => r.id === headRows[i].id);
		headRows = next;
		if (!same) tail.reset(true);
		loaded = true;
	}

	$effect(() => {
		const g = given;
		if (g) untrack(() => useHead(g));
	});

	async function loadHead(tries = 0) {
		try {
			const a = await api.get<HeadAnswer>(headPath(query()), gone.signal);
			useHead(a);
			const retry = a.building ? poolRetryDelay(tries) : undefined;
			if (retry !== undefined) {
				clearTimeout(timer);
				timer = setTimeout(() => void loadHead(tries + 1), retry);
			}
		} catch {
			// Home stays clean while Explore warms up.
			loaded = true;
		}
	}

	function cardHref(c: ExploreCard): string | undefined {
		if (c.titleId !== undefined) return to(`/library/${c.titleId}`);
		return categoryTitleHref(c);
	}

	function rowHref(r: TailRow): string | undefined {
		if (r.category) return to(`/explore?cat=${encodeURIComponent(r.category)}`);
		return typeHref(r.href, 'all');
	}

	onMount(() => {
		if (!welcome && !given) void loadHead();
		void fetchSubcategories('all', '', '', gone.signal).then((list) => {
			subcats = list.slice(0, 12);
		}).catch(() => {});
		void ownedLibrary(api, gone.signal).then((list) => {
			titles = list.titles;
		}).catch(() => {});
		return () => {
			clearTimeout(timer);
			gone.abort();
		};
	});
</script>

{#if subcats.length > 0}
	<div class="subcat-jumps" aria-label={t('discover.categoryStrip.categories')}>
		{#each [
			{ id: 'anime', name: 'Anime', href: '/explore?type=anime', spotlight: true },
			{ id: 'kdrama', name: 'K-Drama', href: '/explore?hub=kdrama', spotlight: false },
			{ id: 'turkish', name: 'Turkish', href: '/explore?hub=turkish', spotlight: false }
		] as hub (hub.id)}
			<a class="jump-pill" class:spotlight={hub.spotlight} href={to(hub.href)}>{hub.name}</a>
		{/each}
		{#each subcats.slice(0, 8) as cat (cat.id)}
			<a class="jump-pill" href={to(`/explore?cat=${encodeURIComponent(cat.id)}`)}>{cat.name}</a>
		{/each}
		<a class="jump-pill all" href={to('/explore')}>
			<Icon name="compass" size={13} />
			<span>{t('home.explore')}</span>
		</a>
	</div>
{/if}

{#if !welcome && !loaded && headRows.length === 0}
	<div class="home-sk"><SkRail kind="wide" item="var(--title-item)" count={5} titleWidth="180px" /></div>
{:else}
	{#each rows as row, ri (row.id)}
		<ExploreRowShelf {row} cards={row.cards} href={rowHref(row)} live={(c) => flow?.live(c) ?? c} {cardHref} {manage} {mayAsk} {who} {owned}
			busy={(c) => flow?.busy(c) ?? false} onquick={(c, act) => void (act === 'ask' ? flow?.askNow(c) : flow?.open(c, act))}
			defer={ri >= EAGER_ROWS} testid={`home-explore-${row.type}`} />
	{/each}
	{#if loaded && tail.more}<div class="sentinel" bind:this={sentinel} use:tailSentinel={() => void tail.load()} aria-hidden="true"></div>{/if}
{/if}

<QuickAsk bind:this={flow} />

<style>
	.subcat-jumps { display: flex; flex-wrap: nowrap; gap: 8px; overflow-x: auto; scrollbar-width: none; padding: 2px 0; }
	.subcat-jumps::-webkit-scrollbar { display: none; }
	.jump-pill {
		flex: none; display: inline-flex; align-items: center; gap: 5px; min-height: 32px; padding: 0 13px;
		border-radius: 999px; border: 1px solid var(--line); background: var(--surface-1);
		color: var(--text-muted); font: 600 12.5px/1 var(--font-ui); text-decoration: none; white-space: nowrap;
		transition: color var(--dur-fast) var(--ease-out), border-color var(--dur-fast) var(--ease-out), background-color var(--dur-fast) var(--ease-out);
	}
	.jump-pill:hover { color: var(--text); border-color: var(--text-faint); background: var(--surface-2); }
	.jump-pill.spotlight { color: var(--text); border-color: color-mix(in srgb, #f43f5e 45%, var(--line)); background: color-mix(in srgb, #f43f5e 10%, var(--surface-1)); }
	.jump-pill.all { color: var(--text); border-style: dashed; }
	.home-sk { min-width: 0; }
	.sentinel { height: 1px; }
</style>
