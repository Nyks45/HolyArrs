<script lang="ts">
	import Icon from '#lib/ui/Icon.svelte';
	import { bytes } from '#lib/core/format.ts';
	import type { SessionStatus, StreamCandidate } from './stream-api.ts';

	let {
		open,
		queryText = $bindable(''),
		sourceTab = $bindable<'all' | 'instant' | 'local' | 'debrid' | 'torrent'>('all'),
		resFilter = $bindable<'all' | '2160p' | '1080p' | '720p' | 'sd'>('all'),
		searching,
		searchError,
		fallbackNotice = '',
		candidates,
		counts,
		activeCandidateKey,
		session,
		ffmpegAvailable,
		mpvAvailable,
		browserQuality,
		audioIdx,
		onClose,
		onSearch,
		onStartCandidate,
		onSelectFile,
		onToggleKeep,
		onChangeQuality,
		onChangeAudio
	}: {
		open: boolean;
		queryText: string;
		sourceTab: 'all' | 'instant' | 'local' | 'debrid' | 'torrent';
		resFilter: 'all' | '2160p' | '1080p' | '720p' | 'sd';
		searching: boolean;
		searchError: string;
		fallbackNotice?: string;
		candidates: StreamCandidate[];
		counts: { local: number; instant: number; debrid: number; torrent: number };
		activeCandidateKey: string;
		session: SessionStatus | null;
		ffmpegAvailable: boolean;
		mpvAvailable: boolean;
		browserQuality: 'original' | '1080p' | '720p' | '480p';
		audioIdx: number;
		onClose: () => void;
		onSearch: () => void;
		onStartCandidate: (c: StreamCandidate, mode: 'browser' | 'mpv') => void;
		onSelectFile: (idx: number) => void;
		onToggleKeep: () => void;
		onChangeQuality: (q: 'original' | '1080p' | '720p' | '480p') => void;
		onChangeAudio: (idx: number) => void;
	} = $props();

	const visibleList = $derived.by(() => {
		return candidates.filter((c) => {
			const k = c.kind ?? 'torrent';
			if (sourceTab === 'instant' && !c.instant && k === 'torrent') return false;
			if (sourceTab === 'local' && k !== 'local') return false;
			if (sourceTab === 'debrid' && k !== 'debrid_cached') return false;
			if (sourceTab === 'torrent' && k !== 'torrent') return false;

			const res = (c.quality || c.resolution || '').toLowerCase();
			if (resFilter === '2160p') return res.includes('2160') || c.resolution_num >= 2160;
			if (resFilter === '1080p') return res.includes('1080') || c.resolution_num === 1080;
			if (resFilter === '720p') return res.includes('720') || c.resolution_num === 720;
			if (resFilter === 'sd') return (c.resolution_num > 0 && c.resolution_num < 720) || res.includes('480');
			return true;
		});
	});

	function badgeFor(c: StreamCandidate): { cls: string; text: string } {
		switch (c.kind) {
			case 'local':
				return { cls: 'local', text: 'Local' };
			case 'debrid_cached':
				return { cls: 'debrid', text: 'Debrid' };
			case 'http':
			case 'hls':
				return { cls: 'instant', text: 'Instant' };
			default:
				return { cls: 'torrent', text: 'Torrent' };
		}
	}

	function candIdKey(c: StreamCandidate): string {
		return c.id || `${c.source_id}:${c.link}`;
	}
</script>

