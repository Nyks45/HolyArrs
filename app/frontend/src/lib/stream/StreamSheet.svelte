<script lang="ts">
	import type HlsType from 'hls.js';
	import { onDestroy } from 'svelte';
	import Sheet from '#lib/ui/Sheet.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
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
		type StreamSourceCheck,
		type StreamSourceInfo
	} from './stream-api.ts';

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
		seasons?: { number: number; episodes: number; year?: number }[];
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
	let sourceChecks = $state<StreamSourceCheck[]>([]);

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

	const filteredCandidates = $derived.by(() => {
		const list = candidates.filter((c) => {
			if (onlySeeded && c.seeders <= 0) return false;
			if (resFilter === '2160p') return c.resolution_num >= 2160;
			if (resFilter === '1080p') return c.resolution_num === 1080;
			if (resFilter === '720p') return c.resolution_num === 720;
			if (resFilter === 'sd') return c.resolution_num > 0 && c.resolution_num < 720;
			return true;
		});
		const copy = [...list];
		copy.sort((a, b) => {
			if (sortBy === 'quality') {
				if (b.resolution_num !== a.resolution_num) return b.resolution_num - a.resolution_num;
				return b.seeders - a.seeders;
			}
			if (sortBy === 'size_asc') return a.size - b.size;
			if (sortBy === 'size_desc') return b.size - a.size;
			if (b.seeders !== a.seeders) return b.seeders - a.seeders;
			if (b.resolution_num !== a.resolution_num) return b.resolution_num - a.resolution_num;
			return b.size - a.size;
		});
		return copy;
	});

	const currentPos = $derived(
		isScrubbing ? scrubValue : pipelineMode === 'hls' ? seekOffset + videoTime : videoTime
	);

	const totalDuration = $derived(session?.probe?.duration_sec ?? 0);

	const activeSubUrl = $derived.by(() => {
		if (!session || subIdx < 0) return '';
		const base = `/api/v1/stream/${encodeURIComponent(session.id)}/sub/${encodeURIComponent(session.token)}/${subIdx}`;
		if (pipelineMode === 'hls' && seekOffset > 0.5) {
			return to(`${base}?offset=${Math.floor(seekOffset)}`);
		}
		return to(base);
	});

	const activeSubLabel = $derived.by(() => {
		const track = session?.probe?.subtitle_tracks.find((s) => s.index === subIdx);
		if (!track) return 'Subtitles';
		return [track.language?.toUpperCase(), track.title].filter(Boolean).join(' · ') || `Track ${track.index + 1}`;
	});

	function candKey(c: StreamCandidate): string {
		return `${c.source_id}:${c.info_hash || c.link}:${c.title}`;
	}

	function destroyPlayer() {
		loadReqSeq++;
		if (hlsInstance) {
			hlsInstance.destroy();
			hlsInstance = null;
		}
		if (videoEl) {
			videoEl.pause();
			videoEl.removeAttribute('src');
			videoEl.load();
		}
		pipelineMode = 'none';
		loadingMedia = false;
	}

	function stopPolling() {
		if (pollTimer) {
			clearInterval(pollTimer);
			pollTimer = null;
		}
	}

	function startPolling(id: string) {
		stopPolling();
		pollTimer = setInterval(() => void pollSession(id), 1200);
	}

	async function pollSession(id: string) {
		try {
			const st = await getStreamStatus(id);
			if (!session || session.id !== id) return;
			session = st;

			if (st.state === 'error') {
				pendingMPV = false;
				return;
			}

			if (pendingMPV && st.files.length > 0 && st.state !== 'resolving') {
				pendingMPV = false;
				await triggerMPV(st.id);
				return;
			}

			if (playMode === 'browser' && st.state === 'ready' && !st.mpv_running) {
				const key = `${st.id}:${st.selected_file}`;
				if (attachedSessionKey !== key) {
					attachedSessionKey = key;
					audioIdx = 0;
					subIdx = -1;
					await attachPlayback(0);
				}
			}
		} catch {
			// Ignore transient poll glitches while stream initializes.
		}
	}

	$effect(() => {
		if (open && !wasOpen) {
			wasOpen = true;
			void initOnOpen();
		} else if (!open && wasOpen) {
			wasOpen = false;
		}
	});

	async function initOnOpen() {
		queryText = title;
		const validSeason =
			regularSeasons.some((s) => s.number === initialSeason)
				? initialSeason
				: (regularSeasons[0]?.number ?? initialSeason ?? 1);
		season = validSeason;
		episode = initialEpisode > 0 ? initialEpisode : 1;
		resFilter = 'all';
		onlySeeded = true;
		sortBy = 'seeders';
		searchError = '';

		try {
			const res = await listStreamSources(kind);
			availableSources = res.sources ?? [];
			mpvAvailable = res.mpv_available;
			ffmpegAvailable = res.ffmpeg_available;
		} catch (e) {
			searchError = message(e);
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
			sourceChecks = res.sources ?? [];
		} catch (e) {
			searchError = message(e);
			candidates = [];
		} finally {
			searching = false;
		}
	}

	function toggleAllSources() {
		selectedSourceIds = [];
		void runSearch();
	}

	function toggleSource(id: number) {
		if (selectedSourceIds.includes(id)) {
			selectedSourceIds = selectedSourceIds.filter((x) => x !== id);
		} else {
			selectedSourceIds = [...selectedSourceIds, id];
		}
		void runSearch();
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
				source_id: c.source_id,
				source_name: c.source_name,
				link: c.link,
				title: sheetTitle,
				release_title: c.title,
				kind,
				season: kind === 'show' ? season : 0,
				episode: kind === 'show' ? episode : 0,
				keep_file: false
			});
			session = st;
			startPolling(st.id);
			if (st.state === 'ready' || (pendingMPV && st.files.length > 0 && st.state !== 'resolving')) {
				void pollSession(st.id);
			}
		} catch (e) {
			pendingMPV = false;
			playerError = message(e);
			toasts.push('Could not start torrent stream', 'fail', { body: playerError });
		} finally {
			starting = false;
		}
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
			const canDirect = !!st.probe?.direct_playable && browserQuality === 'original' && audioIdx === 0;
			if (canDirect && startSec <= 0.5) {
				pipelineMode = 'direct';
				videoEl.src = to(st.media_url);
				videoEl.playbackRate = playbackRate;
				await videoEl.play().catch(() => {});
				return;
			}

			if (!ffmpegAvailable) {
				throw new Error('ffmpeg is not available for remuxing/transcoding');
			}
			const hlsRes = await ensureStreamHLS(st.id, browserQuality, audioIdx, startSec);
			if (req !== loadReqSeq || !session || session.id !== st.id || !videoEl) return;
			seekOffset = hlsRes.start_sec;
			pipelineMode = 'hls';
			const srcUrl = to(hlsRes.playlist_url);

			const Hls = await loadHlsCtor();
			if (req !== loadReqSeq || !videoEl) return;

			if (Hls.isSupported()) {
				const hls = new Hls({
					enableWorker: true,
					maxBufferLength: 30,
					maxMaxBufferLength: 60,
					fragLoadingTimeOut: 30000,
					manifestLoadingTimeOut: 25000,
					renderTextTracksNatively: false,
					enableID3MetadataCues: false,
					enableEmsgMetadataCues: false,
					enableDateRangeMetadataCues: false
				});
				hlsInstance = hls;
				hls.on(Hls.Events.ERROR, (_ev, data) => {
					if (data.fatal) {
						playerError = `Playback error (${data.details})`;
					}
				});
				hls.loadSource(srcUrl);
				hls.attachMedia(videoEl);
				hls.on(Hls.Events.MANIFEST_PARSED, () => {
					if (!videoEl) return;
					videoEl.playbackRate = playbackRate;
					void videoEl.play().catch(() => {});
				});
			} else if (videoEl.canPlayType('application/vnd.apple.mpegurl')) {
				videoEl.src = srcUrl;
				videoEl.playbackRate = playbackRate;
				await videoEl.play().catch(() => {});
			} else {
				throw new Error('HLS is not supported in this browser');
			}
		} catch (e) {
			if (req === loadReqSeq) {
				playerError = message(e);
			}
		} finally {
			if (req === loadReqSeq) {
				loadingMedia = false;
			}
		}
	}

	async function triggerMPV(id: string) {
		destroyPlayer();
		playMode = 'mpv';
		playerError = '';
		try {
			const updated = await launchStreamMPV(id, 'eng,en,tur,tr', 'tur,tr,eng,en');
			session = updated;
			toasts.push('Opened stream in mpv', 'ok');
		} catch (e) {
			playerError = message(e);
			toasts.push('Could not open mpv', 'fail', { body: playerError });
		}
	}

	async function changeFile(fileIndex: number) {
		if (!session) return;
		destroyPlayer();
		attachedSessionKey = '';
		try {
			session = await selectStreamFile(session.id, fileIndex);
			audioIdx = 0;
			subIdx = -1;
			seekOffset = 0;
			videoTime = 0;
			if (playMode === 'mpv') {
				pendingMPV = true;
			}
		} catch (e) {
			playerError = message(e);
		}
	}

	function changeQuality(q: 'original' | '1080p' | '720p' | '480p') {
		const resumeAt = Math.max(0, currentPos);
		browserQuality = q;
		if (session && playMode === 'browser' && session.state === 'ready') {
			void attachPlayback(resumeAt);
		}
	}

	function changeAudio(idx: number) {
		const resumeAt = Math.max(0, currentPos);
		audioIdx = idx;
		if (session && playMode === 'browser' && session.state === 'ready') {
			void attachPlayback(resumeAt);
		}
	}

	function changeSpeed(rate: number) {
		playbackRate = rate;
		if (videoEl) videoEl.playbackRate = rate;
	}

	function commitSeek(targetSec: number) {
		isScrubbing = false;
		if (!session || !videoEl) return;
		if (pipelineMode === 'direct') {
			videoEl.currentTime = targetSec;
			videoTime = targetSec;
			return;
		}
		if (pipelineMode === 'hls') {
			let maxEnd = 0;
			for (let i = 0; i < videoEl.buffered.length; i++) {
				if (videoEl.buffered.end(i) > maxEnd) maxEnd = videoEl.buffered.end(i);
			}
			if (targetSec >= seekOffset && targetSec <= seekOffset + maxEnd) {
				videoEl.currentTime = targetSec - seekOffset;
				videoTime = targetSec - seekOffset;
				return;
			}
			void attachPlayback(targetSec);
		}
	}

	async function toggleKeep() {
		if (!session) return;
		try {
			session = await setStreamKeep(session.id, !session.keep_file);
		} catch (e) {
			toasts.push('Could not update keep setting', 'fail', { body: message(e) });
		}
	}

	async function copyStreamURL() {
		if (!session || typeof window === 'undefined') return;
		const full = `${window.location.origin}${to(session.media_url)}`;
		try {
			await navigator.clipboard.writeText(full);
			toasts.push('Stream URL copied', 'ok');
		} catch {
			toasts.push('Stream URL ready', 'info', { body: full });
		}
	}

	async function stopActiveSession() {
		destroyPlayer();
		stopPolling();
		pendingMPV = false;
		attachedSessionKey = '';
		const id = session?.id;
		session = null;
		activeKey = '';
		if (id) {
			await stopStream(id).catch(() => {});
		}
	}

	function handleSheetClose() {
		destroyPlayer();
		stopPolling();
		pendingMPV = false;
		const s = session;
		if (!s) return;
		if (s.mpv_running) {
			toasts.push('Stream kept running for mpv', 'info', { body: 'Reopen Stream on this title to manage or stop it.' });
			return;
		}
		session = null;
		activeKey = '';
		attachedSessionKey = '';
		void stopStream(s.id).catch(() => {});
	}

	function formatAudioLabel(idx: number, lang?: string, title?: string, codec?: string, ch?: number): string {
		const chLabel = ch === 6 ? '5.1' : ch === 8 ? '7.1' : ch === 2 ? '2.0' : ch ? `${ch}ch` : '';
		const parts = [lang?.toUpperCase(), title, codec?.toUpperCase(), chLabel].filter(Boolean);
		return parts.join(' · ') || `Audio ${idx + 1}`;
	}

	function formatSubLabel(idx: number, lang?: string, title?: string, codec?: string): string {
		const parts = [lang?.toUpperCase(), title, codec?.toUpperCase()].filter(Boolean);
		return parts.join(' · ') || `Subtitle ${idx + 1}`;
	}

	onDestroy(() => {
		destroyPlayer();
		stopPolling();
	});
