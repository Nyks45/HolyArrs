<script lang="ts">
	import { tick } from 'svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import { dragScroll, wheelX } from '#lib/ui/drag-scroll.ts';
	import { sized } from '#lib/core/tmdb-image.ts';
	import type { WatchEntry } from './stream-api.ts';

	interface SeasonEp {
		number: number;
		name?: string;
		overview?: string;
		runtime?: number;
		still?: string;
		air_day?: string;
	}

	let {
		open,
		seasons,
		selectedSeason,
		currentSeason,
		currentEpisode,
		episodes,
		loadingEpisodes,
		progressMap,
		onClose,
		onPickSeason,
		onPickEpisode,
		onShuffleEpisode
	}: {
		open: boolean;
		seasons: { number: number; name?: string }[];
		selectedSeason: number;
		currentSeason: number;
		currentEpisode: number;
		episodes: SeasonEp[];
		loadingEpisodes: boolean;
		progressMap: Record<string, WatchEntry>;
		onClose: () => void;
		onPickSeason: (s: number) => void;
		onPickEpisode: (s: number, e: number) => void;
		onShuffleEpisode: () => void;
	} = $props();

	let epSearch = $state('');
	let railEl = $state<HTMLElement | undefined>(undefined);
	let atStart = $state(true);
	let atEnd = $state(true);

	const shownEpisodes = $derived.by(() => {
		const q = epSearch.trim().toLowerCase();
		if (!q) return episodes;
		return episodes.filter(
			(ep) =>
				String(ep.number) === q ||
				(ep.name ?? '').toLowerCase().includes(q) ||
				(ep.overview ?? '').toLowerCase().includes(q)
		);
	});

	function measureRail() {
		if (!railEl) return;
		atStart = railEl.scrollLeft <= 2;
		atEnd = railEl.scrollLeft + railEl.clientWidth >= railEl.scrollWidth - 2;
	}

	function pageRail(dir: -1 | 1) {
		if (!railEl) return;
		const step = Math.max(240, railEl.clientWidth * 0.85);
		railEl.scrollBy({ left: dir * step, behavior: 'smooth' });
	}

	$effect(() => {
		if (!open || !railEl) return;
		void shownEpisodes.length;
		measureRail();
		const ro = new ResizeObserver(measureRail);
		ro.observe(railEl);
		const mo = new MutationObserver(measureRail);
		mo.observe(railEl, { childList: true });
		return () => {
			ro.disconnect();
			mo.disconnect();
		};
	});

	$effect(() => {
		if (!open) return;
		void selectedSeason;
		void currentEpisode;
		void shownEpisodes.length;
		void tick().then(() => {
			const cur = railEl?.querySelector<HTMLElement>('.wp-ep-card.current');
			cur?.scrollIntoView({ inline: 'center', block: 'nearest', behavior: 'smooth' });
			measureRail();
		});
	});
</script>

