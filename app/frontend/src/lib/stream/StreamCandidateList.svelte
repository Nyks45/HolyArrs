<script lang="ts">
	import Icon from '#lib/ui/Icon.svelte';
	import { bytes } from '#lib/core/format.ts';
	import type { StreamCandidate, StreamSourceInfo } from './stream-api.ts';

	let {
		availableSources,
		selectedSourceIds,
		sourceTab = $bindable<'all' | 'instant' | 'local' | 'torrent'>('all'),
		resFilter = $bindable<'all' | '2160p' | '1080p' | '720p' | 'sd'>('all'),
		onlySeeded = $bindable(true),
		sortBy = $bindable<'seeders' | 'quality' | 'size_asc' | 'size_desc'>('seeders'),
		searching,
		searchError,
		candidates,
		counts,
		activeKey,
		mpvAvailable,
		onToggleAllSources,
		onToggleSource,
		onPlayCandidate,
		onPlayFastest
	}: {
		availableSources: StreamSourceInfo[];
		selectedSourceIds: number[];
		sourceTab: 'all' | 'instant' | 'local' | 'torrent';
		resFilter: 'all' | '2160p' | '1080p' | '720p' | 'sd';
		onlySeeded: boolean;
		sortBy: 'seeders' | 'quality' | 'size_asc' | 'size_desc';
		searching: boolean;
		searchError: string;
		candidates: StreamCandidate[];
		counts: { local: number; instant: number; debrid: number; torrent: number };
		activeKey: string;
		mpvAvailable: boolean;
		onToggleAllSources: () => void;
		onToggleSource: (id: number) => void;
		onPlayCandidate: (c: StreamCandidate, mode: 'browser' | 'mpv') => void;
		onPlayFastest: () => void;
	} = $props();

	const filtered = $derived.by(() => {
		const list = candidates.filter((c) => {
			const k = c.kind ?? 'torrent';
			if (sourceTab === 'instant' && !c.instant && k === 'torrent') return false;
			if (sourceTab === 'local' && k !== 'local') return false;
			if (sourceTab === 'torrent' && k !== 'torrent') return false;
			if (k === 'torrent' && onlySeeded && c.seeders <= 0) return false;
			const r = (c.quality || c.resolution || '').toLowerCase();
			if (resFilter === '2160p') return r.includes('2160') || c.resolution_num >= 2160;
			if (resFilter === '1080p') return r.includes('1080') || c.resolution_num === 1080;
			if (resFilter === '720p') return r.includes('720') || c.resolution_num === 720;
			if (resFilter === 'sd') return (c.resolution_num > 0 && c.resolution_num < 720) || r.includes('480');
			return true;
		});
		const copy = [...list];
		copy.sort((a, b) => {
			if (sourceTab === 'all') {
				const sb = b.score ?? 0;
				const sa = a.score ?? 0;
				if (sb !== sa) return sb - sa;
			}
			if (sortBy === 'quality') return (b.resolution_num ?? 0) - (a.resolution_num ?? 0) || b.seeders - a.seeders;
			if (sortBy === 'size_asc') return a.size - b.size;
			if (sortBy === 'size_desc') return b.size - a.size;
			return b.seeders - a.seeders || (b.resolution_num ?? 0) - (a.resolution_num ?? 0);
		});
		return copy;
	});

	function badgeFor(c: StreamCandidate): { cls: string; text: string } {
		if (c.kind === 'local') return { cls: 'local', text: 'Local' };
		if (c.kind === 'debrid_cached') return { cls: 'debrid', text: 'Debrid' };
		if (c.kind === 'http' || c.kind === 'hls') return { cls: 'instant', text: 'Instant' };
		return { cls: 'torrent', text: 'Torrent' };
	}

	function keyFor(c: StreamCandidate): string {
		return c.id || `${c.source_id}:${c.link}`;
	}
</script>

