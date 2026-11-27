<script lang="ts">
	import { onMount } from 'svelte';
	import Shelf from '#lib/ui/Shelf.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import { to } from '#lib/core/paths.ts';
	import { ago } from '#lib/core/format.ts';
	import { sized } from '#lib/core/tmdb-image.ts';
	import { detailPath } from '#lib/metadata/index.ts';
	import {
		deleteWatchHistory,
		formatDuration,
		formatTimeLeft,
		listWatchHistory,
		type WatchEntry
	} from './stream-api.ts';
	import { prefetchStreamSources } from './player-media.ts';

	let { from = '/', kind = '' }: { from?: string; kind?: 'movie' | 'show' | '' } = $props();

	let items = $state<WatchEntry[]>([]);
	let loaded = $state(false);
	let hoverPrefetchTimer: ReturnType<typeof setTimeout> | null = null;

	const visibleItems = $derived(kind ? items.filter((e) => e.kind === kind) : items);

	function clearHoverPrefetch() {
		if (hoverPrefetchTimer) {
			clearTimeout(hoverPrefetchTimer);
			hoverPrefetchTimer = null;
		}
	}

	function scheduleHoverPrefetch(entry: WatchEntry) {
		const tmdb = Number(entry.tmdb) || 0;
		if (!tmdb && !entry.imdb && !entry.title) return;
		clearHoverPrefetch();
		hoverPrefetchTimer = setTimeout(() => {
			hoverPrefetchTimer = null;
			prefetchStreamSources({
				query: entry.title,
				kind: entry.kind,
				year: entry.year || 0,
				imdb: entry.imdb ?? '',
				tmdb,
				season: entry.kind === 'show' ? (entry.season || 1) : 0,
				episode: entry.kind === 'show' ? (entry.episode || 1) : 0
			});
		}, 350);
	}

	function watchHref(e: WatchEntry): string {
		const base = `/watch/${e.kind}/${encodeURIComponent(e.tmdb)}`;
		const qs = new URLSearchParams();
		if (e.kind === 'show') {
			if (e.season) qs.set('s', String(e.season));
			if (e.episode) qs.set('e', String(e.episode));
		}
		qs.set('resume', e.completed ? '0' : '1');
		if (from) qs.set('from', from);
		return to(`${base}?${qs.toString()}`);
	}

	async function removeEntry(e: WatchEntry, ev: MouseEvent) {
		ev.preventDefault();
		ev.stopPropagation();
		if (!e.key) return;
		items = items.filter((x) => x.key !== e.key);
		await deleteWatchHistory(e.key).catch(() => {});
	}

	onMount(() => {
		void listWatchHistory(24)
			.then((r) => {
				items = r.continue_watching ?? [];
				loaded = true;
			})
			.catch(() => {
				loaded = true;
			});
		return () => clearHoverPrefetch();
	});
</script>