{#if open}
	<aside class="wp-ep-tray" aria-label="Episodes tray">
		<div class="wp-tray-head">
			<div class="wp-tray-left" use:wheelX>
				{#if seasons.length > 0}
					<div class="wp-seasons">
						{#each seasons as s (s.number)}
							<button type="button" class="wp-chip" class:on={selectedSeason === s.number} onclick={() => onPickSeason(s.number)}>
								Season {s.number}
							</button>
						{/each}
					</div>
				{/if}
			</div>
			<div class="head-acts">
				<label class="wp-ep-search">
					<Icon name="search" size={14} />
					<input type="search" bind:value={epSearch} placeholder="Find episode..." aria-label="Find episode" />
				</label>
				<button type="button" class="wp-mini-btn" onclick={onShuffleEpisode}>
					<Icon name="sparkle" size={14} />
					<span>Shuffle</span>
				</button>
				<button type="button" class="wp-close" onclick={onClose}><Icon name="x" size={16} /></button>
			</div>
		</div>

		<div class="wp-tray-body">
			{#if loadingEpisodes}
				<p class="wp-muted">Loading episodes...</p>
			{:else if shownEpisodes.length === 0}
				<p class="wp-muted">No matching episodes in Season {selectedSeason}.</p>
			{:else}
				<div class="wp-rail-wrap">
					{#if !atStart}
						<button type="button" class="wp-rail-arrow prev" aria-label="Scroll episodes left" onclick={() => pageRail(-1)}>
							<Icon name="chevronLeft" size={18} />
						</button>
					{/if}
					<div class="wp-ep-rail" bind:this={railEl} use:dragScroll use:wheelX onscroll={measureRail}>
						{#each shownEpisodes as ep (ep.number)}
							{@const sv = progressMap[`${selectedSeason}:${ep.number}`]}
							<button
								type="button"
								class="wp-ep-card"
								class:current={selectedSeason === currentSeason && ep.number === currentEpisode}
								onclick={() => onPickEpisode(selectedSeason, ep.number)}
							>
								<div class="wp-ep-thumb">
									{#if ep.still}<img src={sized(ep.still, 'poster')} alt="" draggable="false" />{/if}
									<span class="wp-ep-num">E{ep.number}</span>
									{#if sv?.completed}
										<span class="wp-ep-done" title="Watched"><Icon name="check" size={11} /></span>
									{/if}
									{#if sv && (sv.progress_pct ?? 0) > 0}
										<div class="wp-ep-prog"><i style:width="{Math.min(100, sv.progress_pct ?? 0)}%"></i></div>
									{/if}
								</div>
								<div class="wp-ep-meta">
									<b>{ep.name || `Episode ${ep.number}`}</b>
									{#if ep.runtime || sv}
										<span class="wp-ep-sub">
											{ep.runtime ? `${ep.runtime}m` : ''}{ep.runtime && sv ? ' · ' : ''}{sv ? (sv.completed ? 'Watched' : `${Math.round(sv.progress_pct ?? 0)}%`) : ''}
										</span>
									{/if}
									{#if ep.overview}<p>{ep.overview}</p>{/if}
								</div>
							</button>
						{/each}
					</div>
					{#if !atEnd}
						<button type="button" class="wp-rail-arrow next" aria-label="Scroll episodes right" onclick={() => pageRail(1)}>
							<Icon name="chevronRight" size={18} />
						</button>
					{/if}
				</div>
			{/if}
		</div>
	</aside>
{/if}

<style>
	.wp-ep-tray {
		position: absolute; left: 0; right: 0; bottom: 0; max-height: min(46vh, 380px); z-index: 14;
		background: rgb(14 14 20 / 95%); border-top: 1px solid rgb(255 255 255 / 14%);
		border-radius: 18px 18px 0 0; backdrop-filter: blur(24px);
		display: flex; flex-direction: column; box-shadow: 0 -16px 48px rgb(0 0 0 / 65%);
	}
	.wp-tray-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; padding: 12px 20px; border-bottom: 1px solid rgb(255 255 255 / 10%); }
	.wp-tray-left { display: flex; align-items: center; gap: 8px; flex: 1 1 220px; min-width: 0; overflow-x: auto; scrollbar-width: thin; scrollbar-color: rgb(255 255 255 / 22%) transparent; padding-bottom: 2px; }
	.wp-seasons { display: flex; gap: 6px; }
	.wp-chip { flex: none; padding: 5px 11px; border: 1px solid rgb(255 255 255 / 16%); border-radius: 999px; background: transparent; color: rgb(255 255 255 / 78%); font-size: 12px; font-weight: 600; cursor: pointer; }
	.wp-chip.on { background: #fff; color: #111; border-color: #fff; }
	.head-acts { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
	.wp-ep-search { position: relative; display: flex; align-items: center; }
	.wp-ep-search > :global(.icon) { position: absolute; left: 9px; color: rgb(255 255 255 / 55%); pointer-events: none; }
	.wp-ep-search input { height: 30px; width: 150px; padding: 0 10px 0 28px; border-radius: 999px; border: 1px solid rgb(255 255 255 / 18%); background: rgb(255 255 255 / 8%); color: #fff; font-size: 12px; }
	.wp-mini-btn { display: inline-flex; align-items: center; gap: 5px; height: 30px; padding: 0 10px; border-radius: 999px; border: 1px solid rgb(255 255 255 / 18%); background: rgb(255 255 255 / 10%); color: #fff; font-size: 12px; font-weight: 600; cursor: pointer; }
	.wp-close { width: 30px; height: 30px; border-radius: 50%; border: 0; background: rgb(255 255 255 / 12%); color: #fff; display: grid; place-items: center; cursor: pointer; }

	.wp-tray-body { flex: 1; min-height: 0; overflow: hidden; padding: 14px 20px 16px; display: flex; flex-direction: column; }
	.wp-rail-wrap { position: relative; flex: 1; min-width: 0; min-height: 0; display: flex; align-items: stretch; }
	.wp-ep-rail { flex: 1; min-width: 0; display: flex; gap: 12px; overflow-x: auto; overflow-y: hidden; padding-bottom: 8px; scroll-snap-type: x proximity; scrollbar-width: thin; scrollbar-color: rgb(255 255 255 / 28%) transparent; }
	.wp-ep-rail::-webkit-scrollbar { height: 6px; }
	.wp-ep-rail::-webkit-scrollbar-thumb { background: rgb(255 255 255 / 28%); border-radius: 999px; }
	.wp-rail-arrow {
		position: absolute; top: 50%; transform: translateY(-50%); z-index: 5;
		width: 34px; height: 34px; border-radius: 50%;
		border: 1px solid rgb(255 255 255 / 24%); background: rgb(14 14 20 / 88%);
		color: #fff; display: grid; place-items: center; cursor: pointer;
		box-shadow: 0 6px 18px rgb(0 0 0 / 55%); backdrop-filter: blur(8px);
		transition: background 140ms ease, transform 140ms ease;
	}
	.wp-rail-arrow:hover { background: rgb(255 255 255 / 22%); }
	.wp-rail-arrow.prev { left: 4px; }
	.wp-rail-arrow.next { right: 4px; }
	.wp-ep-card { flex: none; width: 224px; scroll-snap-align: start; display: flex; flex-direction: column; gap: 8px; padding: 8px; border: 1px solid rgb(255 255 255 / 10%); border-radius: 12px; background: rgb(255 255 255 / 4%); color: #fff; text-align: left; cursor: pointer; transition: border-color 140ms ease, background 140ms ease, transform 140ms ease; }
	.wp-ep-card:hover { background: rgb(255 255 255 / 9%); border-color: rgb(255 255 255 / 26%); transform: translateY(-2px); }
	.wp-ep-card.current { border-color: #e50914; background: rgb(229 9 20 / 16%); }
	.wp-ep-thumb { position: relative; width: 100%; aspect-ratio: 16 / 9; border-radius: 8px; overflow: hidden; background: rgb(255 255 255 / 10%); }
	.wp-ep-thumb img { width: 100%; height: 100%; object-fit: cover; user-select: none; -webkit-user-drag: none; }
	.wp-ep-num { position: absolute; left: 7px; top: 7px; padding: 2px 6px; border-radius: 5px; background: rgb(0 0 0 / 72%); font-size: 11px; font-weight: 700; }
	.wp-ep-done { position: absolute; right: 7px; top: 7px; width: 20px; height: 20px; border-radius: 50%; background: rgb(34 197 94 / 92%); color: #fff; display: grid; place-items: center; }
	.wp-ep-prog { position: absolute; left: 0; right: 0; bottom: 0; height: 3px; background: rgb(255 255 255 / 26%); }
	.wp-ep-prog i { display: block; height: 100%; background: #e50914; }
	.wp-ep-meta { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
	.wp-ep-meta b { font-size: 13px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
	.wp-ep-sub { color: #86efac; font-size: 11px; font-weight: 600; }
	.wp-ep-meta p { margin: 0; color: rgb(255 255 255 / 64%); font-size: 11.5px; display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
	.wp-muted { margin: 0; color: rgb(255 255 255 / 66%); font-size: 13px; }
</style>
