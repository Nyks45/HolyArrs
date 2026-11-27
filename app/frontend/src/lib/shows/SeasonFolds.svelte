<script lang="ts">
	import { untrack } from 'svelte';
	import { serverWords } from '#lib/core/i18n/server-words.ts';
	// Season pills with a scrollable episode browser in list or grid mode.
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import ArtPlaceholder from '#lib/ui/ArtPlaceholder.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import { wheelX } from '#lib/ui/drag-scroll.ts';
	import { runtimeWords } from '#lib/ui/TitleHero.svelte';
	import { newSeason, newestSeason, seasonName } from './labels.ts';
	import { animeEpisode, animePath, type AnimeView } from './anime.ts';
	import {
		classifyAirDate,
		summarizeSeasonEpisodes,
		type EpisodeReleaseInfo,
		type EpProgress,
		type ShowResumeTarget
	} from './episode-tracking.ts';
	import { api } from '../core/api.ts';
	import { date } from '../core/format.ts';
	import { sized } from '../core/tmdb-image.ts';
	import { t } from '#lib/core/i18n/i18n.svelte.ts';
	import { prefetchStreamSources } from '#lib/stream/index.ts';

	type Ep = { number: number; name?: string; overview?: string; runtime?: number; still?: string; air_day?: string };
	type EpCard = { key: number; title: string; rawName: string; still?: string | null; line: string; overview?: string; release: EpisodeReleaseInfo };

	let {
		tmdb,
		seasons,
		fallback,
		showMeta,
		defaultSeason = null,
		activeTarget = null,
		onstream,
		onstreamSources,
		onToggleWatched,
		onMarkSeasonWatched,
		progress = {}
	}: {
		tmdb: string;
		seasons: { number: number; episodes: number; year?: number }[];
		fallback?: string | null;
		showMeta?: { name: string; year?: number; imdb?: string; original_title?: string; genres?: string[]; original_language?: string; origin_country?: string[] };
		defaultSeason?: number | null;
		activeTarget?: ShowResumeTarget | null;
		onstream?: (season: number, episode: number) => void;
		onstreamSources?: (season: number, episode: number) => void;
		onToggleWatched?: (season: number, episode: number, epName: string, watched: boolean) => void | Promise<void>;
		onMarkSeasonWatched?: (season: number, episodes: { number: number; name?: string }[], watched: boolean) => void | Promise<void>;
		progress?: Record<string, EpProgress>;
	} = $props();

	let picked = $state<number | null>(null);
	let eps = $state<Record<number, Ep[] | 'error'>>({});
	let epSearch = $state('');
	let statusFilter = $state<'all' | 'out' | 'unwatched' | 'upcoming'>('all');
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
	const validPicked = $derived(picked !== null && seasons.some((s) => s.number === picked) ? picked : null);
	const validDefault = $derived(defaultSeason !== null && seasons.some((s) => s.number === defaultSeason) ? defaultSeason : null);
	const shown = $derived(validPicked ?? validDefault ?? newestSeason(seasons) ?? null);
	const cur = $derived(shown === null ? undefined : seasons.find((s) => s.number === shown));
	let retryTick = $state(0);
	let hoverPrefetchTimer: ReturnType<typeof setTimeout> | null = null;

	function clearHoverPrefetch() {
		if (hoverPrefetchTimer) {
			clearTimeout(hoverPrefetchTimer);
			hoverPrefetchTimer = null;
		}
	}

	function scheduleHoverPrefetch(sNum: number, epNum: number) {
		if (!onstream || !tmdb) return;
		clearHoverPrefetch();
		hoverPrefetchTimer = setTimeout(() => {
			hoverPrefetchTimer = null;
			prefetchStreamSources({
				query: showMeta?.name ?? '',
				kind: 'show',
				year: showMeta?.year || 0,
				season: sNum,
				episode: epNum,
				imdb: showMeta?.imdb ?? '',
				tmdb: Number(tmdb) || 0,
				original_title: showMeta?.original_title,
				genres: showMeta?.genres,
				original_language: showMeta?.original_language,
				origin_country: showMeta?.origin_country
			});
		}, 350);
	}

	function retrySeason(n: number) {
		const next = { ...eps };
		delete next[n];
		eps = next;
		retryTick++;
	}

	// A new show drops the old pick and cached episodes.
	$effect(() => {
		void tmdb;
		picked = null;
		eps = {};
		stillState = {};
		epSearch = '';
		statusFilter = 'all';
		expanded = false;
	});
	$effect(() => {
		void shown;
		statusFilter = 'all';
		expanded = false;
	});
	$effect(() => () => clearHoverPrefetch());
	$effect(() => {
		const id = tmdb;
		const n = shown;
		void retryTick;
		if (!id || n === null || !seasons.some((s) => s.number === n)) return;
		const existing = untrack(() => eps[n]);
		if (Array.isArray(existing) || existing === 'error') return;
		const ctl = new AbortController();
		api.get<{ episodes?: Ep[] }>(`/metadata/show/${encodeURIComponent(id)}/seasons/${n}`, ctl.signal)
			.then((r) => {
				if (!ctl.signal.aborted && id === tmdb) eps[n] = r.episodes ?? [];
			})
			.catch(() => {
				if (!ctl.signal.aborted && id === tmdb) eps[n] = 'error';
			});
		return () => ctl.abort();
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

	const seasonList = $derived(shown !== null && Array.isArray(eps[shown]) ? eps[shown] : []);
	const seasonSummary = $derived(shown !== null && seasonList.length > 0 ? summarizeSeasonEpisodes(seasonList, shown, progress) : null);

	const visibleCards = $derived.by((): EpCard[] => {
		if (shown === null || !seasonList.length) return [];
		const q = epSearch.trim().toLowerCase();
		const sorted = sortAsc ? seasonList : [...seasonList].reverse();
		const out: EpCard[] = [];
		for (const e of sorted) {
			const release = classifyAirDate(e.air_day);
			const done = !!progress[`${shown}:${e.number}`]?.completed;
			if (statusFilter === 'out' && !release.isReleased) continue;
			if (statusFilter === 'unwatched' && (!release.isReleased || done)) continue;
			if (statusFilter === 'upcoming' && release.isReleased) continue;
			if (q && String(e.number) !== q && !(e.name ?? '').toLowerCase().includes(q) && !(e.overview ?? '').toLowerCase().includes(q)) continue;
			const abs = anime ? animeEpisode(anime, shown, e.number).absolute : undefined;
			const when = e.air_day ? date(e.air_day) : t('shows.why.noAirDateShort');
			const rawName = e.name || t('shows.list.episodeFallback', { n: e.number });
			out.push({
				key: e.number,
				title: `${e.number}. ${rawName}`,
				rawName,
				still: e.still || fallback,
				line: [e.runtime ? runtimeWords(e.runtime) : '', abs ? `${when} · ${t('shows.list.absolute', { n: abs })}` : when].filter(Boolean).join(' · '),
				overview: e.overview,
				release
			});
		}
		return out;
	});
</script>

{#snippet stillArt(sNum: number, c: EpCard, epProg: EpProgress | undefined)}
	{#if !c.still || stillState[`${sNum}:${c.key}`] !== 'ok'}
		<ArtPlaceholder name={!c.still || stillState[`${sNum}:${c.key}`] === 'bad' ? c.rawName : ''} shape="wide" loading={!!c.still && !stillState[`${sNum}:${c.key}`]} />
	{/if}
	{#if c.still && stillState[`${sNum}:${c.key}`] !== 'bad'}
		<img src={sized(c.still, 'backdrop', 300)} alt="" loading="lazy" decoding="async" draggable="false" onload={() => (stillState[`${sNum}:${c.key}`] = 'ok')} onerror={() => (stillState[`${sNum}:${c.key}`] = 'bad')} />
	{/if}
	{#if c.release.badge}
		<span class="ep-rel {c.release.status}">{c.release.badge}</span>
	{/if}
	{#if epProg?.completed}
		<span class="ep-done" title="Watched"><Icon name="check" size={12} /></span>
	{/if}
	{#if onstream && c.release.isReleased}<span class="ep-play"><Icon name="play" size={15} /></span>{/if}
	{#if epProg && epProg.progress_pct > 0}
		<span class="ep-prog"><i style="width: {Math.min(100, epProg.progress_pct)}%"></i></span>
	{/if}
{/snippet}

{#if seasons.length}
	<section class="tp-section seasons" aria-labelledby="h-seasons" data-testid="season-row">
		<h2 id="h-seasons" class="tp-h2">{t('shows.folds.seasons')}</h2>
		<div class="pills" role="group" aria-label={t('shows.picker.season')} use:wheelX>
			{#each ordered as s (s.number)}
				<button type="button" class="pill" aria-pressed={shown === s.number} onclick={() => { if (eps[s.number] === 'error') retrySeason(s.number); picked = s.number; }}>{seasonName(s.number)}{#if fresh === s.number}<span class="dot" aria-label={t('shows.picker.new')}></span>{/if}</button>
			{/each}
		</div>
		{#if seasonSummary && shown !== null}
			{@const airedList = seasonList.filter((ep) => classifyAirDate(ep.air_day).isReleased)}
			<div class="ep-summary" data-testid="season-summary">
				<div class="ep-sum-copy">
					<span class="ep-sum-text">
						{seasonSummary.airedCount === seasonSummary.total ? `All ${seasonSummary.total} out` : `${seasonSummary.airedCount} of ${seasonSummary.total} out`} · {seasonSummary.watchedCount} watched{#if seasonSummary.nextUpcoming} · Next E{seasonSummary.nextUpcoming.episode} ({seasonSummary.nextUpcoming.badge}){/if}
					</span>
					<div class="ep-sum-bar"><i style="width: {seasonSummary.airedCount > 0 ? Math.round((seasonSummary.watchedCount / seasonSummary.airedCount) * 100) : 0}%"></i></div>
				</div>
				{#if onMarkSeasonWatched && airedList.length > 0}
					<button type="button" class="ep-tool-btn ep-mark-season" onclick={() => onMarkSeasonWatched(shown, airedList, !seasonSummary.allAiredWatched)}>
						<Icon name="check" size={13} />
						<span>{seasonSummary.allAiredWatched ? 'Mark season unwatched' : 'Mark season watched'}</span>
					</button>
				{/if}
			</div>
		{/if}
		<div class="ep-bar">
			<div class="ep-meta-line">
				{#if seasonSummary}
					<div class="ep-status-chips" role="group" aria-label="Filter episodes by status">
						<button type="button" class="ep-chip" class:on={statusFilter === 'all'} onclick={() => (statusFilter = 'all')}>All ({seasonSummary.total})</button>
						<button type="button" class="ep-chip" class:on={statusFilter === 'out'} onclick={() => (statusFilter = 'out')}>Out ({seasonSummary.airedCount})</button>
						<button type="button" class="ep-chip" class:on={statusFilter === 'unwatched'} onclick={() => (statusFilter = 'unwatched')}>Unwatched ({seasonSummary.unwatchedAiredCount})</button>
						{#if seasonSummary.upcomingCount + seasonSummary.tbaCount > 0}
							<button type="button" class="ep-chip" class:on={statusFilter === 'upcoming'} onclick={() => (statusFilter = 'upcoming')}>Upcoming ({seasonSummary.upcomingCount + seasonSummary.tbaCount})</button>
						{/if}
					</div>
				{:else if cur}
					<p class="xs muted">{t(cur.episodes === 1 ? 'shows.list.countOne' : 'shows.list.countOther', { n: cur.episodes })}{cur.year ? ` · ${cur.year}` : ''}</p>
				{/if}
			</div>
			{#if shown !== null && seasonList.length > 0}
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
				<div class="ep-err">
					<p class="small muted">{t('shows.list.readFailed', { season: seasonName(shown) })}</p>
					<button type="button" class="ep-tool-btn" onclick={() => retrySeason(shown)}>Retry</button>
				</div>
			{:else if Array.isArray(eps[shown])}
				{#if visibleCards.length === 0}
					<p class="small muted">{epSearch.trim() || statusFilter !== 'all' ? 'No matching episodes.' : t('shows.list.none')}</p>
				{:else}
					<div class="ep-scroll" class:expanded aria-label={t('shows.folds.seasonEpisodes', { season: seasonName(shown) })}>
						<div class="ep-items" class:grid={viewMode === 'grid'} class:list={viewMode === 'list'}>
							{#each visibleCards as c (c.key)}
								{@const epProg = progress[`${shown}:${c.key}`]}
								{@const isNextUp = activeTarget?.season === shown && activeTarget?.episode === c.key && (activeTarget.kind === 'resume' || activeTarget.kind === 'next')}
								<article
									class="ep"
									class:row={viewMode === 'list'}
									class:upcoming={!c.release.isReleased}
									class:next-up={isNextUp}
									onmouseenter={() => { if (c.release.isReleased) scheduleHoverPrefetch(shown, c.key); }}
									onmouseleave={clearHoverPrefetch}
								>
									{#if onstream && c.release.isReleased}
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
										<div class="ep-title-row">
											{#if onstream && c.release.isReleased}
												<button type="button" class="ep-title-btn" onclick={() => onstream(shown, c.key)}>{c.title}</button>
											{:else}
												<b class="ep-name">{c.title}</b>
											{/if}
											{#if isNextUp}
												<span class="ep-tag-next">{activeTarget?.kind === 'resume' ? 'Resume' : 'Next up'}</span>
											{/if}
											{#if c.release.isReleased && (onToggleWatched || onstreamSources)}
												<div class="ep-acts">
													{#if onToggleWatched}
														<button type="button" class="ep-act-btn" class:done={!!epProg?.completed} aria-pressed={!!epProg?.completed} title={epProg?.completed ? 'Mark unwatched' : 'Mark watched'} aria-label={epProg?.completed ? `Mark ${c.title} unwatched` : `Mark ${c.title} watched`} onclick={() => onToggleWatched(shown, c.key, c.rawName, !epProg?.completed)}>
															<Icon name="check" size={13} />
														</button>
													{/if}
													{#if onstreamSources}
														<button type="button" class="ep-act-btn" title="Pick stream source" aria-label={`Sources for ${seasonName(shown)} ${c.title}`} onclick={() => onstreamSources(shown, c.key)}>
															<Icon name="source" size={13} />
														</button>
													{/if}
												</div>
											{/if}
										</div>
										<span class="xs muted">{serverWords(c.line)}</span>
										{#if c.overview}<p class="ep-text">{c.overview}</p>{/if}
									</div>
								</article>
							{/each}
						</div>
					</div>
				{/if}
			{:else}
				<div class="ep-sk" aria-busy="true">{#each [0, 1, 2] as i (i)}<Skeleton lines={3} />{/each}</div>
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

	.ep-summary { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; margin-top: 10px; padding: 10px 12px; border: 1px solid var(--line); border-radius: 12px; background: color-mix(in srgb, var(--surface) 75%, transparent); }
	.ep-sum-copy { display: flex; flex-direction: column; gap: 6px; flex: 1 1 240px; min-width: 0; }
	.ep-sum-text { font: 600 12.5px/1.3 var(--font-ui); color: var(--text); font-variant-numeric: tabular-nums; }
	.ep-sum-bar { height: 4px; width: 100%; max-width: 320px; border-radius: 999px; background: color-mix(in srgb, var(--text) 14%, transparent); overflow: hidden; }
	.ep-sum-bar i { display: block; height: 100%; background: #22c55e; border-radius: 999px; transition: width var(--dur-fast) var(--ease-out); }
	.ep-mark-season { display: inline-flex; align-items: center; gap: 5px; }

	.ep-bar { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; margin: 10px 0 8px; }
	.ep-meta-line p { margin: 0; }
	.ep-status-chips { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
	.ep-chip { height: 28px; padding: 0 11px; border-radius: 999px; border: 1px solid var(--line); background: transparent; color: var(--text-muted); font: 600 12px/1 var(--font-ui); cursor: pointer; font-variant-numeric: tabular-nums; }
	.ep-chip.on { background: var(--text); color: var(--bg); border-color: var(--text); }
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
	.ep.upcoming { opacity: 0.72; }
	.ep.row { flex-direction: row; align-items: flex-start; gap: 14px; padding: 8px; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); }
	.ep.next-up { border-color: color-mix(in srgb, #e50914 68%, var(--line)); background: color-mix(in srgb, #e50914 7%, var(--surface)); }
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
	.ep-rel { position: absolute; left: 8px; top: 8px; z-index: 2; padding: 3px 7px; border-radius: 999px; background: rgb(15 15 18 / 82%); color: #fff; font: 700 10.5px/1 var(--font-ui); letter-spacing: 0.02em; backdrop-filter: blur(8px); }
	.ep-rel.today, .ep-rel.aired { background: rgb(22 163 74 / 90%); }
	.ep-rel.upcoming { background: rgb(59 130 246 / 88%); }
	.ep-play { position: absolute; right: 8px; bottom: 8px; z-index: 2; display: inline-grid; place-items: center; width: 32px; height: 32px; padding: 0; border: 1px solid rgb(255 255 255 / 24%); border-radius: 999px; background: rgb(15 15 18 / 78%); color: #fff; backdrop-filter: blur(8px); transition: transform var(--dur-fast) var(--ease-out), background var(--dur-fast) var(--ease-out); }
	.still-btn:hover .ep-play { background: #fff; color: #111; transform: scale(1.08); }
	.ep-done { position: absolute; top: 8px; right: 8px; z-index: 2; display: inline-grid; place-items: center; width: 22px; height: 22px; border-radius: 999px; background: rgb(34 197 94 / 90%); color: #fff; }
	.ep-prog { position: absolute; left: 0; right: 0; bottom: 0; z-index: 2; height: 4px; background: rgb(255 255 255 / 26%); }
	.ep-prog i { display: block; height: 100%; background: #e50914; }

	.ep-title-row { display: flex; align-items: center; gap: 8px; min-width: 0; }
	.ep-name, .ep-title-btn { font: 600 15px/1.3 var(--font-ui); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; min-width: 0; }
	.ep-title-btn { padding: 0; border: 0; background: none; color: inherit; text-align: left; cursor: pointer; }
	.ep-title-btn:hover { text-decoration: underline; }
	.ep-tag-next { flex: none; padding: 2px 8px; border-radius: 999px; background: rgb(229 9 20 / 16%); color: #f87171; font: 700 11px/1.3 var(--font-ui); }
	.ep-acts { flex: none; display: inline-flex; align-items: center; gap: 5px; }
	.ep-act-btn { display: inline-grid; place-items: center; width: 26px; height: 26px; border: 1px solid var(--line); border-radius: 999px; background: var(--surface-2); color: var(--text-muted); cursor: pointer; }
	.ep-act-btn:hover { color: var(--text); border-color: var(--text-faint); }
	.ep-act-btn.done { background: rgb(34 197 94 / 18%); border-color: rgb(34 197 94 / 55%); color: #4ade80; }
	.ep.row .ep-name, .ep.row .ep-title-btn { white-space: normal; display: -webkit-box; -webkit-line-clamp: 1; line-clamp: 1; -webkit-box-orient: vertical; }
	.ep-text { margin: 2px 0 0; color: var(--text-muted); font: 400 13.5px/1.4 var(--font-ui); display: -webkit-box; -webkit-line-clamp: 3; line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; }
	.ep-err { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-top: 8px; }
	.ep-err p { margin: 0; }
	.ep-sk { display: flex; gap: 12px; margin-top: 12px; }
	.ep-sk > :global(*) { flex: 1; }
</style>