{#if open}
	<aside class="wp-drawer" aria-label="Sources drawer">
		<div class="wp-drawer-head">
			<h3>Sources</h3>
			<button type="button" class="wp-close" onclick={onClose}><Icon name="x" size={18} /></button>
		</div>

		<div class="wp-drawer-body">
			{#if fallbackNotice}
				<div class="wp-fallback-banner">{fallbackNotice}</div>
			{/if}

			{#if session}
				<div class="wp-box">
					<div class="wp-box-title">Active Playback Options</div>
					<div class="wp-grid-2">
						<label class="wp-label">
							<span>Video Quality</span>
							<select
								value={browserQuality}
								disabled={session.transport === 'hls_remote'}
								onchange={(e) => onChangeQuality((e.currentTarget as HTMLSelectElement).value as 'original' | '1080p' | '720p' | '480p')}
							>
								<option value="original">Original</option>
								{#if ffmpegAvailable}
									<option value="1080p">1080p Transcode</option>
									<option value="720p">720p Transcode</option>
									<option value="480p">480p Transcode</option>
								{/if}
							</select>
						</label>
						<label class="wp-label">
							<span>Audio Track</span>
							<select
								value={String(audioIdx)}
								disabled={!ffmpegAvailable || (session.probe?.audio_tracks.length ?? 0) === 0}
								onchange={(e) => onChangeAudio(Number((e.currentTarget as HTMLSelectElement).value))}
							>
								{#if (session.probe?.audio_tracks.length ?? 0) === 0}
									<option value="0">Default Audio</option>
								{:else}
									{#each session.probe?.audio_tracks ?? [] as tr (tr.index)}
										<option value={String(tr.index)}>
											{[tr.language?.toUpperCase(), tr.title, tr.codec?.toUpperCase()].filter(Boolean).join(' · ') || `Audio ${tr.index + 1}`}
										</option>
									{/each}
								{/if}
							</select>
						</label>
					</div>
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
				<button type="submit" class="wp-mini-btn accent" disabled={searching}>{searching ? '...' : 'Search'}</button>
			</form>

			<div class="wp-tab-row">
				<button type="button" class="wp-chip" class:on={sourceTab === 'all'} onclick={() => { sourceTab = 'all'; }}>All ({candidates.length})</button>
				<button type="button" class="wp-chip" class:on={sourceTab === 'instant'} onclick={() => { sourceTab = 'instant'; }}>Instant ({counts.instant})</button>
				{#if counts.local > 0}
					<button type="button" class="wp-chip" class:on={sourceTab === 'local'} onclick={() => { sourceTab = 'local'; }}>Local ({counts.local})</button>
				{/if}
				{#if counts.debrid > 0}
					<button type="button" class="wp-chip" class:on={sourceTab === 'debrid'} onclick={() => { sourceTab = 'debrid'; }}>Debrid ({counts.debrid})</button>
				{/if}
				<button type="button" class="wp-chip" class:on={sourceTab === 'torrent'} onclick={() => { sourceTab = 'torrent'; }}>Torrents ({counts.torrent})</button>
			</div>

			<div class="wp-tab-row">
				{#each ['all', '2160p', '1080p', '720p', 'sd'] as r}
					<button type="button" class="wp-chip" class:on={resFilter === r} onclick={() => { resFilter = r as typeof resFilter; }}>
						{r === 'all' ? 'Any Quality' : r.toUpperCase()}
					</button>
				{/each}
			</div>

			{#if searchError}
				<div class="wp-err-inline">{searchError}</div>
			{/if}

			{#if searching}
				<p class="wp-muted">Searching Local Library, Stremio addons, Nuvio plugins &amp; indexers...</p>
			{:else if visibleList.length === 0}
				<p class="wp-muted">No matching streams found in this tab.</p>
			{:else}
				<div class="wp-cand-list">
					{#each visibleList as c (candIdKey(c))}
						{@const b = badgeFor(c)}
						<article class="wp-cand" class:current={activeCandidateKey === candIdKey(c)}>
							<div class="wp-cand-info">
								<div class="wp-cand-name">{c.release_name || c.title}</div>
								<div class="wp-cand-meta">
									<span class="tag kind-{b.cls}">{b.text}</span>
									<span class="tag src">{c.provider || c.source_name}</span>
									{#if c.quality || c.resolution}<span class="tag res">{c.quality || c.resolution}</span>{/if}
									{#if c.hdr}<span class="tag hdr">{c.hdr}</span>{/if}
									{#if c.codec}<span class="tag">{c.codec}</span>{/if}
									{#if c.size > 0}<span>{bytes(c.size)}</span>{/if}
									{#if (c.kind ?? 'torrent') === 'torrent'}<span>{c.seeders} seeds</span>{/if}
								</div>
							</div>
							<div class="wp-cand-acts">
								<button type="button" class="wp-mini-btn accent" onclick={() => onStartCandidate(c, 'browser')}>Play</button>
								{#if mpvAvailable}
									<button type="button" class="wp-mini-btn" onclick={() => onStartCandidate(c, 'mpv')}>MPV</button>
								{/if}
							</div>
						</article>
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
	.wp-close { width: 32px; height: 32px; border-radius: 50%; border: 0; background: rgb(255 255 255 / 12%); color: #fff; display: grid; place-items: center; cursor: pointer; }
	.wp-drawer-body { flex: 1; overflow-y: auto; padding: 14px 16px 28px; display: flex; flex-direction: column; gap: 12px; }
	.wp-fallback-banner { padding: 8px 10px; border-radius: 8px; background: rgb(245 158 11 / 20%); color: #fcd34d; font-size: 12.5px; }
	.wp-box { padding: 12px; border-radius: 10px; background: rgb(255 255 255 / 5%); display: flex; flex-direction: column; gap: 10px; }
	.wp-box-title { font-size: 12px; font-weight: 700; color: rgb(255 255 255 / 72%); text-transform: uppercase; letter-spacing: .04em; }
	.wp-grid-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
	.wp-label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: rgb(255 255 255 / 72%); }
	.wp-label select, .wp-search-row input { height: 34px; padding: 0 10px; border-radius: 8px; border: 1px solid rgb(255 255 255 / 20%); background: rgb(28 28 34); color: #fff; font-size: 13px; }
	.wp-search-row { display: flex; gap: 8px; }
	.wp-search-row input { flex: 1; }
	.wp-tab-row { display: flex; flex-wrap: wrap; gap: 6px; }
	.wp-chip { padding: 5px 10px; border: 1px solid rgb(255 255 255 / 18%); border-radius: 999px; background: transparent; color: rgb(255 255 255 / 78%); font-size: 12px; font-weight: 600; cursor: pointer; }
	.wp-chip.on { background: #fff; color: #111; border-color: #fff; }
	.wp-cand-list { display: flex; flex-direction: column; gap: 8px; }
	.wp-cand { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 10px 12px; border: 1px solid rgb(255 255 255 / 12%); border-radius: 10px; background: rgb(255 255 255 / 4%); }
	.wp-cand.current { border-color: #e50914; background: rgb(229 9 20 / 14%); }
	.wp-cand-info { min-width: 0; flex: 1; display: flex; flex-direction: column; gap: 5px; }
	.wp-cand-name { font-size: 13px; font-weight: 600; word-break: break-word; }
	.wp-cand-meta { display: flex; flex-wrap: wrap; gap: 5px; font-size: 11px; color: rgb(255 255 255 / 72%); }
	.tag { padding: 2px 6px; border-radius: 5px; background: rgb(255 255 255 / 12%); color: #fff; font-weight: 600; }
	.tag.kind-local { background: rgb(34 197 94 / 26%); color: #86efac; }
	.tag.kind-debrid { background: rgb(168 85 247 / 28%); color: #e9d5ff; }
	.tag.kind-instant { background: rgb(56 189 248 / 26%); color: #bae6fd; }
	.tag.kind-torrent { background: rgb(245 158 11 / 22%); color: #fde68a; }
	.tag.src { background: rgb(255 255 255 / 14%); }
	.tag.res { background: rgb(34 197 94 / 22%); }
	.tag.hdr { background: rgb(245 158 11 / 22%); }
	.wp-cand-acts { display: flex; gap: 6px; flex: none; }
	.wp-mini-btn { padding: 6px 10px; border-radius: 8px; border: 1px solid rgb(255 255 255 / 20%); background: rgb(255 255 255 / 10%); color: #fff; font-size: 12px; font-weight: 600; cursor: pointer; }
	.wp-mini-btn.accent { background: #e50914; border-color: #e50914; }
	.wp-muted { margin: 0; color: rgb(255 255 255 / 66%); font-size: 13px; }
	.wp-err-inline { padding: 8px 10px; border-radius: 8px; background: rgb(239 68 68 / 20%); color: #fca5a5; font-size: 12.5px; }
</style>