{#if loaded && visibleItems.length > 0}
	<div class="cw-row" data-testid="continue-watching">
		<Shelf title="Continue Watching" href={to('/watch/history')} item="var(--title-item, 270px)">
			{#each visibleItems as entry (entry.key ?? `${entry.kind}:${entry.tmdb}`)}
				{@const art = entry.backdrop || entry.poster}
				<article
					class="cw-card"
					onmouseenter={() => scheduleHoverPrefetch(entry)}
					onmouseleave={clearHoverPrefetch}
				>
					<a class="cw-media" href={watchHref(entry)}>
						{#if art}
							<img src={sized(art, 'backdrop', 480)} alt="" loading="lazy" decoding="async" />
						{:else}
							<span class="cw-ph"><Icon name={entry.kind === 'show' ? 'tv' : 'film'} size={28} /></span>
						{/if}
						<span class="cw-scrim"></span>
						<span class="cw-play-badge" aria-hidden="true">
							<Icon name="play" size={18} />
						</span>
						{#if (entry.progress_pct ?? 0) > 0}
							<span class="cw-prog"><i style="width: {Math.min(100, entry.progress_pct ?? 0)}%"></i></span>
						{/if}
					</a>

					<div class="cw-tools">
						<a
							class="cw-tool-btn"
							href={to(detailPath(entry.kind, entry.tmdb))}
							aria-label={`Details for ${entry.title}`}
							title="Title details"
						>
							<Icon name="info" size={14} />
						</a>
						{#if entry.key}
							<button
								type="button"
								class="cw-tool-btn"
								aria-label={`Remove ${entry.title} from continue watching`}
								title="Remove"
								onclick={(ev) => void removeEntry(entry, ev)}
							>
								<Icon name="x" size={14} />
							</button>
						{/if}
					</div>

					<a class="cw-meta" href={watchHref(entry)}>
						<b class="cw-title" dir="auto">{entry.title}</b>
						<span class="cw-sub">
							{#if entry.kind === 'show' && entry.season}
								<span>S{String(entry.season).padStart(2, '0')} E{String(entry.episode ?? 1).padStart(2, '0')}</span>
								<span>·</span>
							{/if}
							{#if entry.completed}
								<span>Finished</span>
							{:else}
								<span>{formatDuration(entry.position_sec)}</span>
								{#if entry.duration_sec > 0}
									<span>· {formatTimeLeft(entry.position_sec, entry.duration_sec)}</span>
								{/if}
							{/if}
						</span>
						<span class="cw-foot">
							{#if entry.resolution || entry.source_name}
								<span>{[entry.resolution, entry.source_name].filter(Boolean).join(' · ')}</span>
							{/if}
							{#if entry.updated_at}
								<span>{ago(entry.updated_at)}</span>
							{/if}
						</span>
					</a>
				</article>
			{/each}
		</Shelf>
	</div>
{/if}

<style>
	.cw-row { display: contents; }
	.cw-card { position: relative; display: flex; flex-direction: column; gap: 7px; min-width: 0; }
	.cw-media {
		position: relative; display: block; aspect-ratio: 16 / 9; border-radius: 12px;
		overflow: hidden; background: var(--surface-2); color: var(--text-muted);
	}
	.cw-media img { width: 100%; height: 100%; object-fit: cover; transition: transform var(--dur) var(--ease-out); }
	.cw-card:hover .cw-media img { transform: scale(1.03); }
	.cw-ph { position: absolute; inset: 0; display: grid; place-items: center; }
	.cw-scrim { position: absolute; inset: 0; background: linear-gradient(to top, rgb(0 0 0 / 68%) 0%, transparent 45%); }
	.cw-play-badge {
		position: absolute; left: 10px; bottom: 12px; display: inline-grid; place-items: center;
		width: 34px; height: 34px; border-radius: 999px; background: rgb(255 255 255 / 92%); color: #111;
		box-shadow: 0 4px 14px rgb(0 0 0 / 40%); transition: transform var(--dur-fast) var(--ease-out);
	}
	.cw-card:hover .cw-play-badge { transform: scale(1.08); }
	.cw-prog { position: absolute; left: 0; right: 0; bottom: 0; height: 4px; background: rgb(255 255 255 / 26%); }
	.cw-prog i { display: block; height: 100%; background: #e50914; }

	.cw-tools {
		position: absolute; top: 8px; right: 8px; z-index: 2; display: flex; gap: 5px;
		opacity: 0; transition: opacity var(--dur-fast) var(--ease-out);
	}
	.cw-card:hover .cw-tools, .cw-card:focus-within .cw-tools { opacity: 1; }
	.cw-tool-btn {
		display: inline-grid; place-items: center; width: 26px; height: 26px; border: 0; border-radius: 999px;
		background: rgb(15 15 18 / 78%); color: #fff; cursor: pointer; text-decoration: none; backdrop-filter: blur(8px);
	}
	.cw-tool-btn:hover { background: #fff; color: #111; }

	.cw-meta { display: flex; flex-direction: column; gap: 2px; min-width: 0; text-decoration: none; color: inherit; }
	.cw-title { font: 600 14.5px/1.25 var(--font-ui); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
	.cw-sub { display: flex; flex-wrap: wrap; gap: 4px; font: 500 12.5px/1.25 var(--font-ui); color: var(--text); font-variant-numeric: tabular-nums; }
	.cw-foot { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 6px; font: 500 11.5px/1.2 var(--font-ui); color: var(--text-muted); }

	@media (hover: none) { .cw-tools { opacity: 1; } }
</style>
