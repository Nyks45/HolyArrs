<script lang="ts">
	import { serverWords } from '#lib/core/i18n/server-words.ts';
	// Season pills with a scrollable episode browser in list or grid mode.
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import ArtPlaceholder from '#lib/ui/ArtPlaceholder.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import { wheelX } from '#lib/ui/drag-scroll.ts';
	import { runtimeWords } from '#lib/ui/TitleHero.svelte';
	import { newSeason, newestSeason, seasonName } from './labels.ts';
	import { animeEpisode, animePath, type AnimeView } from './anime.ts';
	import { api } from '../core/api.ts';
	import { date } from '../core/format.ts';
	import { sized } from '../core/tmdb-image.ts';
	import { t } from '#lib/core/i18n/i18n.svelte.ts';
	import { prefetchStreamSources } from '#lib/stream/index.ts';

	type Ep = { number: number; name?: string; overview?: string; runtime?: number; still?: string; air_day?: string };
	type EpCard = { key: number; title: string; rawName: string; still?: string | null; line: string; overview?: string };

	let {
		tmdb,
		seasons,
		fallback,
		onstream,
		progress = {}
	}: {
		tmdb: string;
		seasons: { number: number; episodes: number; year?: number }[];
		fallback?: string | null;
		onstream?: (season: number, episode: number) => void;
		progress?: Record<string, { progress_pct: number; completed: boolean; position_sec: number }>;
	} = $props();

	let picked = $state<number | null>(null);
	let eps = $state<Record<number, Ep[] | 'error'>>({});
	let epSearch = $state('');
	let sortAsc = $state(true);
	let viewMode = $state<'list' | 'grid'>('list');
	let expanded = $state(false);
	// Best effort: the page works without anime data.
	let anime = $state<AnimeView | null>(null);
	// A failed picture falls back to the placeholder.
	let stillState = $state<Record<string, 'ok' | 'bad'>>({});

	// Specials sort last, the rest newest first.
	const ordered = $derived([...seasons].sort((a, b) => Number(a.number === 0) - Number(b.number === 0) || b.number - a.number));
	const fresh = $derived(newSeason(seasons));
	const shown = $derived(picked ?? newestSeason(seasons) ?? null);
	const cur = $derived(shown === null ? undefined : seasons.find((s) => s.number === shown));

	// A new show drops the old pick and cached episodes.
	$effect(() => {
		void tmdb;
		picked = null;
		eps = {};
		epSearch = '';
		expanded = false;
	});
	$effect(() => {
		void shown;
		expanded = false;
	});
	$effect(() => {
		if (shown !== null && !eps[shown]) read(shown);
	});
	$effect(() => {
		const id = tmdb;
		const ctl = new AbortController();
		anime = null;
		api.get<AnimeView>(animePath('show', id), ctl.signal)
			.then((v) => (anime = v.anime ? v : null))
			.catch(() => {});
		return () => ctl.abort();
	});

	const asked = new Set<string>();
	// The set stops the effect from refetching a season while its request is in flight.
	function read(n: number) {
		const id = tmdb;
		if (asked.has(`${id}:${n}`)) return;
		asked.add(`${id}:${n}`);
		api.get<{ episodes?: Ep[] }>(`/metadata/show/${encodeURIComponent(id)}/seasons/${n}`)
			.then((r) => id === tmdb && (eps[n] = r.episodes ?? []))
			.catch(() => {
				asked.delete(`${id}:${n}`);
				if (id === tmdb) eps[n] = 'error';
			});
	}

	const visibleCards = $derived.by((): EpCard[] => {
		if (shown === null) return [];
		const list = eps[shown];
		if (!Array.isArray(list)) return [];
		const q = epSearch.trim().toLowerCase();
		const filtered = q
			? list.filter((e) => String(e.number) === q || (e.name ?? '').toLowerCase().includes(q) || (e.overview ?? '').toLowerCase().includes(q))
			: list;
		const sorted = sortAsc ? filtered : [...filtered].reverse();
		return sorted.map((e) => {
			const abs = anime ? animeEpisode(anime, shown, e.number).absolute : undefined;
			const when = e.air_day ? date(e.air_day) : t('shows.why.noAirDateShort');
			const rawName = e.name || t('shows.list.episodeFallback', { n: e.number });
			return {
				key: e.number,
				title: `${e.number}. ${rawName}`,
				rawName,
				still: e.still || fallback,
				line: [e.runtime ? runtimeWords(e.runtime) : '', abs ? `${when} · ${t('shows.list.absolute', { n: abs })}` : when].filter(Boolean).join(' · '),
				overview: e.overview
			};
		});
	});
</script>

