<script lang="ts">
	import Icon from '#lib/ui/Icon.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import LoadError from '#lib/ui/LoadError.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import WideCard from '#lib/ui/WideCard.svelte';
	import { SkGrid } from '#lib/ui/sk/index.ts';
	import { cardAction, cardTmdb, discoverCard, ownedMark, ownedTitle, type ExploreCard, type OwnedIndex } from '#lib/ui/title-cards.ts';
	import { type Audience } from '#lib/ui/title-status.ts';
	import type { ExploreType } from './explore-type.ts';
	import { categoryTitleHref, fetchCategoryBrowse, type CategorySummary } from './subcategories.ts';

	let {
		catId,
		type,
		categories = [],
		owned,
		manage = false,
		mayAsk = false,
		who,
		busyFor,
		onquick,
		onback,
		onopenall
	}: {
		catId: string;
		type: ExploreType;
		categories?: CategorySummary[];
		owned: OwnedIndex;
		manage?: boolean;
		mayAsk?: boolean;
		who: Audience;
		busyFor?: (c: ExploreCard) => boolean;
		onquick?: (c: ExploreCard, act: 'add' | 'ask') => void;
		onback: () => void;
		onopenall: () => void;
	} = $props();

	let loading = $state(true);
	let failed = $state(false);
	let name = $state('');
	let cards = $state<ExploreCard[]>([]);
	let askSeq = 0;

	async function loadCat(id: string, t: ExploreType, signal?: AbortSignal) {
		const seq = ++askSeq;
		loading = true;
		failed = false;
		try {
			const res = await fetchCategoryBrowse(id, t, signal);
			if (seq !== askSeq) return;
			name = res.name || categories.find((c) => c.id === id)?.name || id;
			cards = (res.items ?? []).map((d) => ({ ...discoverCard(d, `cat-${id}`), wide: d.backdrop ?? null }));
		} catch {
			if (seq === askSeq) {
				failed = true;
				cards = [];
			}
		} finally {
			if (seq === askSeq) loading = false;
		}
	}

	$effect(() => {
		const id = catId;
		const t = type;
		if (!id) return;
		const ac = new AbortController();
		void loadCat(id, t, ac.signal);
		return () => ac.abort();
	});

</script>

<section class="cat-view" data-testid="explore-subcategory-view" aria-label={name || catId}>
	<header class="cat-head">
		<div class="cat-left">
			<button type="button" class="back-btn" onclick={onback}>
				<Icon name="back" size={16} />
				<span>Explore</span>
			</button>
			<div class="cat-title">
				<h2>{name || categories.find((c) => c.id === catId)?.name || catId}</h2>
				{#if !loading && !failed}
					<span class="cat-count">{cards.length} {cards.length === 1 ? 'title' : 'titles'}</span>
				{/if}
			</div>
		</div>
		<button type="button" class="switch-btn" onclick={onopenall}>
			<Icon name="sliders" size={14} />
			<span>All subcategories</span>
		</button>
	</header>

	{#if failed}
		<LoadError hasData={false} message="Could not load this subcategory right now." onretry={() => void loadCat(catId, type)} />
	{:else if loading}
		<SkGrid kind="wide" min="var(--title-item)" count={12} />
	{:else if cards.length === 0}
		<EmptyState icon="compass" title="No titles in this subcategory yet" text="Try switching to All or choosing another subcategory." />
	{:else}
		<ul class="cat-grid">
			{#each cards as c (c.key)}
				{@const act = cardAction(c, { manage, ask: mayAsk })}
				{@const mark = ownedMark(c, ownedTitle(c, owned), who)}
				{@const tmdb = cardTmdb(c)}
				<li>
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
						preview={tmdb && (c.kind === 'movie' || c.kind === 'show') ? { kind: c.kind, tmdb, year: c.year, titleId: c.titleId } : undefined}
					>
						{#snippet growAction()}
							{#if act && tmdb && onquick}
								<IconButton
									icon="plus"
									label={act === 'add' ? `Add ${c.name}` : `Request ${c.name}`}
									variant="glass"
									size="lg"
									aria-haspopup="dialog"
									aria-busy={busyFor?.(c) || undefined}
									onclick={() => onquick(c, act)}
								/>
							{/if}
						{/snippet}
					</WideCard>
				</li>
			{/each}
		</ul>
	{/if}
</section>

<style>
	.cat-view { display: flex; flex-direction: column; gap: 20px; }
	.cat-head {
		display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap;
		padding: 14px 18px; border-radius: 16px; border: 1px solid var(--line);
		background: color-mix(in srgb, var(--surface-2) 66%, transparent);
	}
	.cat-left { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; min-width: 0; }
	.back-btn, .switch-btn {
		display: inline-flex; align-items: center; gap: 6px; min-height: 34px; padding: 0 12px;
		border-radius: 999px; border: 1px solid var(--line); background: var(--surface-1);
		color: var(--text); font: 600 13px/1 var(--font-ui); cursor: pointer;
	}
	.back-btn:hover, .switch-btn:hover { border-color: var(--text-faint); background: var(--surface-2); }
	.cat-title { display: flex; align-items: baseline; gap: 10px; min-width: 0; }
	.cat-title h2 { margin: 0; font: 700 22px/1.2 var(--font-display); color: var(--text); }
	.cat-count { font: 600 12.5px/1 var(--font-ui); color: var(--text-muted); font-variant-numeric: tabular-nums; }
	.cat-grid {
		display: grid; grid-template-columns: repeat(auto-fill, minmax(var(--title-item, 240px), 1fr));
		gap: 18px 8px; margin: 0; padding: 0; list-style: none;
	}
</style>