</script>

<Sheet bind:open title={sheetTitle} size="xl" onclose={handleSheetClose}>
	<div class="ss-wrap" data-testid="stream-sheet">
		<!-- Search, Episode & Source Picker -->
		<section class="ss-controls">
			<form class="ss-query-row" onsubmit={(e) => { e.preventDefault(); void runSearch(); }}>
				{#if kind === 'show'}
					<label class="ss-field ss-compact">
						<span>Season</span>
						<select
							aria-label="Season"
							value={season}
							onchange={(e) => {
								season = Number(e.currentTarget.value);
								episode = 1;
								void runSearch();
							}}
						>
							{#if regularSeasons.length > 0}
								{#each regularSeasons as s (s.number)}
									<option value={s.number}>S{String(s.number).padStart(2, '0')}</option>
								{/each}
							{:else}
								<option value={season}>S{String(season).padStart(2, '0')}</option>
							{/if}
						</select>
					</label>
					<label class="ss-field ss-compact">
						<span>Ep</span>
						<input
							type="number"
							min="1"
							max="999"
							aria-label="Episode"
							bind:value={episode}
							onchange={() => void runSearch()}
						/>
					</label>
				{/if}

				<label class="ss-field ss-grow">
					<span class="sr-only">Search query</span>
					<input
						type="search"
						placeholder="Search release name…"
						bind:value={queryText}
						data-testid="stream-query"
					/>
				</label>

				<button type="submit" class="ss-btn ss-btn-primary" disabled={searching}>
					<Icon name="search" size={16} />
					<span>{searching ? 'Searching…' : 'Search'}</span>
				</button>
			</form>

			<!-- Source Chips -->
			<div class="ss-sources" aria-label="Sources">
				<button
					type="button"
					class="ss-chip"
					class:active={selectedSourceIds.length === 0}
					onclick={toggleAllSources}
				>
					All Sources ({availableSources.length})
				</button>
				{#each availableSources as src (src.id)}
					{@const chk = sourceChecks.find((c) => c.id === src.id)}
					<button
						type="button"
						class="ss-chip"
						class:active={selectedSourceIds.includes(src.id)}
						class:warn={!!chk?.error}
						title={chk?.error ? `${src.name}: ${chk.error}` : src.name}
						onclick={() => toggleSource(src.id)}
					>
						<span>{src.name}</span>
						{#if chk?.answered}
							<span class="ss-chip-cnt">{chk.count}</span>
						{:else if chk?.error}
							<span class="ss-chip-err">!</span>
						{/if}
					</button>
				{/each}
			</div>

			<!-- Quality & Sort Filters -->
			<div class="ss-filter-bar">
				<div class="ss-pills" role="group" aria-label="Resolution filter">
					{#each [
						{ v: 'all', label: 'All' },
						{ v: '2160p', label: '2160p 4K' },
						{ v: '1080p', label: '1080p' },
						{ v: '720p', label: '720p' },
						{ v: 'sd', label: 'SD' }
					] as opt (opt.v)}
						<button
							type="button"
							class="ss-pill"
							class:active={resFilter === opt.v}
							onclick={() => (resFilter = opt.v as typeof resFilter)}
						>
							{opt.label}
						</button>
					{/each}
				</div>

				<div class="ss-filter-right">
					<label class="ss-check">
						<input type="checkbox" bind:checked={onlySeeded} />
						<span>Seeded only</span>
					</label>

					<label class="ss-sort">
						<span>Sort</span>
						<select bind:value={sortBy} aria-label="Sort releases">
							<option value="seeders">Most Seeders</option>
							<option value="quality">Highest Quality</option>
							<option value="size_asc">Smallest Size</option>
							<option value="size_desc">Largest Size</option>
						</select>
					</label>
				</div>
			</div>
		</section>

		<!-- Active Stream Player & Telemetry -->
		{#if session || starting || playerError}
			<section class="ss-player-card" data-testid="stream-player">
				{#if session}
					<div class="ss-telemetry">
						<div class="ss-tel-main">
							<span class="ss-badge ss-state-{session.state}">
								{session.state === 'resolving'
									? 'Finding peers…'
									: session.state === 'buffering'
										? 'Buffering…'
										: session.state === 'ready'
											? 'Ready'
											: 'Error'}
							</span>
							<strong class="ss-rel-name" title={session.release_title}>{session.release_title}</strong>
						</div>
						<div class="ss-tel-stats">
							<span>{session.source_name}</span>
							<span>·</span>
							<span>{session.seeders} seeds / {session.peers} peers</span>
							<span>·</span>
							<span>{formatSpeed(session.download_bps)}</span>
							<span>·</span>
							<span>{session.progress_pct.toFixed(1)}% ({bytes(session.downloaded_bytes)} / {bytes(session.file_size)})</span>
						</div>
					</div>
				{/if}

				{#if session?.state === 'error' || playerError}
					<div class="ss-err-banner">
						<Icon name="alert" size={16} />
						<span>{session?.error || playerError}</span>
						{#if session && playMode === 'browser' && session.state === 'ready'}
							<button type="button" class="ss-btn ss-btn-sm" onclick={() => void attachPlayback(seekOffset)}>Retry</button>
						{/if}
					</div>
				{/if}

				{#if playMode === 'browser' && session}
					<div class="ss-video-box">
						{#if session.state === 'resolving' || (session.state === 'buffering' && !playerError) || loadingMedia}
							<div class="ss-video-overlay">
								<div class="ss-spinner"></div>
								<span>
									{session.state === 'resolving'
										? 'Connecting to torrent peers & fetching metadata…'
										: loadingMedia
											? 'Preparing video stream…'
											: `Buffering initial pieces (${bytes(session.downloaded_bytes)})…`}
								</span>
							</div>
						{/if}
						<!-- svelte-ignore a11y_media_has_caption -->
						<video
							bind:this={videoEl}
							controls
							playsinline
							class="ss-video"
							ontimeupdate={() => {
								if (videoEl && !isScrubbing) videoTime = videoEl.currentTime;
							}}
							onseeked={() => {
								if (videoEl && !isScrubbing) videoTime = videoEl.currentTime;
							}}
							onloadedmetadata={() => {
								if (videoEl) {
									videoEl.playbackRate = playbackRate;
									for (let i = 0; i < videoEl.textTracks.length; i++) {
										videoEl.textTracks[i].mode = subIdx >= 0 ? 'showing' : 'disabled';
									}
								}
							}}
						>
							{#if activeSubUrl}
								{#key activeSubUrl}
									<track kind="subtitles" src={activeSubUrl} label={activeSubLabel} default />
								{/key}
							{/if}
						</video>
					</div>

					{#if pipelineMode === 'hls' && totalDuration > 0}
						<div class="ss-seek-bar">
							<span class="ss-time">{formatDuration(currentPos)}</span>
							<input
								type="range"
								min="0"
								max={Math.floor(totalDuration)}
								step="1"
								aria-label="Seek timeline"
								value={Math.min(Math.floor(totalDuration), Math.floor(currentPos))}
								oninput={(e) => {
									isScrubbing = true;
									scrubValue = Number(e.currentTarget.value);
								}}
								onchange={(e) => commitSeek(Number(e.currentTarget.value))}
							/>
							<span class="ss-time">{formatDuration(totalDuration)}</span>
						</div>
					{/if}
				{:else if playMode === 'mpv' && session}
					<div class="ss-mpv-status">
						<Icon name="play" size={18} />
						<span>
							{#if session.mpv_running}
								Playing in desktop <strong>mpv</strong> — torrent streaming stays active below.
							{:else if pendingMPV || session.state === 'resolving'}
								Waiting for torrent metadata to launch <strong>mpv</strong>…
							{:else}
								<strong>mpv</strong> exited. You can relaunch it, switch to browser playback, or stop the stream.
							{/if}
						</span>
					</div>
				{/if}

				<!-- Playback Options Bar -->
				{#if session}
					<div class="ss-options-bar">
						<div class="ss-opt-group">
							{#if session.files.length > 1}
								<label class="ss-opt">
									<span>File</span>
									<select
										aria-label="Torrent video file"
										value={session.selected_file}
										onchange={(e) => void changeFile(Number(e.currentTarget.value))}
									>
										{#each session.files as f (f.index)}
											<option value={f.index}>{f.name} ({bytes(f.size)})</option>
										{/each}
									</select>
								</label>
							{/if}

							{#if playMode === 'browser'}
								<label class="ss-opt">
									<span>Quality</span>
									<select
										aria-label="Stream quality"
										value={browserQuality}
										onchange={(e) => changeQuality(e.currentTarget.value as typeof browserQuality)}
									>
										<option value="original">Original</option>
										<option value="1080p">1080p</option>
										<option value="720p">720p</option>
										<option value="480p">480p</option>
									</select>
								</label>

								{#if session.probe && session.probe.audio_tracks.length > 0}
									<label class="ss-opt">
										<span>Audio</span>
										<select
											aria-label="Audio track"
											value={audioIdx}
											onchange={(e) => changeAudio(Number(e.currentTarget.value))}
										>
											{#each session.probe.audio_tracks as a (a.index)}
												<option value={a.index}>
													{formatAudioLabel(a.index, a.language, a.title, a.codec, a.channels)}
												</option>
											{/each}
										</select>
									</label>
								{/if}

								{#if session.probe && session.probe.subtitle_tracks.length > 0}
									<label class="ss-opt">
										<span>Subtitles</span>
										<select
											aria-label="Embedded subtitles"
											value={subIdx}
											onchange={(e) => (subIdx = Number(e.currentTarget.value))}
										>
											<option value={-1}>Off</option>
											{#each session.probe.subtitle_tracks as sub (sub.index)}
												<option value={sub.index}>
													{formatSubLabel(sub.index, sub.language, sub.title, sub.codec)}
												</option>
											{/each}
										</select>
									</label>
								{/if}

								<label class="ss-opt">
									<span>Speed</span>
									<select
										aria-label="Playback speed"
										value={playbackRate}
										onchange={(e) => changeSpeed(Number(e.currentTarget.value))}
									>
										<option value={0.75}>0.75x</option>
										<option value={1}>1x</option>
										<option value={1.25}>1.25x</option>
										<option value={1.5}>1.5x</option>
										<option value={2}>2x</option>
									</select>
								</label>
							{/if}
						</div>

						<div class="ss-opt-actions">
							{#if playMode === 'mpv' && session.files.length > 0}
								<button
									type="button"
									class="ss-btn ss-btn-sm"
									onclick={() => {
										playMode = 'browser';
										attachedSessionKey = '';
										void pollSession(session!.id);
									}}
								>
									Play in Browser
								</button>
							{/if}

							{#if mpvAvailable && session.files.length > 0}
								<button
									type="button"
									class="ss-btn ss-btn-sm"
									onclick={() => void triggerMPV(session!.id)}
								>
									<Icon name="play" size={14} />
									<span>{session.mpv_running ? 'Relaunch mpv' : 'Open in mpv'}</span>
								</button>
							{/if}

							<button type="button" class="ss-btn ss-btn-sm" onclick={() => void copyStreamURL()}>
								<Icon name="copy" size={14} />
								<span>Copy URL</span>
							</button>

							<button
								type="button"
								class="ss-btn ss-btn-sm"
								class:ss-btn-on={session.keep_file}
								onclick={() => void toggleKeep()}
								title="Save completed video into /mnt/D/Media/downloads on stop"
							>
								<Icon name="download" size={14} />
								<span>{session.keep_file ? 'Keeping File' : 'Keep File'}</span>
							</button>

							<button type="button" class="ss-btn ss-btn-sm ss-btn-danger" onclick={() => void stopActiveSession()}>
								<Icon name="x" size={14} />
								<span>Stop</span>
							</button>
						</div>
					</div>
				{/if}
			</section>
		{/if}

		<!-- Candidates List -->
		<section class="ss-results">
			{#if searchError}
				<div class="ss-err-banner">
					<Icon name="alert" size={16} />
					<span>{searchError}</span>
				</div>
			{/if}

			{#if searching && candidates.length === 0}
				<p class="ss-hint">Searching enabled torrent sources…</p>
			{:else if availableSources.length === 0 && !searching}
				<EmptyState icon="source" title="No torrent sources available" text="Enable at least one torrent source in Settings to stream directly." />
			{:else if filteredCandidates.length === 0}
				<div class="ss-empty-filter">
					<p>
						{#if candidates.length > 0}
							{candidates.length} release{candidates.length === 1 ? '' : 's'} found, none matched the current filter.
						{:else}
							No torrent releases found for this search.
						{/if}
					</p>
					{#if candidates.length > 0 && (onlySeeded || resFilter !== 'all')}
						<button
							type="button"
							class="ss-btn ss-btn-sm"
							onclick={() => {
								onlySeeded = false;
								resFilter = 'all';
							}}
						>
							Show all {candidates.length} releases
						</button>
					{/if}
				</div>
			{:else}
				<div class="ss-list-head">
					<span>{filteredCandidates.length} release{filteredCandidates.length === 1 ? '' : 's'}</span>
					{#if onlySeeded && candidates.length > filteredCandidates.length}
						<button type="button" class="ss-link-btn" onclick={() => (onlySeeded = false)}>
							Include {candidates.length - filteredCandidates.length} with 0 seeds
						</button>
					{/if}
				</div>

				<ul class="ss-list">
					{#each filteredCandidates as c, i (candKey(c) + ':' + i)}
						{@const isActive = activeKey === candKey(c)}
						<li class="ss-row" class:active={isActive}>
							<div class="ss-row-info">
								<div class="ss-row-title" title={c.title}>{c.title}</div>
								<div class="ss-row-meta">
									<span class="ss-tag ss-tag-src">{c.source_name}</span>
									{#if c.resolution}<span class="ss-tag ss-tag-res">{c.resolution}</span>{/if}
									{#if c.hdr}<span class="ss-tag ss-tag-hdr">{c.hdr}</span>{/if}
									{#if c.codec}<span class="ss-tag">{c.codec}</span>{/if}
									{#if c.audio}<span class="ss-tag">{c.audio}</span>{/if}
									{#if c.source_tag}<span class="ss-tag">{c.source_tag}</span>{/if}
									{#if c.is_pack}<span class="ss-tag">Pack</span>{/if}
									<span class="ss-meta-num">{bytes(c.size)}</span>
									<span class="ss-meta-seed" class:zero={c.seeders <= 0}>
										{c.seeders} seed{c.seeders === 1 ? '' : 's'} · {c.peers} peer{c.peers === 1 ? '' : 's'}
									</span>
								</div>
							</div>

							<div class="ss-row-actions">
								<button
									type="button"
									class="ss-btn ss-btn-primary ss-btn-sm"
									onclick={() => void playCandidate(c, 'browser')}
									disabled={starting && isActive}
								>
									<Icon name="play" size={14} />
									<span>Play</span>
								</button>
								{#if mpvAvailable}
									<button
										type="button"
										class="ss-btn ss-btn-sm"
										onclick={() => void playCandidate(c, 'mpv')}
										disabled={starting && isActive}
									>
										<span>mpv</span>
									</button>
								{/if}
							</div>
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	</div>
</Sheet>

<style>
	.ss-wrap { display: flex; flex-direction: column; gap: 16px; }
	.ss-controls { display: flex; flex-direction: column; gap: 10px; }
	.ss-query-row { display: flex; flex-wrap: wrap; gap: 8px; align-items: end; }
	.ss-field { display: flex; flex-direction: column; gap: 4px; font: 600 12px/1 var(--font-ui); color: var(--text-muted); }
	.ss-field input, .ss-field select {
		height: 38px; padding: 0 12px; border: 1px solid var(--line); border-radius: 10px;
		background: var(--surface-2); color: var(--text); font: 500 14px/1 var(--font-ui);
	}
	.ss-compact { width: 82px; }
	.ss-grow { flex: 1; min-width: 180px; }

	.ss-sources { display: flex; flex-wrap: wrap; gap: 6px; }
	.ss-chip {
		display: inline-flex; align-items: center; gap: 6px; min-height: 30px; padding: 4px 12px;
		border: 1px solid var(--line); border-radius: 999px; background: transparent; color: var(--text-muted);
		font: 600 12.5px/1 var(--font-ui); cursor: pointer;
	}
	.ss-chip.active { background: var(--text); color: var(--bg); border-color: var(--text); }
	.ss-chip.warn { border-color: color-mix(in srgb, var(--fail) 50%, transparent); }
	.ss-chip-cnt { opacity: 0.78; font-variant-numeric: tabular-nums; }
	.ss-chip-err { color: var(--fail); font-weight: 700; }

	.ss-filter-bar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 10px; padding-top: 2px; }
	.ss-pills { display: flex; flex-wrap: wrap; gap: 6px; }
	.ss-pill {
		min-height: 30px; padding: 0 11px; border: 1px solid var(--line); border-radius: 999px;
		background: transparent; color: var(--text-muted); font: 600 12.5px/1 var(--font-ui); cursor: pointer;
	}
	.ss-pill.active { background: color-mix(in srgb, var(--text) 14%, transparent); color: var(--text); border-color: var(--line-strong); }
	.ss-filter-right { display: flex; flex-wrap: wrap; align-items: center; gap: 14px; }
	.ss-check { display: inline-flex; align-items: center; gap: 6px; font: 500 13px/1 var(--font-ui); color: var(--text-muted); cursor: pointer; }
	.ss-sort { display: inline-flex; align-items: center; gap: 6px; font: 600 12.5px/1 var(--font-ui); color: var(--text-muted); }
	.ss-sort select {
		height: 32px; padding: 0 10px; border: 1px solid var(--line); border-radius: 8px;
		background: var(--surface-2); color: var(--text); font: 600 12.5px/1 var(--font-ui);
	}

	.ss-player-card {
		display: flex; flex-direction: column; gap: 10px; padding: 14px; border-radius: 14px;
		background: var(--surface-2); border: 1px solid var(--line);
	}
	.ss-telemetry { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px; }
	.ss-tel-main { display: flex; align-items: center; gap: 8px; min-width: 0; flex: 1; }
	.ss-rel-name { font: 600 13.5px/1.3 var(--font-ui); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.ss-tel-stats { display: flex; flex-wrap: wrap; gap: 6px; font: 500 12.5px/1.2 var(--font-ui); color: var(--text-muted); font-variant-numeric: tabular-nums; }

	.ss-badge { display: inline-flex; align-items: center; height: 22px; padding: 0 9px; border-radius: 999px; font: 700 11px/1 var(--font-ui); flex: none; }
	.ss-state-resolving, .ss-state-buffering { background: color-mix(in srgb, var(--accent) 22%, transparent); color: var(--text); }
	.ss-state-ready { background: color-mix(in srgb, #22c55e 22%, transparent); color: #4ade80; }
	.ss-state-error { background: color-mix(in srgb, var(--fail) 24%, transparent); color: var(--fail); }

	.ss-video-box { position: relative; width: 100%; aspect-ratio: 16 / 9; background: #000; border-radius: 10px; overflow: hidden; }
	.ss-video { width: 100%; height: 100%; display: block; background: #000; }
	.ss-video-overlay {
		position: absolute; inset: 0; z-index: 2; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px;
		background: rgb(0 0 0 / 62%); color: #fff; font: 500 13.5px/1.3 var(--font-ui); pointer-events: none;
	}
	.ss-spinner {
		width: 26px; height: 26px; border-radius: 50%; border: 2.5px solid rgb(255 255 255 / 25%);
		border-top-color: #fff; animation: ss-spin 0.8s linear infinite;
	}
	@keyframes ss-spin { to { transform: rotate(360deg); } }

	.ss-seek-bar { display: flex; align-items: center; gap: 10px; }
	.ss-seek-bar input[type='range'] { flex: 1; accent-color: var(--accent); cursor: pointer; }
	.ss-time { font: 600 12px/1 var(--font-ui); color: var(--text-muted); font-variant-numeric: tabular-nums; min-width: 44px; text-align: center; }

	.ss-mpv-status { display: flex; align-items: center; gap: 10px; padding: 12px 14px; border-radius: 10px; background: color-mix(in srgb, var(--text) 6%, transparent); font: 500 13.5px/1.4 var(--font-ui); }

	.ss-options-bar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 10px; }
	.ss-opt-group { display: flex; flex-wrap: wrap; gap: 8px; }
	.ss-opt { display: inline-flex; align-items: center; gap: 6px; font: 600 12px/1 var(--font-ui); color: var(--text-muted); }
	.ss-opt select {
		height: 32px; max-width: 220px; padding: 0 9px; border: 1px solid var(--line); border-radius: 8px;
		background: var(--surface-1); color: var(--text); font: 500 12.5px/1 var(--font-ui);
	}
	.ss-opt-actions { display: flex; flex-wrap: wrap; gap: 6px; }

	.ss-btn {
		display: inline-flex; align-items: center; justify-content: center; gap: 6px; height: 38px; padding: 0 14px;
		border: 1px solid var(--line); border-radius: 10px; background: var(--surface-1); color: var(--text);
		font: 600 13.5px/1 var(--font-ui); cursor: pointer; white-space: nowrap;
	}
	.ss-btn:hover:not(:disabled) { border-color: var(--line-strong); background: color-mix(in srgb, var(--text) 10%, var(--surface-1)); }
	.ss-btn:disabled { opacity: 0.55; cursor: not-allowed; }
	.ss-btn-primary { background: var(--text); color: var(--bg); border-color: var(--text); }
	.ss-btn-primary:hover:not(:disabled) { opacity: 0.9; background: var(--text); }
	.ss-btn-sm { height: 32px; padding: 0 11px; border-radius: 8px; font-size: 12.5px; }
	.ss-btn-on { border-color: var(--accent); color: var(--accent); }
	.ss-btn-danger { color: var(--fail); border-color: color-mix(in srgb, var(--fail) 35%, transparent); }

	.ss-err-banner {
		display: flex; align-items: center; gap: 8px; padding: 10px 12px; border-radius: 10px;
		background: color-mix(in srgb, var(--fail) 14%, transparent); color: var(--fail); font: 500 13px/1.4 var(--font-ui);
	}
	.ss-hint, .ss-empty-filter { padding: 18px 8px; text-align: center; color: var(--text-muted); font: 500 14px/1.4 var(--font-ui); }
	.ss-empty-filter { display: flex; flex-direction: column; align-items: center; gap: 10px; }

	.ss-list-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; font: 600 12.5px/1 var(--font-ui); color: var(--text-muted); }
	.ss-link-btn { border: 0; background: none; padding: 0; color: var(--accent); font: inherit; cursor: pointer; text-decoration: underline; }

	.ss-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; }
	.ss-row {
		display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 11px 13px;
		border: 1px solid var(--line); border-radius: 12px; background: color-mix(in srgb, var(--text) 3%, transparent);
	}
	.ss-row.active { border-color: var(--accent); background: color-mix(in srgb, var(--accent) 9%, transparent); }
	.ss-row-info { min-width: 0; flex: 1; display: flex; flex-direction: column; gap: 6px; }
	.ss-row-title { font: 600 13.5px/1.35 var(--font-ui); word-break: break-word; }
	.ss-row-meta { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; font: 500 12px/1.2 var(--font-ui); color: var(--text-muted); }
	.ss-tag { padding: 2px 7px; border-radius: 6px; background: color-mix(in srgb, var(--text) 9%, transparent); color: var(--text); font-weight: 600; }
	.ss-tag-src { background: color-mix(in srgb, var(--accent) 18%, transparent); }
	.ss-tag-res { background: color-mix(in srgb, #38bdf8 18%, transparent); }
	.ss-tag-hdr { background: color-mix(in srgb, #f59e0b 20%, transparent); }
	.ss-meta-num, .ss-meta-seed { font-variant-numeric: tabular-nums; }
	.ss-meta-seed { color: #4ade80; font-weight: 600; }
	.ss-meta-seed.zero { color: var(--text-faint); font-weight: 500; }
	.ss-row-actions { display: flex; gap: 6px; flex: none; }

	@media (max-width: 640px) {
		.ss-row { flex-direction: column; align-items: stretch; }
		.ss-row-actions { justify-content: flex-end; }
	}
</style>
