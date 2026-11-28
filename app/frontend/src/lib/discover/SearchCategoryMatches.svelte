<script lang="ts">
	import Icon from '#lib/ui/Icon.svelte';
	import Shelf from '#lib/ui/Shelf.svelte';
	import WideCard from '#lib/ui/WideCard.svelte';
	import { cardTmdb, discoverCard, type ExploreCard } from '#lib/ui/title-cards.ts';
	import { to } from '#lib/core/paths.ts';
	import { categoryTitleHref, fetchCategoryBrowse, fetchCategorySearch, type CategorySummary } from './subcategories.ts';

	let {
		query,
		scope = 'all',
		onlyLib = false,
		onmatches
	}: {
		query: string;
		scope?: string;
		onlyLib?: boolean;
		onmatches?: (has: boolean) => void;
	} = $props();

	let categories = $state<CategorySummary[]>([]);
	let activeId = $state('');
	let activeName = $state('');
	let cards = $state<ExploreCard[]>([]);
	let searchingCategories = $state(false);
	let pendingId = $state('');
	let previewError = $state('');
	let previewRequest: AbortController | undefined;
	let seq = 0;

	$effect(() => {
		const raw = query.trim();
		const sc = scope;
		const mySeq = ++seq;
		pendingId = '';
		previewError = '';
		if (onlyLib || raw.length < 2 || sc === 'music' || sc === 'people') {
			searchingCategories = false;
			categories = [];
			activeId = '';
			activeName = '';
			cards = [];
			onmatches?.(false);
			return;
		}
		searchingCategories = true;
		const ac = new AbortController();
		const timer = setTimeout(() => {
			void fetchCategorySearch(raw, sc, ac.signal).then(
				(res) => {
					if (mySeq !== seq) return;
					searchingCategories = false;
					categories = res.categories ?? [];
					activeId = res.category_id ?? '';
					activeName = res.category_name ?? '';
					cards = (res.category_items ?? []).map((d) => ({ ...discoverCard(d, `scat-${activeId || 'hit'}`), wide: d.backdrop ?? null }));
					onmatches?.(categories.length > 0 || cards.length > 0);
				},
				() => {
					if (mySeq !== seq) return;
					searchingCategories = false;
					categories = [];
					cards = [];
					onmatches?.(false);
				}
			);
		}, 160);
		return () => {
			clearTimeout(timer);
			ac.abort();
			previewRequest?.abort();
		};
	});

	async function previewCategory(c: CategorySummary) {
		previewRequest?.abort();
		const request = new AbortController();
		previewRequest = request;
		pendingId = c.id;
		previewError = '';
		try {
			const res = await fetchCategoryBrowse(c.id, scope, request.signal);
			if (request.signal.aborted) return;
			activeId = c.id;
			activeName = c.name;
			cards = (res.items ?? []).slice(0, 24).map((d) => ({ ...discoverCard(d, `scat-${c.id}`), wide: d.backdrop ?? null }));
			onmatches?.(true);
		} catch {
			if (!request.signal.aborted) previewError = `Could not load ${c.name}. Try again.`;
		} finally {
			if (previewRequest === request) pendingId = '';
		}
	}

</script>

{#if categories.length > 0 || cards.length > 0}
	<section class="cat-search" data-testid="search-category-matches" aria-label="Matching categories">
		{#if categories.length > 0}
			<div class="cat-chips-row">
				<span class="cat-label">
					<Icon name="compass" size={14} />
					<span>Categories</span>
				</span>
				<div class="cat-chips">
					{#each categories as c (c.id)}
						<button
							type="button"
							class="cat-chip"
							class:active={activeId === c.id}
							aria-pressed={activeId === c.id}
							aria-busy={pendingId === c.id}
							disabled={searchingCategories}
							onclick={() => void previewCategory(c)}
						>
							<span>{c.name}</span>
						</button>
					{/each}
					{#if activeId}
						<a class="cat-chip open-link" href={to(`/explore?cat=${encodeURIComponent(activeId)}`)}>
							<span>Open {activeName || activeId}</span>
							<Icon name="arrowUpRight" size={13} />
						</a>
					{/if}
				</div>
			</div>
		{/if}

		{#if cards.length > 0}
			<div class="cat-shelf">
				<Shelf
					title={activeName ? `${activeName} picks` : 'Category picks'}
					href={activeId ? to(`/explore?cat=${encodeURIComponent(activeId)}`) : undefined}
					item="var(--title-item)"
					items={cards}
					key={(c) => c.key}
					placeholder="wide"
					lines={1}
				>
					{#snippet card(c: ExploreCard)}
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
							typed={scope === 'all'}
							preview={tmdb && (c.kind === 'movie' || c.kind === 'show') ? { kind: c.kind, tmdb, year: c.year, titleId: c.titleId } : undefined}
						/>
					{/snippet}
				</Shelf>
			</div>
		{/if}
		{#if previewError}<p class="cat-error" role="status">{previewError}</p>{/if}
	</section>
{/if}

<style>
	.cat-search { display: flex; flex-direction: column; gap: 18px; }
	.cat-chips-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
	.cat-label {
		display: inline-flex; align-items: center; gap: 6px;
		font: 600 12.5px/1 var(--font-ui); color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.04em;
	}
	.cat-chips { display: flex; flex-wrap: wrap; gap: 8px; }
	.cat-chip {
		display: inline-flex; align-items: center; gap: 5px; min-height: 32px; padding: 0 13px;
		border-radius: 999px; border: 1px solid var(--line); background: var(--surface-2);
		color: var(--text); font: 600 12.5px/1 var(--font-ui); text-decoration: none; cursor: pointer;
		transition: background-color var(--dur-fast) var(--ease-out), border-color var(--dur-fast) var(--ease-out);
	}
	.cat-chip:hover { border-color: var(--text-faint); background: var(--surface-3); }
	.cat-chip.active { background: var(--text); color: var(--bg); border-color: var(--text); }
	.cat-chip[aria-busy='true'] { opacity: 0.65; }
	.cat-error { color: var(--text-muted); font: var(--t-footnote); }
	.cat-chip.open-link { color: var(--accent-text); background: color-mix(in srgb, var(--surface-2) 70%, transparent); }
	.cat-shelf { display: contents; }
	@media (min-width: 768px) { .cat-shelf { --shelf-gap: 5px; } }
</style>
