<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '#lib/core/api.ts';
	import { t } from '#lib/core/i18n/i18n.svelte.ts';
	import { serverWords } from '#lib/core/i18n/server-words.ts';
	import { to } from '#lib/core/paths.ts';
	import { session } from '#lib/core/session.svelte.ts';
	import type { TitleView } from '#lib/core/types.ts';
	import { ownedLibrary } from '#lib/mediafiles/index.ts';
	import { QuickAsk } from '#lib/requests/index.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Shelf from '#lib/ui/Shelf.svelte';
	import { SkRail } from '#lib/ui/sk/index.ts';
	import WideCard from '#lib/ui/WideCard.svelte';
	import { contextLine } from '#lib/ui/grow-lines.ts';
	import { cardAction, cardTmdb, ownedIndex, ownedMark, ownedTitle, type ExploreCard, type ExploreRow } from '#lib/ui/title-cards.ts';
	import { audienceOf } from '#lib/ui/title-status.ts';
	import { headPath, poolRetryDelay, serverRow, type HeadAnswer } from './explore-server.ts';
	import { typeHref } from './explore-type.ts';
	import { categoryTitleHref, fetchSubcategories, type CategorySummary } from './subcategories.ts';

	let {
		welcome = false
	}: {
		welcome?: boolean;
	} = $props();

	let rows = $state<(ExploreRow & { type: string })[]>([]);
	let subcats = $state<CategorySummary[]>([]);
	let titles = $state<TitleView[]>([]);
	let loaded = $state(false);
	let flow: ReturnType<typeof QuickAsk> | undefined = $state();
	let timer: ReturnType<typeof setTimeout> | undefined;

	const manage = $derived(session.can('manage_library'));
	const mayAsk = $derived(session.can('ask'));
	const who = $derived(audienceOf(session.can));
	const owned = $derived(ownedIndex(titles));
	const visibleRows = $derived((welcome
		? rows.filter((r) => r.type === 'because_watched' || r.type === 'because_opened' || r.type === 'subcategory')
		: rows).slice(0, 8));

	function liveCard(c: ExploreCard): ExploreCard {
		return flow?.live(c) ?? c;
	}

	function quick(c: ExploreCard, act: 'add' | 'ask') {
		void (act === 'ask' ? flow?.askNow(c) : flow?.open(c, act));
	}

	function rowHref(r: ExploreRow & { type: string }): string | undefined {
		if (r.category) return to(`/explore?cat=${encodeURIComponent(r.category)}`);
		return typeHref(r.href, 'all');
	}

	async function loadHomeExplore(tries = 0, signal?: AbortSignal) {
		try {
			const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || undefined;
			const ans = await api.get<HeadAnswer>(headPath({ type: 'all', tz }), signal);
			const nextRows = (ans.head ?? [])
				.map(serverRow)
				.filter((r) => r.type !== 'top_services' && r.cards.length > 0);
			rows = nextRows;
			const retry = ans.building ? poolRetryDelay(tries) : undefined;
			if (retry !== undefined) {
				clearTimeout(timer);
				timer = setTimeout(() => void loadHomeExplore(tries + 1, signal), retry);
			}
		} catch {
			/* Home stays clean even if Explore is warming up. */
		} finally {
			loaded = true;
		}
	}

	onMount(() => {
		const ac = new AbortController();
		void loadHomeExplore(0, ac.signal);
		void fetchSubcategories('all', '', '', ac.signal).then((list) => {
			subcats = list.slice(0, 12);
		}).catch(() => {});
		void ownedLibrary(api, ac.signal).then((list) => {
			titles = list.titles;
		}).catch(() => {});
		return () => {
			clearTimeout(timer);
			ac.abort();
		};
	});
</script>

{#if subcats.length > 0}
	<div class="subcat-jumps" aria-label="Quick categories">
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
			<span>Explore all</span>
		</a>
	</div>
{/if}

{#if !welcome && !loaded && rows.length === 0}
	<div class="home-sk"><SkRail kind="wide" item="var(--title-item)" count={5} titleWidth="180px" /></div>
{:else}
	{#each visibleRows as row (row.id)}
		<div class="wide-row" data-testid={`home-explore-${row.type}`}>
			<Shelf
				title={serverWords(row.title)}
				subtitle={row.subtitle ? serverWords(row.subtitle) : undefined}
				href={rowHref(row)}
				item="var(--title-item)"
				items={row.cards}
				key={(c) => c.key}
				placeholder="wide"
				lines={row.cards.some((c) => c.subtitle) ? 2 : 1}
			>
				{#snippet card(raw: ExploreCard)}
					{@const c = liveCard(raw)}
					{@const act = cardAction(c, { manage, ask: mayAsk })}
					{@const mark = ownedMark(c, ownedTitle(c, owned), who)}
					{@const tmdb = cardTmdb(c)}
					<WideCard
						cover
						title={c.name}
						href={categoryTitleHref(c)}
						image={c.wide ?? c.image}
						art={c.art}
						kind={c.kind}
						subtitle={c.subtitle}
						badges={c.badges}
						badgesAt="bottom"
						vtName={c.vtName}
						fresh={c.fresh}
						owned={mark}
						typed
						row={row.type}
						preview={tmdb && (c.kind === 'movie' || c.kind === 'show') ? { kind: c.kind, tmdb, year: c.year, titleId: c.titleId } : undefined}
						growContext={contextLine({ state: mark?.state, label: mark?.label, mine: c.mine, fresh: c.fresh }, { rowType: row.type, rowTitle: row.title, fresh: c.fresh, streaming: c.streaming })}
					>
						{#snippet growAction()}
							{#if act && tmdb}
								<IconButton
									icon="plus"
									label={act === 'add' ? t('discover.explore.add', { name: c.name }) : t('discover.explore.askFor', { name: c.name })}
									variant="glass"
									size="lg"
									aria-haspopup="dialog"
									aria-busy={flow?.busy(c) || undefined}
									onclick={() => quick(c, act)}
								/>
							{/if}
						{/snippet}
					</WideCard>
				{/snippet}
			</Shelf>
		</div>
	{/each}
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
	.wide-row { display: contents; }
	@media (min-width: 768px) { .wide-row { --shelf-gap: 5px; } }
	.home-sk { min-width: 0; }
</style>