{#if availableSources.length > 0}
	<div class="ss-sources-bar">
		<button type="button" class="ss-chip" class:on={selectedSourceIds.length === 0} onclick={onToggleAllSources}>All Indexers</button>
		{#each availableSources as src (src.id)}
			<button type="button" class="ss-chip" class:on={selectedSourceIds.includes(src.id)} onclick={() => onToggleSource(src.id)}>{src.name}</button>
		{/each}
	</div>
{/if}

<div class="ss-toolbar">
	<div class="ss-tabs">
		<button type="button" class="ss-chip" class:on={sourceTab === 'all'} onclick={() => { sourceTab = 'all'; }}>All ({candidates.length})</button>
		<button type="button" class="ss-chip" class:on={sourceTab === 'instant'} onclick={() => { sourceTab = 'instant'; }}>Instant ({counts.instant})</button>
		{#if counts.local > 0}
			<button type="button" class="ss-chip" class:on={sourceTab === 'local'} onclick={() => { sourceTab = 'local'; }}>Local ({counts.local})</button>
		{/if}
		<button type="button" class="ss-chip" class:on={sourceTab === 'torrent'} onclick={() => { sourceTab = 'torrent'; }}>Torrents ({counts.torrent})</button>
	</div>

	<div class="ss-right-opts">
		{#if candidates.some((c) => c.instant)}
			<button type="button" class="ss-fast-btn" onclick={onPlayFastest}>
				<Icon name="play" size={14} />
				<span>Play Fastest</span>
			</button>
		{/if}
		<select bind:value={resFilter} aria-label="Filter resolution">
			<option value="all">Any quality</option>
			<option value="2160p">2160p</option>
			<option value="1080p">1080p</option>
			<option value="720p">720p</option>
			<option value="sd">SD</option>
		</select>
		<select bind:value={sortBy} aria-label="Sort results">
			<option value="seeders">Best / Seeds</option>
			<option value="quality">Resolution</option>
			<option value="size_desc">Largest</option>
			<option value="size_asc">Smallest</option>
		</select>
		<label class="ss-seeded"><input type="checkbox" bind:checked={onlySeeded} /><span>Seeded</span></label>
	</div>
</div>

{#if searchError}
	<p class="ss-err">{searchError}</p>
{:else if searching}
	<p class="ss-muted">Searching Local Library, Stremio addons, Nuvio plugins &amp; indexers...</p>
{:else if filtered.length === 0}
	<p class="ss-muted">No streams match the current filter.</p>
{:else}
	<div class="ss-cands">
		{#each filtered as c (keyFor(c))}
			{@const b = badgeFor(c)}
			<div class="ss-cand" class:active={activeKey === keyFor(c)}>
				<div class="ss-cand-main">
					<div class="ss-cand-title">{c.release_name || c.title}</div>
					<div class="ss-cand-meta">
						<span class="tag kind-{b.cls}">{b.text}</span>
						<span class="tag">{c.provider || c.source_name}</span>
						{#if c.quality || c.resolution}<span class="tag res">{c.quality || c.resolution}</span>{/if}
						{#if c.hdr}<span class="tag hdr">{c.hdr}</span>{/if}
						{#if c.codec}<span class="tag">{c.codec}</span>{/if}
						{#if c.size > 0}<span>{bytes(c.size)}</span>{/if}
						{#if (c.kind ?? 'torrent') === 'torrent'}<span>{c.seeders} seeds</span>{/if}
					</div>
				</div>
				<div class="ss-cand-acts">
					<button type="button" class="ss-btn primary" onclick={() => onPlayCandidate(c, 'browser')}>Play</button>
					{#if mpvAvailable}
						<button type="button" class="ss-btn" onclick={() => onPlayCandidate(c, 'mpv')}>MPV</button>
					{/if}
				</div>
			</div>
		{/each}
	</div>
{/if}

<style>
	.ss-sources-bar, .ss-tabs { display: flex; flex-wrap: wrap; gap: 6px; }
	.ss-toolbar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 10px; }
	.ss-right-opts { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
	.ss-right-opts select { height: 32px; padding: 0 8px; border-radius: 8px; border: 1px solid var(--border); background: var(--surface-2); color: var(--text); font-size: 12.5px; }
	.ss-seeded { display: inline-flex; align-items: center; gap: 5px; font-size: 12.5px; color: var(--muted); }
	.ss-chip { padding: 5px 10px; border-radius: 999px; border: 1px solid var(--border); background: var(--surface-2); color: var(--muted); font-size: 12px; font-weight: 600; cursor: pointer; }
	.ss-chip.on { background: var(--accent); border-color: var(--accent); color: #fff; }
	.ss-fast-btn { display: inline-flex; align-items: center; gap: 5px; height: 32px; padding: 0 12px; border-radius: 8px; border: 0; background: #16a34a; color: #fff; font-size: 12.5px; font-weight: 700; cursor: pointer; }
	.ss-cands { display: flex; flex-direction: column; gap: 8px; max-height: 44vh; overflow-y: auto; }
	.ss-cand { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 10px 12px; border-radius: 10px; border: 1px solid var(--border); background: var(--surface-2); }
	.ss-cand.active { border-color: var(--accent); }
	.ss-cand-main { min-width: 0; flex: 1; display: flex; flex-direction: column; gap: 5px; }
	.ss-cand-title { font-size: 13px; font-weight: 600; color: var(--text); word-break: break-word; }
	.ss-cand-meta { display: flex; flex-wrap: wrap; gap: 5px; font-size: 11px; color: var(--muted); }
	.tag { padding: 2px 6px; border-radius: 5px; background: var(--surface-3); color: var(--text); font-weight: 600; }
	.tag.kind-local { background: color-mix(in srgb, #22c55e 22%, transparent); color: #22c55e; }
	.tag.kind-debrid { background: color-mix(in srgb, #a855f7 22%, transparent); color: #a855f7; }
	.tag.kind-instant { background: color-mix(in srgb, #0ea5e9 22%, transparent); color: #0ea5e9; }
	.tag.kind-torrent { background: color-mix(in srgb, #f59e0b 22%, transparent); color: #d97706; }
	.tag.res { color: #22c55e; }
	.tag.hdr { color: #f59e0b; }
	.ss-cand-acts { display: flex; gap: 6px; flex: none; }
	.ss-btn { height: 32px; padding: 0 12px; border-radius: 8px; border: 1px solid var(--border); background: var(--surface-1); color: var(--text); font-size: 12px; font-weight: 600; cursor: pointer; }
	.ss-btn.primary { background: var(--accent); border-color: var(--accent); color: #fff; }
	.ss-muted { margin: 6px 0; color: var(--muted); font-size: 13px; }
	.ss-err { margin: 6px 0; color: #e5484d; font-size: 13px; }
</style>
