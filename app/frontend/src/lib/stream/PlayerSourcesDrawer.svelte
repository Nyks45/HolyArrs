<script lang="ts">
	import Icon from '#lib/ui/Icon.svelte';
	import { bytes } from '#lib/core/format.ts';
	import { candidateKey, matchesQualityFilter, sortCandidatesForQuality } from './player-media.ts';
	import type { SessionStatus, StreamCandidate } from './stream-api.ts';

	let {
		open,
		queryText = $bindable(''),
		sourceTab = $bindable<'all' | 'instant' | 'local' | 'debrid' | 'torrent'>('all'),
		resFilter = $bindable<'all' | '2160p' | '1080p' | '720p' | 'sd'>('all'),
		searching,
		loadingMoreSources = false,
		searchError,
		fallbackNotice = '',
		candidates,
		counts,
		activeCandidateKey,
		session,
		mpvAvailable,
		onClose,
		onSearch,
		onStartCandidate,
		onSelectFile,
		onToggleKeep
	}: {
		open: boolean;
		queryText: string;
		sourceTab: 'all' | 'instant' | 'local' | 'debrid' | 'torrent';
		resFilter: 'all' | '2160p' | '1080p' | '720p' | 'sd';
		searching: boolean;
		loadingMoreSources?: boolean;
		searchError: string;
		fallbackNotice?: string;
		candidates: StreamCandidate[];
		counts: { local: number; instant: number; debrid: number; torrent: number };
		activeCandidateKey: string;
		session: SessionStatus | null;
		mpvAvailable: boolean;
		onClose: () => void;
		onSearch: () => void;
		onStartCandidate: (c: StreamCandidate, mode: 'browser' | 'mpv') => void;
		onSelectFile: (idx: number) => void;
		onToggleKeep: () => void;
	} = $props();

	const visibleList = $derived.by(() => {
		const tabFiltered = candidates.filter((c) => {
			const k = c.kind ?? 'torrent';
			if (sourceTab === 'instant' && !c.instant && k === 'torrent') return false;
			if (sourceTab === 'local' && k !== 'local') return false;
			if (sourceTab === 'debrid' && k !== 'debrid_cached') return false;
			if (sourceTab === 'torrent' && k !== 'torrent') return false;
			return resFilter === 'all' || matchesQualityFilter(c, resFilter);
		});
		return sortCandidatesForQuality(tabFiltered, resFilter);
	});

	function badgeFor(c: StreamCandidate): { cls: string; text: string } {
		switch (c.kind) {
			case 'local':
				return { cls: 'local', text: 'Local' };
			case 'debrid_cached':
				return { cls: 'debrid', text: 'Debrid' };
			case 'http':
			case 'hls':
				return { cls: 'instant', text: c.fast_start ? 'Native' : 'Instant' };
			default:
				return { cls: 'torrent', text: 'Torrent' };
		}
	}
</script>

