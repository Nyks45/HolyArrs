<script lang="ts">
	import type HlsType from 'hls.js';
	import { onDestroy } from 'svelte';
	import Sheet from '#lib/ui/Sheet.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import { message } from '#lib/core/api.ts';
	import { bytes } from '#lib/core/format.ts';
	import { to } from '#lib/core/paths.ts';
	import { toasts } from '#lib/core/toasts.svelte.ts';
	import { hlsScript } from '#lib/core/trailer/hls-player.ts';
	import {
		ensureStreamHLS,
		formatDuration,
		formatSpeed,
		getStreamStatus,
		launchStreamMPV,
		listStreamSources,
		searchStreamReleases,
		selectStreamFile,
		setStreamKeep,
		startStream,
		stopStream,
		type SessionStatus,
		type StreamCandidate,
		type StreamSourceInfo
	} from './stream-api.ts';
	import StreamCandidateList from './StreamCandidateList.svelte';

	let {
		open = $bindable(false),
		kind,
		title,
		year,
		tmdb,
		imdb = '',
		seasons = [],
		initialSeason = 1,
		initialEpisode = 1
	}: {
		open?: boolean;
		kind: 'movie' | 'show';
		title: string;
		year?: number;
		tmdb: string;
		imdb?: string;
		seasons?: { number: number; name?: string }[];
		initialSeason?: number;
		initialEpisode?: number;
	} = $props();

	let availableSources = $state<StreamSourceInfo[]>([]);
	let selectedSourceIds = $state<number[]>([]);
	let mpvAvailable = $state(false);
	let ffmpegAvailable = $state(false);

	let queryText = $state('');
	let season = $state(1);
	let episode = $state(1);
	let searching = $state(false);
	let searchError = $state('');
	let candidates = $state<StreamCandidate[]>([]);
	let counts = $state({ local: 0, instant: 0, debrid: 0, torrent: 0 });

	let sourceTab = $state<'all' | 'instant' | 'local' | 'torrent'>('all');
	let resFilter = $state<'all' | '2160p' | '1080p' | '720p' | 'sd'>('all');
	let onlySeeded = $state(true);
	let sortBy = $state<'seeders' | 'quality' | 'size_asc' | 'size_desc'>('seeders');

	let session = $state<SessionStatus | null>(null);
	let activeKey = $state('');
	let starting = $state(false);
	let playMode = $state<'browser' | 'mpv'>('browser');
	let pendingMPV = $state(false);
	let browserQuality = $state<'original' | '1080p' | '720p' | '480p'>('original');
	let audioIdx = $state(0);
	let subIdx = $state(-1);
	let playbackRate = $state(1);
	let seekOffset = $state(0);
	let videoTime = $state(0);
	let isScrubbing = $state(false);
	let scrubValue = $state(0);
	let pipelineMode = $state<'none' | 'direct' | 'hls'>('none');
	let loadingMedia = $state(false);
	let playerError = $state('');

	let videoEl = $state<HTMLVideoElement | undefined>(undefined);
	let hlsInstance: HlsType | null = null;
	let pollTimer: ReturnType<typeof setInterval> | null = null;
	let attachedSessionKey = '';
	let wasOpen = false;
	let loadReqSeq = 0;
	let cachedHlsCtor: Promise<typeof HlsType> | null = null;

	function loadHlsCtor(): Promise<typeof HlsType> {
		cachedHlsCtor ??= (import(/* @vite-ignore */ to(hlsScript)) as Promise<{ default: typeof HlsType }>).then((m) => m.default);
		cachedHlsCtor.catch(() => (cachedHlsCtor = null));
		return cachedHlsCtor;
	}

	const regularSeasons = $derived.by(() => {
		const nonZero = seasons.filter((s) => s.number > 0);
		return nonZero.length > 0 ? nonZero : seasons;
	});

	const sheetTitle = $derived(
		kind === 'show' ? `${title} · S${String(season).padStart(2, '0')}E${String(episode).padStart(2, '0')}` : title
	);

	const watchHref = $derived(
		kind === 'show'
			? to(`/watch/show/${encodeURIComponent(tmdb)}?s=${season}&e=${episode}`)
			: to(`/watch/movie/${encodeURIComponent(tmdb)}`)
	);

	const currentPos = $derived(isScrubbing ? scrubValue : pipelineMode === 'hls' ? seekOffset + videoTime : videoTime);
	const totalDuration = $derived(session?.probe?.duration_sec ?? 0);

	const activeSubUrl = $derived.by(() => {
		if (!session || subIdx < 0) return '';
		const base = `/api/v1/stream/${encodeURIComponent(session.id)}/sub/${encodeURIComponent(session.token)}/${subIdx}`;
		return pipelineMode === 'hls' && seekOffset > 0.5 ? to(`${base}?offset=${Math.floor(seekOffset)}`) : to(base);
	});

	function candKey(c: StreamCandidate): string {
		return c.id || `${c.source_id}:${c.link}`;
	}

	function destroyPlayer() {
		loadingMedia = false;
		if (hlsInstance) { hlsInstance.destroy(); hlsInstance = null; }
		if (videoEl) { videoEl.pause(); videoEl.removeAttribute('src'); videoEl.load(); }
		pipelineMode = 'none';
	}

	function stopPolling() {
		if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
	}

	function startPolling(id: string) {
		stopPolling();
		pollTimer = setInterval(() => void pollSession(id), 1200);
	}

	async function pollSession(id: string) {
		try {
			const st = await getStreamStatus(id);
			if (!open || !session || session.id !== id) return;
			session = st;
			if (st.state === 'error') { pendingMPV = false; return; }
			if (pendingMPV && ((st.transport && st.transport !== 'torrent') || (st.files.length > 0 && st.state !== 'resolving'))) {
				pendingMPV = false;
				await triggerMPV(st.id);
				return;
			}
			if (playMode === 'browser' && st.state === 'ready' && !st.mpv_running) {
				const key = `${st.id}:${st.selected_file}`;
				if (attachedSessionKey !== key) {
					attachedSessionKey = key;
					await attachPlayback(0);
				}
			}
		} catch {
			/* Ignore transient poll errors. */
		}
	}

	async function initOnOpen() {
		queryText = title;
		if (kind === 'show') {
			season = initialSeason > 0 ? initialSeason : regularSeasons[0]?.number ?? 1;
			episode = initialEpisode > 0 ? initialEpisode : 1;
		}
		try {
			const srcs = await listStreamSources(kind);
			availableSources = srcs.sources ?? [];
			mpvAvailable = srcs.mpv_available;
			ffmpegAvailable = srcs.ffmpeg_available;
		} catch {
			availableSources = [];
		}
		await runSearch();
	}

	async function runSearch() {
		const q = queryText.trim();
		if (!q && !imdb && !tmdb) return;
		searching = true;
		searchError = '';
		try {
			const res = await searchStreamReleases({
				query: q || title,
				kind,
				year: year || 0,
				season: kind === 'show' ? season : 0,
				episode: kind === 'show' ? episode : 0,
				imdb,
				tmdb: Number(tmdb) || 0,
				source_ids: selectedSourceIds
			});
			candidates = res.candidates ?? [];
			counts = res.counts ?? { local: 0, instant: 0, debrid: 0, torrent: candidates.length };
		} catch (e) {
			searchError = message(e);
			candidates = [];
		} finally {
			searching = false;
		}
	}

	async function playCandidate(c: StreamCandidate, mode: 'browser' | 'mpv') {
		destroyPlayer();
		stopPolling();
		session = null;
		attachedSessionKey = '';
		activeKey = candKey(c);
		playMode = mode;
		pendingMPV = mode === 'mpv';
		starting = true;
		playerError = '';
		browserQuality = 'original';
		audioIdx = 0;
		subIdx = -1;
		seekOffset = 0;
		videoTime = 0;

		try {
			const st = await startStream({
				candidate_id: c.id,
				local_file_id: c.local_file_id,
				debrid_provider: c.debrid_provider,
				source_id: c.source_id,
				source_name: c.provider || c.source_name,
				link: c.link,
				title: sheetTitle,
				release_title: c.release_name || c.title,
				kind,
				season: kind === 'show' ? season : 0,
				episode: kind === 'show' ? episode : 0,
				keep_file: false,
				tmdb: Number(tmdb) || 0,
				imdb
			});
			session = st;
			startPolling(st.id);
			if (st.state === 'ready' || (pendingMPV && st.state !== 'resolving')) void pollSession(st.id);
		} catch (e) {
			pendingMPV = false;
			playerError = message(e);
			toasts.push('Could not start stream', 'fail', { body: playerError });
		} finally {
			starting = false;
		}
	}

	function playFastest() {
		const top = candidates.find((c) => c.instant) ?? candidates[0];
		if (top) void playCandidate(top, 'browser');
	}

	async function attachPlayback(startSec: number) {
		const st = session;
		if (!st || !videoEl) return;
		const req = ++loadReqSeq;
		destroyPlayer();
		loadReqSeq = req;
		loadingMedia = true;
		playerError = '';
		seekOffset = startSec;
		videoTime = 0;

		try {
			const tr = st.transport ?? 'torrent';
			const directUrl = to(st.direct_url || st.media_url);
			if (tr === 'hls_remote') {
				pipelineMode = 'hls';
				await attachHLSUrl(directUrl, req);
				return;
			}
			const canDirect = browserQuality === 'original' && audioIdx === 0 && (tr === 'local' || tr === 'direct' ? st.probe?.direct_playable !== false : !!st.probe?.direct_playable) && startSec <= 0.5;
			if (canDirect) {
				pipelineMode = 'direct';
				videoEl.src = directUrl;
				videoEl.playbackRate = playbackRate;
				await videoEl.play().catch(() => {});
				return;
			}
			if (!ffmpegAvailable) throw new Error('ffmpeg is not available for remuxing/transcoding');
			const hlsRes = await ensureStreamHLS(st.id, browserQuality, audioIdx, startSec);
			if (req !== loadReqSeq || !session || session.id !== st.id || !videoEl) return;
			seekOffset = hlsRes.start_sec;
			pipelineMode = 'hls';
			await attachHLSUrl(to(hlsRes.playlist_url), req);
		} catch (e) {
			if (req === loadReqSeq) playerError = message(e);
		} finally {
			if (req === loadReqSeq) loadingMedia = false;
		}
	}

	async function attachHLSUrl(srcUrl: string, req: number) {
		if (!videoEl) return;
		const Hls = await loadHlsCtor();
		if (req !== loadReqSeq || !videoEl) return;
		if (Hls.isSupported()) {
			const hls = new Hls({ enableWorker: true, maxBufferLength: 30, maxMaxBufferLength: 60, fragLoadingTimeOut: 30000, manifestLoadingTimeOut: 25000, renderTextTracksNatively: false });
			hlsInstance = hls;
			hls.on(Hls.Events.ERROR, (_ev, data) => { if (data.fatal) playerError = `Playback error (${data.details})`; });
			hls.loadSource(srcUrl);
			hls.attachMedia(videoEl);
			hls.on(Hls.Events.MANIFEST_PARSED, () => { if (videoEl) { videoEl.playbackRate = playbackRate; void videoEl.play().catch(() => {}); } });
		} else if (videoEl.canPlayType('application/vnd.apple.mpegurl')) {
			videoEl.src = srcUrl;
			videoEl.playbackRate = playbackRate;
			await videoEl.play().catch(() => {});
		} else {
			throw new Error('HLS is not supported in this browser');
		}
	}

	async function triggerMPV(id: string) {
		destroyPlayer();
		playMode = 'mpv';
		try {
			session = await launchStreamMPV(id, '');
		} catch (e) {
			playerError = message(e);
			toasts.push('Could not open MPV', 'fail', { body: playerError });
		}
	}

	function handleSheetClose() {
		destroyPlayer();
		stopPolling();
		if (session && !session.mpv_running) void stopStream(session.id).catch(() => {});
		session = null;
		activeKey = '';
		attachedSessionKey = '';
		open = false;
	}

	$effect(() => {
		if (open && !wasOpen) { wasOpen = true; void initOnOpen(); }
		else if (!open && wasOpen) { wasOpen = false; handleSheetClose(); }
	});

	onDestroy(() => {
		destroyPlayer();
		stopPolling();
		if (session && !session.mpv_running) void stopStream(session.id).catch(() => {});
	});
