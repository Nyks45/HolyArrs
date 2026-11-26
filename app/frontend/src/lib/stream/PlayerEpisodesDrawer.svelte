<script lang="ts">
	import Icon from '#lib/ui/Icon.svelte';
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
</script>

{#if open}
	<aside class="wp-drawer" aria-label="Episodes drawer">
		<div class="wp-drawer-head">
			<h3>Episodes</h3>
			<div class="head-acts">
				<button type="button" class="wp-mini-btn" onclick={onShuffleEpisode}>
					<Icon name="sparkle" size={14} />
					<span>Shuffle</span>
				</button>
				<button type="button" class="wp-close" onclick={onClose}><Icon name="x" size={18} /></button>
			</div>
		</div>

		<div class="wp-drawer-body">
			{#if seasons.length > 0}
				<div class="wp-seasons">
					{#each seasons as s (s.number)}
						<button type="button" class="wp-chip" class:on={selectedSeason === s.number} onclick={() => onPickSeason(s.number)}>
							S{s.number}
						</button>
					{/each}
				</div>
			{/if}

			{#if loadingEpisodes}
				<p class="wp-muted">Loading episodes...</p>
			{:else if episodes.length === 0}
				<p class="wp-muted">No episode details available.</p>
			{:else}
				<div class="wp-ep-list">
					{#each episodes as ep (ep.number)}
						{@const sv = progressMap[`${selectedSeason}:${ep.number}`]}
						<button
							type="button"
							class="wp-ep-card"
							class:current={selectedSeason === currentSeason && ep.number === currentEpisode}
							onclick={() => onPickEpisode(selectedSeason, ep.number)}
						>
							<div class="wp-ep-thumb">
								{#if ep.still}
									<img src={sized(ep.still, 'poster')} alt="" />
								{/if}
								{#if sv && (sv.progress_pct ?? 0) > 0}
									<div class="wp-ep-prog"><i style:width="{Math.min(100, sv.progress_pct ?? 0)}%"></i></div>
								{/if}
							</div>
							<div class="wp-ep-meta">
								<b>E{ep.number} · {ep.name || `Episode ${ep.number}`}</b>
								{#if sv}
									<span class="wp-ep-saved">{sv.completed ? 'Watched' : `${Math.round(sv.progress_pct ?? 0)}% watched`}</span>
								{/if}
								{#if ep.overview}<p>{ep.overview}</p>{/if}
							</div>
						</button>
					{/each}
				</div>
			{/if}
		</div>
	</aside>
{/if}

<style>
	.wp-drawer {
		position: absolute; top: 0; right: 0; bottom: 0; width: min(440px, 100vw); z-index: 12;
		background: rgb(14 14 18 / 94%); border-left: 1px solid rgb(255 255 255 / 14%);
		backdrop-filter: blur(22px); display: flex; flex-direction: column;
	}
	.wp-drawer-head { display: flex; align-items: center; justify-content: space-between; padding: 16px 18px; border-bottom: 1px solid rgb(255 255 255 / 12%); }
	.wp-drawer-head h3 { margin: 0; font-size: 16px; font-weight: 700; }
	.head-acts { display: flex; align-items: center; gap: 8px; }
	.wp-close { width: 32px; height: 32px; border-radius: 50%; border: 0; background: rgb(255 255 255 / 12%); color: #fff; display: grid; place-items: center; cursor: pointer; }
	.wp-drawer-body { flex: 1; overflow-y: auto; padding: 14px 16px 28px; display: flex; flex-direction: column; gap: 12px; }
	.wp-seasons { display: flex; flex-wrap: wrap; gap: 6px; }
	.wp-chip { padding: 5px 10px; border: 1px solid rgb(255 255 255 / 18%); border-radius: 999px; background: transparent; color: rgb(255 255 255 / 78%); font-size: 12px; font-weight: 600; cursor: pointer; }
	.wp-chip.on { background: #fff; color: #111; border-color: #fff; }
	.wp-mini-btn { display: inline-flex; align-items: center; gap: 5px; padding: 6px 10px; border-radius: 8px; border: 1px solid rgb(255 255 255 / 20%); background: rgb(255 255 255 / 10%); color: #fff; font-size: 12px; font-weight: 600; cursor: pointer; }
	.wp-ep-list { display: flex; flex-direction: column; gap: 8px; }
	.wp-ep-card { display: flex; gap: 12px; padding: 8px; border: 1px solid rgb(255 255 255 / 12%); border-radius: 10px; background: rgb(255 255 255 / 4%); color: #fff; text-align: left; cursor: pointer; }
	.wp-ep-card.current { border-color: #e50914; background: rgb(229 9 20 / 14%); }
	.wp-ep-thumb { position: relative; width: 112px; aspect-ratio: 16 / 9; flex: none; border-radius: 7px; overflow: hidden; background: rgb(255 255 255 / 10%); }
	.wp-ep-thumb img { width: 100%; height: 100%; object-fit: cover; }
	.wp-ep-prog { position: absolute; left: 0; right: 0; bottom: 0; height: 3px; background: rgb(255 255 255 / 26%); }
	.wp-ep-prog i { display: block; height: 100%; background: #e50914; }
	.wp-ep-meta { min-width: 0; flex: 1; display: flex; flex-direction: column; gap: 3px; }
	.wp-ep-meta b { font-size: 13px; }
	.wp-ep-saved { color: #4ade80; font-size: 11px; font-weight: 600; }
	.wp-ep-meta p { margin: 0; color: rgb(255 255 255 / 66%); font-size: 12px; display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
	.wp-muted { margin: 0; color: rgb(255 255 255 / 66%); font-size: 13px; }
</style>