{#if open}
	<aside class="wp-pop" aria-label="Sources menu">
		<div class="wp-pop-head">
			<div class="wp-pop-title">
				<h3>Sources</h3>
				{#if loadingMoreSources}
					<span class="wp-loading-tag">Finding more...</span>
				{/if}
			</div>
			<button type="button" class="wp-close" onclick={onClose}><Icon name="x" size={16} /></button>
		</div>

		<div class="wp-pop-body">
			{#if fallbackNotice}
				<div class="wp-fallback-banner">{fallbackNotice}</div>
			{/if}

			{#if session && ((session.files?.length ?? 0) > 1 || session.transport === 'torrent' || !session.transport)}
				<div class="wp-box">
					{#if (session.files?.length ?? 0) > 1}
						<label class="wp-label">
							<span>Release File</span>
							<select value={String(session.selected_file)} onchange={(e) => onSelectFile(Number((e.currentTarget as HTMLSelectElement).value))}>
								{#each session.files as f (f.index)}
									<option value={String(f.index)}>{f.name} ({bytes(f.size)})</option>
								{/each}
							</select>
						</label>
					{/if}
					{#if session.transport === 'torrent' || !session.transport}
						<button type="button" class="wp-mini-btn" onclick={onToggleKeep}>
							{session.keep_file ? 'Keeping download in Library' : 'Keep downloaded file after watching'}
						</button>
					{/if}
				</div>
			{/if}

			<form class="wp-search-row" onsubmit={(e) => { e.preventDefault(); onSearch(); }}>
				<input type="text" bind:value={queryText} placeholder="Custom search query..." />
				<button type="submit" class="wp-mini-btn accent" disabled={searching}>{searching ? '...' : 'Refresh'}</button>
			</form>

			<div class="wp-tab-row">
				<button type="button" class="wp-chip" class:on={sourceTab === 'all'} onclick={() => { sourceTab = 'all'; }}>All ({candidates.length})</button>
				<button type="button" class="wp-chip" class:on={sourceTab === 'instant'} onclick={() => { sourceTab = 'instant'; }}>Instant ({counts.instant})</button>
				{#if counts.local > 0}<button type="button" class="wp-chip" class:on={sourceTab === 'local'} onclick={() => { sourceTab = 'local'; }}>Local ({counts.local})</button>{/if}
				{#if counts.debrid > 0}<button type="button" class="wp-chip" class:on={sourceTab === 'debrid'} onclick={() => { sourceTab = 'debrid'; }}>Debrid ({counts.debrid})</button>{/if}
				<button type="button" class="wp-chip" class:on={sourceTab === 'torrent'} onclick={() => { sourceTab = 'torrent'; }}>Torrents ({counts.torrent})</button>
			</div>

			<div class="wp-tab-row">
				{#each ['all', '2160p', '1080p', '720p', 'sd'] as r (r)}
					<button type="button" class="wp-chip" class:on={resFilter === r} onclick={() => { resFilter = r as typeof resFilter; }}>
						{r === 'all' ? 'Auto' : r.toUpperCase()}
					</button>
				{/each}
			</div>

			{#if searchError}<div class="wp-err-inline">{searchError}</div>{/if}

			{#if searching && candidates.length === 0}
				<p class="wp-muted">Checking fast instant sources...</p>
			{:else if visibleList.length === 0}
				<p class="wp-muted">No verified sources found for this filter.</p>
			{:else}
				<div class="wp-cand-list">
					{#each visibleList as c (candidateKey(c))}
						{@const b = badgeFor(c)}
						<article class="wp-cand" class:current={activeCandidateKey === candidateKey(c)}>
							<div class="wp-cand-info">
								<div class="wp-cand-name">{c.release_name || c.title}</div>
								<div class="wp-cand-meta">
									<span class="tag kind-{b.cls}">{b.text}</span>
									<span class="tag src">{c.provider || c.source_name}</span>
									{#if c.quality || c.resolution}<span class="tag res">{c.quality || c.resolution}</span>{/if}
									{#if c.probe_ms}<span class="tag ms">{c.probe_ms}ms</span>{/if}
									{#if c.hdr}<span class="tag hdr">{c.hdr}</span>{/if}
									{#if c.size > 0}<span>{bytes(c.size)}</span>{/if}
									{#if (c.kind ?? 'torrent') === 'torrent'}<span>{c.seeders} seeds</span>{/if}
								</div>
							</div>
							<div class="wp-cand-acts">
								<button type="button" class="wp-mini-btn accent" onclick={() => onStartCandidate(c, 'browser')}>Play</button>
								{#if mpvAvailable}<button type="button" class="wp-mini-btn" onclick={() => onStartCandidate(c, 'mpv')}>MPV</button>{/if}
							</div>
						</article>
					{/each}
				</div>
			{/if}
		</div>
	</aside>
{/if}

<style>
	.wp-pop { position: absolute; top: 62px; right: 24px; width: min(420px, calc(100vw - 32px)); max-height: min(74vh, 620px); z-index: 14; background: rgb(16 16 22 / 94%); border: 1px solid rgb(255 255 255 / 14%); border-radius: 16px; backdrop-filter: blur(22px); display: flex; flex-direction: column; box-shadow: 0 20px 50px rgb(0 0 0 / 60%); }
	.wp-pop-head { display: flex; align-items: center; justify-content: space-between; padding: 13px 16px; border-bottom: 1px solid rgb(255 255 255 / 10%); }
	.wp-pop-title { display: flex; align-items: center; gap: 8px; }
	.wp-pop-head h3 { margin: 0; font-size: 15px; font-weight: 700; }
	.wp-loading-tag { font-size: 11px; font-weight: 600; color: #7dd3fc; }
	.wp-close { width: 28px; height: 28px; border-radius: 50%; border: 0; background: rgb(255 255 255 / 12%); color: #fff; display: grid; place-items: center; cursor: pointer; }
	.wp-pop-body { flex: 1; overflow-y: auto; padding: 12px 14px 16px; display: flex; flex-direction: column; gap: 10px; }
	.wp-fallback-banner { padding: 7px 10px; border-radius: 8px; background: rgb(245 158 11 / 20%); color: #fcd34d; font-size: 12px; }
	.wp-box { padding: 10px; border-radius: 10px; background: rgb(255 255 255 / 5%); display: flex; flex-direction: column; gap: 8px; }
	.wp-label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: rgb(255 255 255 / 72%); }
	.wp-label select, .wp-search-row input { height: 34px; padding: 0 10px; border-radius: 8px; border: 1px solid rgb(255 255 255 / 20%); background: rgb(28 28 34); color: #fff; font-size: 12.5px; }
	.wp-search-row { display: flex; gap: 8px; }
	.wp-search-row input { flex: 1; }
	.wp-tab-row { display: flex; flex-wrap: wrap; gap: 5px; }
	.wp-chip { padding: 4px 9px; border: 1px solid rgb(255 255 255 / 16%); border-radius: 999px; background: transparent; color: rgb(255 255 255 / 76%); font-size: 11.5px; font-weight: 600; cursor: pointer; }
	.wp-chip.on { background: #fff; color: #111; border-color: #fff; }
	.wp-cand-list { display: flex; flex-direction: column; gap: 7px; }
	.wp-cand { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 9px 11px; border: 1px solid rgb(255 255 255 / 10%); border-radius: 10px; background: rgb(255 255 255 / 4%); }
	.wp-cand.current { border-color: #e50914; background: rgb(229 9 20 / 16%); }
	.wp-cand-info { min-width: 0; flex: 1; display: flex; flex-direction: column; gap: 4px; }
	.wp-cand-name { font-size: 12.5px; font-weight: 600; word-break: break-word; display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
	.wp-cand-meta { display: flex; flex-wrap: wrap; gap: 4px; font-size: 11px; color: rgb(255 255 255 / 72%); }
	.tag { padding: 2px 6px; border-radius: 5px; background: rgb(255 255 255 / 12%); color: #fff; font-weight: 600; }
	.tag.kind-local { background: rgb(34 197 94 / 26%); color: #86efac; }
	.tag.kind-debrid { background: rgb(168 85 247 / 28%); color: #e9d5ff; }
	.tag.kind-instant { background: rgb(56 189 248 / 26%); color: #bae6fd; }
	.tag.kind-torrent { background: rgb(245 158 11 / 22%); color: #fde68a; }
	.tag.res { background: rgb(34 197 94 / 22%); }
	.tag.ms { background: rgb(34 197 94 / 16%); color: #86efac; }
	.tag.hdr { background: rgb(245 158 11 / 22%); }
	.wp-cand-acts { display: flex; gap: 5px; flex: none; }
	.wp-mini-btn { padding: 6px 10px; border-radius: 8px; border: 1px solid rgb(255 255 255 / 20%); background: rgb(255 255 255 / 10%); color: #fff; font-size: 11.5px; font-weight: 600; cursor: pointer; }
	.wp-mini-btn.accent { background: #e50914; border-color: #e50914; }
	.wp-muted { margin: 0; color: rgb(255 255 255 / 66%); font-size: 12.5px; }
	.wp-err-inline { padding: 7px 10px; border-radius: 8px; background: rgb(239 68 68 / 20%); color: #fca5a5; font-size: 12px; }
</style>