</script>

<Sheet bind:open title={sheetTitle} size="xl" onclose={handleSheetClose}>
	<div class="ss-wrap" data-testid="stream-sheet">
		<section class="ss-controls">
			<form class="ss-query-row" onsubmit={(e) => { e.preventDefault(); void runSearch(); }}>
				{#if kind === 'show'}
					<label class="ss-field ss-compact">
						<span>Season</span>
						<select value={season} onchange={(e) => { season = Number(e.currentTarget.value); episode = 1; void runSearch(); }}>
							{#each regularSeasons.length > 0 ? regularSeasons : [{ number: season }] as s (s.number)}
								<option value={s.number}>S{String(s.number).padStart(2, '0')}</option>
							{/each}
						</select>
					</label>
					<label class="ss-field ss-compact">
						<span>Ep</span>
						<input type="number" min="1" max="999" bind:value={episode} onchange={() => void runSearch()} />
					</label>
				{/if}

				<label class="ss-field ss-grow">
					<span class="sr-only">Search query</span>
					<input type="search" placeholder="Search release name..." bind:value={queryText} />
				</label>

				<button type="submit" class="ss-btn ss-btn-primary" disabled={searching}>
					<Icon name="search" size={16} /><span>{searching ? 'Searching...' : 'Search'}</span>
				</button>
				<a class="ss-btn" href={watchHref} onclick={() => { open = false; }}>Full Player</a>
			</form>
		</section>

		{#if session || starting || playerError}
			<section class="ss-player-card">
				{#if session}
					<div class="ss-tel-bar">
						<strong>{session.release_title || session.title}</strong>
						<span class="ss-tel-meta">
							{session.source_provider || session.source_name} · {session.state}
							{#if session.transport === 'torrent' || !session.transport}
								· {session.seeders} seeds · {formatSpeed(session.download_bps)}
							{:else if session.file_size > 0}
								· {bytes(session.file_size)}
							{/if}
						</span>
						<div class="ss-tel-acts">
							{#if mpvAvailable && session.state !== 'resolving'}
								<button type="button" class="ss-btn" onclick={() => { if (session) void triggerMPV(session.id); }}>MPV</button>
							{/if}
							{#if session.transport === 'torrent' || !session.transport}
								<button type="button" class="ss-btn" onclick={() => { if (session) void setStreamKeep(session.id, !session.keep_file).then((s) => { session = s; }); }}>
									{session.keep_file ? 'Keeping File' : 'Keep'}
								</button>
							{/if}
							<button type="button" class="ss-btn" onclick={() => { if (session) void stopStream(session.id); session = null; destroyPlayer(); stopPolling(); }}>Stop</button>
						</div>
					</div>
				{/if}
				{#if playerError}<p class="ss-err">{playerError}</p>{/if}
				{#if playMode === 'browser' && session}
					<div class="ss-video-wrap">
						<video bind:this={videoEl} controls playsinline class="ss-video" ontimeupdate={() => { if (videoEl) videoTime = videoEl.currentTime; }}>
							{#if activeSubUrl}<track kind="subtitles" src={activeSubUrl} label="Subtitle" default />{/if}
						</video>
						{#if totalDuration > 0 && pipelineMode === 'hls'}
							<div class="ss-seek-bar">
								<span>{formatDuration(currentPos)}</span>
								<input type="range" min="0" max={totalDuration} step="1" value={currentPos} oninput={(e) => { isScrubbing = true; scrubValue = Number((e.currentTarget as HTMLInputElement).value); }} onchange={(e) => { isScrubbing = false; void attachPlayback(Number((e.currentTarget as HTMLInputElement).value)); }} />
								<span>{formatDuration(totalDuration)}</span>
							</div>
						{/if}
					</div>
					{#if loadingMedia}<p class="ss-muted">Preparing playback stream...</p>{/if}
				{/if}
				{#if (session?.files?.length ?? 0) > 1 && session}
					<select value={String(session.selected_file)} onchange={(e) => { if (session) void selectStreamFile(session.id, Number((e.currentTarget as HTMLSelectElement).value)).then((s) => { session = s; attachedSessionKey = ''; }); }}>
						{#each session.files as f (f.index)}<option value={String(f.index)}>{f.name} ({bytes(f.size)})</option>{/each}
					</select>
				{/if}
			</section>
		{/if}

		<StreamCandidateList
			{availableSources}
			{selectedSourceIds}
			bind:sourceTab
			bind:resFilter
			bind:onlySeeded
			bind:sortBy
			{searching}
			{searchError}
			{candidates}
			{counts}
			{activeKey}
			{mpvAvailable}
			onToggleAllSources={() => { selectedSourceIds = []; void runSearch(); }}
			onToggleSource={(id) => { selectedSourceIds = selectedSourceIds.includes(id) ? selectedSourceIds.filter((x) => x !== id) : [...selectedSourceIds, id]; void runSearch(); }}
			onPlayCandidate={(c, m) => void playCandidate(c, m)}
			onPlayFastest={playFastest}
		/>
	</div>
</Sheet>

<style>
	.ss-wrap { display: flex; flex-direction: column; gap: 14px; }
	.ss-query-row { display: flex; flex-wrap: wrap; gap: 8px; align-items: end; }
	.ss-field { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--muted); }
	.ss-field.ss-grow { flex: 1; min-width: 180px; }
	.ss-field input, .ss-field select { height: 36px; padding: 0 10px; border-radius: 9px; border: 1px solid var(--border); background: var(--surface-2); color: var(--text); font-size: 13px; }
	.ss-field.ss-compact input { width: 72px; }
	.ss-btn { display: inline-flex; align-items: center; gap: 6px; height: 36px; padding: 0 14px; border-radius: 9px; border: 1px solid var(--border); background: var(--surface-2); color: var(--text); font-size: 13px; font-weight: 600; text-decoration: none; cursor: pointer; }
	.ss-btn-primary { background: var(--accent); border-color: var(--accent); color: #fff; }
	.ss-player-card { padding: 12px; border-radius: 12px; border: 1px solid var(--border); background: var(--surface-2); display: flex; flex-direction: column; gap: 10px; }
	.ss-tel-bar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px; font-size: 13px; }
	.ss-tel-meta { color: var(--muted); font-size: 12px; }
	.ss-tel-acts { display: flex; gap: 6px; }
	.ss-video-wrap { display: flex; flex-direction: column; gap: 6px; }
	.ss-video { width: 100%; max-height: 38vh; border-radius: 9px; background: #000; }
	.ss-seek-bar { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--muted); }
	.ss-seek-bar input { flex: 1; accent-color: var(--accent); }
	.ss-muted { margin: 0; font-size: 12.5px; color: var(--muted); }
	.ss-err { margin: 0; font-size: 12.5px; color: #e5484d; }
</style>