{#snippet stillArt(sNum: number, c: EpCard, epProg: { progress_pct: number; completed: boolean; position_sec: number } | undefined)}
	{#if !c.still || stillState[`${sNum}:${c.key}`] !== 'ok'}
		<ArtPlaceholder name={!c.still || stillState[`${sNum}:${c.key}`] === 'bad' ? c.rawName : ''} shape="wide" loading={!!c.still && !stillState[`${sNum}:${c.key}`]} />
	{/if}
	{#if c.still && stillState[`${sNum}:${c.key}`] !== 'bad'}
		<img src={sized(c.still, 'backdrop', 300)} alt="" loading="lazy" decoding="async" draggable="false" onload={() => (stillState[`${sNum}:${c.key}`] = 'ok')} onerror={() => (stillState[`${sNum}:${c.key}`] = 'bad')} />
	{/if}
	{#if epProg?.completed}
		<span class="ep-done" title="Watched"><Icon name="check" size={12} /></span>
	{/if}
	{#if onstream}<span class="ep-play"><Icon name="play" size={15} /></span>{/if}
	{#if epProg && epProg.progress_pct > 0}
		<span class="ep-prog"><i style="width: {Math.min(100, epProg.progress_pct)}%"></i></span>
	{/if}
{/snippet}

{#if seasons.length}
	<section class="tp-section seasons" aria-labelledby="h-seasons" data-testid="season-row">
		<h2 id="h-seasons" class="tp-h2">{t('shows.folds.seasons')}</h2>
		<div class="pills" role="group" aria-label={t('shows.picker.season')} use:wheelX>
			{#each ordered as s (s.number)}
				<button type="button" class="pill" aria-pressed={shown === s.number} onclick={() => (picked = s.number)}>{seasonName(s.number)}{#if fresh === s.number}<span class="dot" aria-label={t('shows.picker.new')}></span>{/if}</button>
			{/each}
		</div>
		<div class="ep-bar">
			<div class="ep-meta-line">
				{#if cur}<p class="xs muted">{t(cur.episodes === 1 ? 'shows.list.countOne' : 'shows.list.countOther', { n: cur.episodes })}{cur.year ? ` · ${cur.year}` : ''}</p>{/if}
			</div>
			{#if shown !== null && Array.isArray(eps[shown]) && eps[shown].length > 0}
				<div class="ep-tools">
					<label class="ep-filter">
						<Icon name="search" size={13} />
						<input type="search" bind:value={epSearch} placeholder="Filter episodes..." aria-label="Filter episodes" />
					</label>
					<button type="button" class="ep-tool-btn" onclick={() => (sortAsc = !sortAsc)} title="Toggle episode order">
						{sortAsc ? '1..N' : 'N..1'}
					</button>
					<div class="ep-mode-group" role="group" aria-label="Episode layout">
						<button type="button" class="ep-mode-btn" class:on={viewMode === 'list'} aria-label="List view" onclick={() => (viewMode = 'list')}>
							<Icon name="list" size={14} />
						</button>
						<button type="button" class="ep-mode-btn" class:on={viewMode === 'grid'} aria-label="Grid view" onclick={() => (viewMode = 'grid')}>
							<Icon name="grid" size={14} />
						</button>
					</div>
					{#if visibleCards.length > 6}
						<button type="button" class="ep-tool-btn" onclick={() => (expanded = !expanded)}>
							{expanded ? 'Collapse' : `Expand all (${visibleCards.length})`}
						</button>
					{/if}
				</div>
			{/if}
		</div>

		{#if shown !== null}
			{#if eps[shown] === 'error'}
				<p class="small muted">{t('shows.list.readFailed', { season: seasonName(shown) })}</p>
			{:else if Array.isArray(eps[shown])}
				{#if visibleCards.length === 0}
					<p class="small muted">{epSearch.trim() ? 'No matching episodes.' : t('shows.list.none')}</p>
				{:else}
					<div class="ep-scroll" class:expanded aria-label={t('shows.folds.seasonEpisodes', { season: seasonName(shown) })}>
						<div class="ep-items" class:grid={viewMode === 'grid'} class:list={viewMode === 'list'}>
							{#each visibleCards as c (c.key)}
								{@const epProg = progress[`${shown}:${c.key}`]}
								<article
									class="ep"
									class:row={viewMode === 'list'}
									onmouseenter={() => { if (onstream && tmdb) prefetchStreamSources({ query: '', kind: 'show', tmdb: Number(tmdb) || 0, season: shown, episode: c.key }); }}
								>
									{#if onstream}
										<button
											type="button"
											class="still still-btn"
											aria-label={`Stream ${seasonName(shown)} ${c.title}`}
											title={epProg && !epProg.completed ? 'Resume episode' : 'Play episode'}
											onclick={() => onstream(shown, c.key)}
										>
											{@render stillArt(shown, c, epProg)}
										</button>
									{:else}
										<span class="still">
											{@render stillArt(shown, c, epProg)}
										</span>
									{/if}
									<div class="ep-body">
										<b class="ep-name">{c.title}</b>
										<span class="xs muted">{serverWords(c.line)}</span>
										{#if c.overview}<p class="ep-text">{c.overview}</p>{/if}
									</div>
								</article>
							{/each}
						</div>
					</div>
				{/if}
			{:else}
				<div class="sk" aria-busy="true">{#each [0, 1, 2] as i (i)}<Skeleton lines={3} />{/each}</div>
			{/if}
		{/if}
	</section>
{/if}

<style>
	.pills { display: flex; gap: 6px; overflow-x: auto; scrollbar-width: thin; scrollbar-color: var(--line) transparent; padding-bottom: 4px; }
	.pill { flex: none; display: inline-flex; align-items: center; gap: 6px; min-height: 34px; padding: 4px 14px; border: 1px solid var(--line); border-radius: 999px; background: transparent; color: var(--text); font: var(--t-subhead); font-weight: 600; cursor: pointer; }
	.pill[aria-pressed='true'] { background: var(--text); color: var(--bg); border-color: var(--text); }
	.pill:focus-visible { outline: none; box-shadow: var(--focus-ring); }
	@media (max-width: 767px) and (pointer: coarse) { .pill { min-height: 44px; } }
	.dot { width: 7px; height: 7px; border-radius: 50%; background: var(--accent); }

	.ep-bar { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; margin: 10px 0 8px; }
	.ep-meta-line p { margin: 0; }
	.ep-tools { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
	.ep-filter { position: relative; display: inline-flex; align-items: center; color: var(--text-muted); }
	.ep-filter > :global(.icon) { position: absolute; left: 9px; pointer-events: none; }
	.ep-filter input { height: 30px; width: 170px; padding: 0 10px 0 28px; border-radius: 999px; border: 1px solid var(--line); background: var(--surface); color: var(--text); font-size: 12.5px; }
	.ep-tool-btn { height: 30px; padding: 0 11px; border-radius: 999px; border: 1px solid var(--line); background: var(--surface); color: var(--text); font-size: 12px; font-weight: 600; cursor: pointer; }
	.ep-tool-btn:hover { background: var(--surface-2); }
	.ep-mode-group { display: inline-flex; border: 1px solid var(--line); border-radius: 999px; overflow: hidden; background: var(--surface); }
	.ep-mode-btn { display: grid; place-items: center; width: 32px; height: 28px; border: 0; background: transparent; color: var(--text-muted); cursor: pointer; }
	.ep-mode-btn.on { background: var(--text); color: var(--bg); }

	.ep-scroll { max-height: min(65vh, 560px); overflow-y: auto; overflow-x: hidden; overscroll-behavior: contain; padding-right: 4px; scrollbar-width: thin; }
	.ep-scroll.expanded { max-height: none; overflow-y: visible; padding-right: 0; }
	.ep-items.list { display: flex; flex-direction: column; gap: 10px; }
	.ep-items.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 14px; }

	.ep { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
	.ep.row { flex-direction: row; align-items: flex-start; gap: 14px; padding: 8px; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); }
	.ep.row .still { flex: none; width: clamp(148px, 22vw, 210px); margin-bottom: 0; }
	.ep.row .ep-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; padding-top: 2px; }
	.ep-body { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
	@media (max-width: 600px) {
		.ep.row { flex-direction: column; gap: 8px; }
		.ep.row .still { width: 100%; }
	}

	.still { position: relative; display: block; aspect-ratio: 16 / 9; margin-bottom: 4px; border-radius: 10px; overflow: hidden; background: var(--surface-2); }
	.still-btn { width: 100%; padding: 0; border: 0; text-align: left; cursor: pointer; }
	.still-btn:focus-visible { outline: none; box-shadow: var(--focus-ring); }
	.still img { position: relative; width: 100%; height: 100%; object-fit: cover; user-select: none; -webkit-user-drag: none; transition: transform var(--dur-fast) var(--ease-out); }
	.still-btn:hover img { transform: scale(1.03); }
	.ep-play { position: absolute; right: 8px; bottom: 8px; z-index: 2; display: inline-grid; place-items: center; width: 32px; height: 32px; padding: 0; border: 1px solid rgb(255 255 255 / 24%); border-radius: 999px; background: rgb(15 15 18 / 78%); color: #fff; backdrop-filter: blur(8px); transition: transform var(--dur-fast) var(--ease-out), background var(--dur-fast) var(--ease-out); }
	.still-btn:hover .ep-play { background: #fff; color: #111; transform: scale(1.08); }
	.ep-done { position: absolute; top: 8px; right: 8px; z-index: 2; display: inline-grid; place-items: center; width: 22px; height: 22px; border-radius: 999px; background: rgb(34 197 94 / 90%); color: #fff; }
	.ep-prog { position: absolute; left: 0; right: 0; bottom: 0; z-index: 2; height: 4px; background: rgb(255 255 255 / 26%); }
	.ep-prog i { display: block; height: 100%; background: #e50914; }
	.ep-name { font: 600 15px/1.3 var(--font-ui); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.ep.row .ep-name { white-space: normal; display: -webkit-box; -webkit-line-clamp: 1; line-clamp: 1; -webkit-box-orient: vertical; }
	.ep-text { margin: 2px 0 0; color: var(--text-muted); font: 400 13.5px/1.4 var(--font-ui); display: -webkit-box; -webkit-line-clamp: 3; line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; }
	.sk { display: flex; gap: 12px; margin-top: 12px; }
	.sk > :global(*) { flex: 1; }
</style>
