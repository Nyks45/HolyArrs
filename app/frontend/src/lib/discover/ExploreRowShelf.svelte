<!-- One Explore row: wide title cards (or posters for artists), its later pages read as it scrolls sideways, and See all in a grid. -->
<script lang="ts">
	import { t } from '#lib/core/i18n/i18n.svelte.ts';
	import { serverWords } from '#lib/core/i18n/server-words.ts';
	import IconButton from '#lib/ui/IconButton.svelte';
	import PosterCard from '#lib/ui/PosterCard.svelte';
	import Shelf from '#lib/ui/Shelf.svelte';
	import WideCard from '#lib/ui/WideCard.svelte';
	import { contextLine } from '#lib/ui/grow-lines.ts';
	import { cardAction, cardTmdb, followsByTmdb, ownedMark, ownedTitle, type ExploreCard, type ExploreRow, type OwnedIndex } from '#lib/ui/title-cards.ts';
	import type { Audience } from '#lib/ui/title-status.ts';
	import RowGridSheet from './RowGridSheet.svelte';
	import { createRowPager } from './row-pager.svelte.ts';

	let {
		row,
		cards,
		href,
		live = (c) => c,
		cardHref,
		searchHref,
		manage,
		mayAsk,
		who,
		owned,
		busy = () => false,
		onquick,
		oncount,
		serviceLogo = () => undefined,
		defer = false,
		dataRow,
		testid
	}: {
		row: ExploreRow & { type?: string; wide?: boolean };
		/** The row's cards as this page shows them (after its own dedupe). */
		cards: ExploreCard[];
		href?: string;
		live?: (c: ExploreCard) => ExploreCard;
		cardHref: (c: ExploreCard) => string | undefined;
		searchHref?: (c: ExploreCard) => string;
		manage: boolean;
		mayAsk: boolean;
		who: Audience;
		owned: OwnedIndex;
		busy?: (c: ExploreCard) => boolean;
		onquick: (c: ExploreCard, act: 'add' | 'ask') => void;
		oncount?: (rowType: string, event: 'open' | 'ask', titleLayer?: string) => void;
		serviceLogo?: (service: number) => string | undefined;
		defer?: boolean;
		/** The data-row the page counts opens by; none leaves it out. */
		dataRow?: string;
		testid?: string;
	} = $props();

	const gone = new AbortController();
	$effect(() => () => gone.abort());
	const pager = createRowPager(() => ({ id: row.id, more: row.more, cards }), gone.signal);
	let gridOpen = $state(false);

	const wideKind = (k: string): boolean => k === 'movie' || k === 'show';
	const anyWide = $derived(cards.some((c) => wideKind(c.kind)));
	const isWide = $derived(!!row.wide || anyWide);
	const rowType = $derived(row.type ?? row.id);
	// A row with its own page (a category) opens it; a row that pages opens its grid.
	const seeAll = $derived(href ? undefined : row.more ? () => (gridOpen = true) : undefined);
</script>

<div class="row-wrap" class:wide-row={isWide} data-row={dataRow} data-row-id={row.id} data-title-layer={row.titleLayer} data-testid={testid}>
	<Shelf title={serverWords(row.title)} subtitle={serverWords(row.subtitle)} {href} onseeall={seeAll} item={anyWide ? 'var(--title-item)' : 'var(--explore-item)'}
		items={pager.cards} key={(c) => c.key} {defer} placeholder={isWide ? 'wide' : 'poster'} lines={cards.some((c) => c.subtitle) ? 2 : 1}
		hasMore={pager.hasMore} onmore={() => void pager.load()}>
		{#snippet card(raw: ExploreCard)}
			{@const c = live(raw)}
			{@const act = cardAction(c, { manage, ask: mayAsk })}
			{@const mark = ownedMark(c, ownedTitle(c, owned), who)}
			{@const tmdb = cardTmdb(c)}
			{#if row.wide || wideKind(c.kind)}
				<WideCard cover title={c.name} href={cardHref(c)} image={c.wide ?? c.image} art={c.art} kind={c.kind} subtitle={c.subtitle} badges={c.badges} badgesAt="bottom" vtName={c.vtName} fresh={c.fresh} owned={mark} typed row={rowType}
					preview={tmdb && (c.kind === 'movie' || c.kind === 'show') ? { kind: c.kind, tmdb, year: c.year, titleId: c.titleId } : undefined}
					growService={c.streaming ? { name: c.streaming.name, logo: serviceLogo(c.streaming.service) } : null}
					growContext={contextLine({ state: mark?.state, label: mark?.label, mine: c.mine, fresh: c.fresh }, { rowType, rowTitle: row.title, fresh: c.fresh, streaming: c.streaming })}
					followTmdb={followsByTmdb(c)}>
					{#snippet growAction()}
						{#if act && tmdb}
							<IconButton icon="plus" label={act === 'add' ? t('discover.explore.add', { name: c.name }) : t('discover.explore.askFor', { name: c.name })} variant="glass" size="lg" aria-haspopup="dialog" aria-busy={busy(c) || undefined} onclick={() => { oncount?.(rowType, 'ask', row.titleLayer); onquick(c, act); }} />
						{/if}
					{/snippet}
				</WideCard>
			{:else}
				<PosterCard title={c.name} href={cardHref(c)} image={c.image} kind={c.kind} subtitle={c.subtitle} badges={c.badges} typed row={rowType} owned={mark} fresh={c.fresh} needsArt={c.kind === 'artist'} actionOnHover vtName={c.vtName}
					preview={tmdb && (c.kind === 'movie' || c.kind === 'show') ? { kind: c.kind, tmdb, year: c.year, titleId: c.titleId } : undefined}
					followTmdb={followsByTmdb(c)}>
					{#snippet action()}
						{#if act && tmdb}
							<IconButton icon="plus" label={act === 'add' ? t('discover.explore.add', { name: c.name }) : t('discover.explore.askFor', { name: c.name })} variant="glass" size="lg" aria-haspopup="dialog" aria-busy={busy(c) || undefined} onclick={() => { oncount?.(rowType, 'ask', row.titleLayer); onquick(c, act); }} />
						{:else if act && searchHref}
							<IconButton icon="plus" label={act === 'add' ? t('discover.explore.add', { name: c.name }) : t('discover.explore.askFor', { name: c.name })} variant="glass" size="lg" href={searchHref(c)} />
						{/if}
					{/snippet}
				</PosterCard>
			{/if}
		{/snippet}
	</Shelf>
</div>
{#if row.more}<RowGridSheet bind:open={gridOpen} title={serverWords(row.title)} subtitle={serverWords(row.subtitle)} {pager} href={cardHref} />{/if}

<style>
	.row-wrap { display: contents; }
	/* Wide title rows sit almost edge to edge: about 279px cards at a 1536px window with a 5px gap. */
	@media (min-width: 768px) { .row-wrap.wide-row { --shelf-gap: 5px; } }
</style>
