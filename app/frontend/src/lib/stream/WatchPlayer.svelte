<script lang="ts">
	import type HlsType from 'hls.js';
	import { onDestroy, onMount } from 'svelte';
	import { goto, replaceState } from '$app/navigation';
	import { api, message } from '#lib/core/api.ts';
	import { to } from '#lib/core/paths.ts';
	import { toasts } from '#lib/core/toasts.svelte.ts';
	import { sized } from '#lib/core/tmdb-image.ts';
	import { detailPath, loadDetails, type TitleDetails } from '#lib/metadata/index.ts';
	import {
		ensureStreamHLS,
		fetchStreamSubtitle,
		formatEndsAt,
		formatTimeLeft,
		getStreamStatus,
		getTitleWatchHistory,
		launchStreamMPV,
		listStreamSources,
		saveWatchProgress,
		searchStreamReleases,
		searchStreamSubtitles,
		selectStreamFile,
		setStreamKeep,
		startStream,
		stopStream,
		uploadStreamSubtitle,
		type SessionStatus,
		type StreamCandidate,
		type StreamSubCandidate,
		type SubPref,
		type WatchEntry
	} from './stream-api.ts';
	import {
		getSkipSegments,
		getStreamingPrefs,
		postScrobble,
		saveStreamingPrefs,
		type SkipSegment,
		type StreamPrefs,
		type SubStylePrefs
	} from './streaming-config-api.ts';
	import { attachHlsMedia, buildWatchEntryPayload } from './player-media.ts';
	import { resolveSubtitleStyle, stripSdhCueLines } from './subtitle-style.ts';
	import { activeCuesAt, parseWebVTT, type VTTCue } from './vtt.ts';
	import PlayerHud from './PlayerHud.svelte';
	import PlayerLandingOverlay from './PlayerLandingOverlay.svelte';
	import PlayerSourcesDrawer from './PlayerSourcesDrawer.svelte';
	import PlayerSubsDrawer from './PlayerSubsDrawer.svelte';
	import PlayerEpisodesDrawer from './PlayerEpisodesDrawer.svelte';

	interface SeasonEp {
		number: number;
		name?: string;
		overview?: string;
		runtime?: number;
		still?: string;
		air_day?: string;
	}

	let {
		kind,
		tmdb,
		initialSeason = 1,
		initialEpisode = 1,
		autoResume = true,
		initialDrawer = 'none',
		backHref = ''
	}: {
		kind: 'movie' | 'show';
		tmdb: string;
		initialSeason?: number;
		initialEpisode?: number;
		autoResume?: boolean;
		initialDrawer?: 'none' | 'sources';
		backHref?: string;
	} = $props();

	let stageEl = $state<HTMLElement | undefined>(undefined);
	let videoEl = $state<HTMLVideoElement | undefined>(undefined);
	let details = $state<TitleDetails | null>(null);
	let loadingPage = $state(true);
	let pageError = $state('');

	let season = $state(1);
	let drawerSeason = $state(1);
	let episode = $state(1);
	let seasonEpisodes = $state<Record<number, SeasonEp[]>>({});
	let loadingEpisodes = $state(false);
	let historyLatest = $state<WatchEntry | null>(null);
	let historyEpisodes = $state<WatchEntry[]>([]);
	let prefs = $state<StreamPrefs | null>(null);
	let subStyle = $state<SubStylePrefs>({
		color: '#ffffff',
		bg_color: '#000000',
		bg_opacity: 0.65,
		size: 1.0,
		offset_pct: 6,
		hdr_dimmed: true,
		hdr_brightness: 40,
		strip_sdh: false,
		font_family: 'sans'
	});

	let mpvAvailable = $state(false);
	let ffmpegAvailable = $state(false);
	let queryText = $state('');
	let searching = $state(false);
	let searchError = $state('');
	let candidates = $state<StreamCandidate[]>([]);
	let counts = $state({ local: 0, instant: 0, debrid: 0, torrent: 0 });
	let sourceTab = $state<'all' | 'instant' | 'local' | 'debrid' | 'torrent'>('all');
	let resFilter = $state<'all' | '2160p' | '1080p' | '720p' | 'sd'>('all');

	let session = $state<SessionStatus | null>(null);
	let activeCandidate = $state<StreamCandidate | null>(null);
	let fallbackNotice = $state('');
	let triedCandKeys = new Set<string>();
	let autoFallbacks = 0;
	let starting = $state(false);
	let playMode = $state<'browser' | 'mpv'>('browser');
	let pendingMPV = $state(false);
	let pendingStartSec = $state(0);
	let pendingFileIdx = $state<number | null>(null);

	let browserQuality = $state<'original' | '1080p' | '720p' | '480p'>('original');
	let audioIdx = $state(0);
	let fitMode = $state<'contain' | 'cover'>('contain');
	let playbackRate = $state(1);
	let volume = $state(1);
	let muted = $state(false);
	let paused = $state(true);
	let isFullscreen = $state(false);

	let seekOffset = $state(0);
	let videoTime = $state(0);
	let videoDuration = $state(0);
	let bufferedEnd = $state(0);
	let isScrubbing = $state(false);
	let scrubValue = $state(0);
	let pipelineMode = $state<'none' | 'direct' | 'hls'>('none');
	let loadingMedia = $state(false);
	let playerError = $state('');

	let drawer = $state<'none' | 'sources' | 'audioSubs' | 'episodes'>('none');
	let hudVisible = $state(true);
	let feedbackText = $state('');

	let subMode = $state<'off' | 'embedded' | 'site' | 'upload'>('off');
	let subIndex = $state(0);
	let subLang = $state('');
	let subLabel = $state('');
	let subSiteId = $state('');
	let subSiteKey = $state('');
	let subFile = $state('');
	let subDelayMS = $state(0);
	let subSize = $state<'sm' | 'md' | 'lg' | 'xl'>('md');
	let parsedCues = $state<VTTCue[]>([]);
	let siteSubLangsInput = $state('en, tr');
	let siteSubCandidates = $state<StreamSubCandidate[]>([]);
	let searchingSubs = $state(false);
	let loadingSub = $state(false);
	let subError = $state('');

	let skipSegments = $state<SkipSegment[]>([]);
	let autoSkipDismissed = $state<Record<string, boolean>>({});
	let skipCountdown = $state(0);
	let nextCountdown = $state(10);

	let hlsInstance: HlsType | null = null;
	let pollTimer: ReturnType<typeof setInterval> | null = null;
	let progressTimer: ReturnType<typeof setInterval> | null = null;
	let scrobbleTimer: ReturnType<typeof setInterval> | null = null;
	let skipTimer: ReturnType<typeof setInterval> | null = null;
	let hudTimer: ReturnType<typeof setTimeout> | null = null;
	let feedbackTimer: ReturnType<typeof setTimeout> | null = null;
	let nextTimer: ReturnType<typeof setInterval> | null = null;
	let attachedSessionKey = '';
	let loadReqSeq = 0;
	let didInitialResumeDecision = false;
	let destroyed = false;

	const extIds = $derived((details as unknown as { external_ids?: { imdb?: string } } | null)?.external_ids);
	const imdbId = $derived(extIds?.imdb ?? '');
	const regularSeasons = $derived.by(() => {
		const raw = details && 'seasons' in details && Array.isArray(details.seasons) ? (details.seasons as { number: number; name?: string }[]) : [];
		const nonZero = raw.filter((s) => s.number > 0);
		return nonZero.length > 0 ? nonZero : raw;
	});
	const curSeasonEps = $derived(seasonEpisodes[season] ?? []);
	const drawerEps = $derived(seasonEpisodes[drawerSeason] ?? []);
	const curEpObj = $derived(curSeasonEps.find((e) => e.number === episode));
	const activeSaved = $derived.by(() =>
		kind === 'show' ? (historyEpisodes.find((h) => (h.season ?? 0) === season && (h.episode ?? 0) === episode) ?? null) : historyLatest
	);
	const episodeProgressMap = $derived.by(() => {
		const map: Record<string, WatchEntry> = {};
		for (const h of historyEpisodes) map[`${h.season ?? 0}:${h.episode ?? 0}`] = h;
		return map;
	});
	const nextEpisodeTarget = $derived.by(() => {
		if (kind !== 'show') return null;
		const idx = curSeasonEps.findIndex((e) => e.number === episode);
		if (idx >= 0 && idx + 1 < curSeasonEps.length) return { season, episode: curSeasonEps[idx + 1].number };
		const sIdx = regularSeasons.findIndex((s) => s.number === season);
		if (sIdx >= 0 && sIdx + 1 < regularSeasons.length) return { season: regularSeasons[sIdx + 1].number, episode: 1 };
		return null;
	});
	const backdropUrl = $derived(details?.backdrop ? sized(details.backdrop, 'backdrop') : activeSaved?.backdrop ? sized(activeSaved.backdrop, 'backdrop') : '');
	const headerTitle = $derived(details?.name || activeSaved?.title || 'Watch');
	const headerSub = $derived(kind === 'show' ? `S${String(season).padStart(2, '0')}E${String(episode).padStart(2, '0')}${curEpObj?.name ? ` · ${curEpObj.name}` : ''}` : '');
	const totalDuration = $derived(session?.probe?.duration_sec || videoDuration || activeSaved?.duration_sec || 0);
	const currentPos = $derived(isScrubbing ? scrubValue : pipelineMode === 'hls' ? seekOffset + videoTime : videoTime);
	const remainingText = $derived(formatTimeLeft(currentPos, totalDuration));
	const endsAtText = $derived(formatEndsAt(currentPos, totalDuration, playbackRate));
	const scrubbedPct = $derived(totalDuration > 0 ? Math.min(100, Math.max(0, (currentPos / totalDuration) * 100)) : 0);
	const bufferedPct = $derived(totalDuration > 0 ? Math.min(100, Math.max(scrubbedPct, ((pipelineMode === 'hls' ? seekOffset + bufferedEnd : bufferedEnd) / totalDuration) * 100)) : 0);
	const resolvedSubCSS = $derived(resolveSubtitleStyle(subStyle));
	const visibleCues = $derived.by(() => {
		const cues = subMode === 'off' || parsedCues.length === 0 ? [] : activeCuesAt(parsedCues, currentPos, subDelayMS);
		if (!subStyle.strip_sdh) return cues;
		return cues.map((c) => ({ ...c, lines: stripSdhCueLines(c.lines) })).filter((c) => c.lines.length > 0);
	});
	const activeSkip = $derived(pipelineMode === 'none' ? null : (skipSegments.find((s) => currentPos >= s.start && currentPos < s.end - 1) ?? null));
	const nearEpisodeEnd = $derived(kind === 'show' && !!nextEpisodeTarget && pipelineMode !== 'none' && totalDuration > 180 && totalDuration - currentPos <= 25 && currentPos > 30);

	function candKey(c: StreamCandidate): string { return c.id || `${c.source_id}:${c.link}`; }
	function showFeedback(text: string) {
		feedbackText = text;
		if (feedbackTimer) clearTimeout(feedbackTimer);
		feedbackTimer = setTimeout(() => { feedbackText = ''; }, 900);
	}
	function wakeHud() {
		hudVisible = true;
		if (hudTimer) clearTimeout(hudTimer);
		if (!paused && drawer === 'none') hudTimer = setTimeout(() => { hudVisible = false; }, 2800);
	}
	function currentSubPref(): SubPref {
		return { mode: subMode, language: subLang, label: subLabel, index: subIndex, site_id: subSiteId, site_key: subSiteKey, delay_ms: subDelayMS, size: subSize };
	}

	async function persistProgress(posOverride?: number, keepalive = false, completedOverride?: boolean) {
		const entry = buildWatchEntryPayload({
			kind, tmdb, imdbId, title: headerTitle, year: details?.year ?? 0,
			poster: details?.poster ?? '', backdrop: details?.backdrop ?? '',
			season, episode, episodeName: curEpObj?.name ?? '', pos: posOverride ?? currentPos,
			dur: totalDuration, completedOverride, activeCandidate, session, saved: activeSaved,
			browserQuality, audioIdx, playbackRate, subPref: currentSubPref()
		});
		if (!entry) return;
		try {
			const saved = await saveWatchProgress(entry, keepalive);
			if (destroyed) return;
			if (kind === 'movie') historyLatest = saved;
			else {
				const idx = historyEpisodes.findIndex((h) => h.key === saved.key);
				historyEpisodes = idx >= 0 ? historyEpisodes.map((h, i) => (i === idx ? saved : h)) : [saved, ...historyEpisodes];
			}
		} catch { /* Ignore transient progress save failures. */ }
	}

	async function sendScrobble(action: 'start' | 'pause' | 'stop') {
		const idNum = Number(tmdb);
		if (!idNum) return;
		const pct = totalDuration > 0 ? Math.min(100, Math.max(0, (currentPos / totalDuration) * 100)) : 0;
		try {
			await postScrobble({ action, kind, tmdb: idNum, imdb: imdbId, season: kind === 'show' ? season : 0, episode: kind === 'show' ? episode : 0, progress: pct });
		} catch { /* Non-fatal telemetry. */ }
	}

	function destroyVideoPipeline() {
		loadingMedia = false;
		videoDuration = 0;
		if (hlsInstance) { hlsInstance.destroy(); hlsInstance = null; }
		if (videoEl) { videoEl.pause(); videoEl.removeAttribute('src'); videoEl.load(); }
		pipelineMode = 'none';
	}
	function stopPolling() { if (pollTimer) { clearInterval(pollTimer); pollTimer = null; } }
	function startPolling(id: string) { stopPolling(); pollTimer = setInterval(() => void pollSession(id), 1200); }

	async function fetchSkips(sessId = '') {
		const animeHint = (details?.genres ?? []).some((g) => g.toLowerCase().includes('anim'));
		try {
			const res = await getSkipSegments({ kind, imdb: imdbId, tmdb, season: kind === 'show' ? season : 0, episode: kind === 'show' ? episode : 0, title: headerTitle, anime: animeHint, session_id: sessId });
			if (!destroyed) skipSegments = res.segments ?? [];
		} catch { /* Optional skip segments. */ }
	}

	async function pollSession(id: string) {
		try {
			const st = await getStreamStatus(id);
			if (destroyed || !session || session.id !== id) return;
			session = st;
			if (st.state === 'error') { pendingMPV = false; return; }
			if (pendingMPV && ((st.transport && st.transport !== 'torrent') || (st.files.length > 0 && st.state !== 'resolving'))) {
				pendingMPV = false;
				await triggerMPV(st.id, pendingStartSec);
				return;
			}
			if (playMode === 'browser' && st.state === 'ready' && !st.mpv_running) {
				const key = `${st.id}:${st.selected_file}`;
				if (attachedSessionKey !== key) {
					attachedSessionKey = key;
					if (pendingFileIdx !== null && pendingFileIdx !== st.selected_file && pendingFileIdx < st.files.length) {
						const wanted = pendingFileIdx; pendingFileIdx = null;
						await selectStreamFile(st.id, wanted);
						return;
					}
					pendingFileIdx = null;
					await attachPlayback(pendingStartSec);
					if (st.chapters?.length) void fetchSkips(st.id);
					if (subMode === 'embedded' && st.probe?.subtitle_tracks.some((t) => t.index === subIndex)) await loadEmbeddedSubtitle(subIndex, true);
				} else if (pipelineMode === 'direct' && st.probe && !st.probe.direct_playable && ffmpegAvailable) {
					await attachPlayback(currentPos, true);
				}
			}
		} catch { /* Transient poll error. */ }
	}

	async function loadSeasonEpisodes(sNum: number) {
		if (kind !== 'show' || !tmdb || seasonEpisodes[sNum]) return;
		loadingEpisodes = true;
		try {
			const res = await api.get<{ episodes?: SeasonEp[] }>(`/metadata/show/${encodeURIComponent(tmdb)}/seasons/${sNum}`);
			if (!destroyed) seasonEpisodes = { ...seasonEpisodes, [sNum]: res.episodes ?? [] };
		} catch { /* Ignore metadata episode list errors. */ } finally { loadingEpisodes = false; }
	}

	async function loadInitial() {
		loadingPage = true; pageError = '';
		try {
			const [d, hist, srcs, pf] = await Promise.all([
				loadDetails(api.get, kind, tmdb),
				getTitleWatchHistory(kind, tmdb).catch(() => ({ latest: null, episodes: [] })),
				listStreamSources(kind),
				getStreamingPrefs().catch(() => null)
			]);
			if (destroyed) return;
			details = d ?? null;
			historyLatest = hist.latest;
			historyEpisodes = hist.episodes ?? [];
			mpvAvailable = srcs.mpv_available;
			ffmpegAvailable = srcs.ffmpeg_available;
			if (pf) { prefs = pf; subStyle = { ...pf.sub_style }; }
			queryText = d?.name || hist.latest?.title || '';

			if (kind === 'show') {
				const sDef = initialSeason > 0 ? initialSeason : hist.latest?.season && hist.latest.season > 0 ? hist.latest.season : regularSeasons[0]?.number ?? 1;
				const eDef = initialEpisode > 0 ? initialEpisode : hist.latest?.episode && hist.latest.episode > 0 ? hist.latest.episode : 1;
				season = sDef; drawerSeason = sDef; episode = eDef;
				await loadSeasonEpisodes(sDef);
			}
			const sv = kind === 'show' ? (historyEpisodes.find((h) => (h.season ?? 0) === season && (h.episode ?? 0) === episode) ?? null) : historyLatest;
			applySavedPrefs(sv);
			void fetchSkips();
			await runSearch();

			if (!didInitialResumeDecision) {
				didInitialResumeDecision = true;
				if (initialDrawer === 'sources') {
					drawer = 'sources';
				} else if (autoResume && sv && !sv.completed) {
					if (sv.release_link) void resumeFromSaved(sv, sv.position_sec);
					else {
						const b = pickBestCandidate();
						if (b && (b.instant || prefs?.auto_play_fastest)) void startCandidate(b, 'browser', sv.position_sec, true);
					}
				} else if (prefs?.auto_play_fastest) {
					const b = pickBestCandidate();
					if (b && b.instant) void startCandidate(b, 'browser', 0, true);
				}
			}
		} catch (e) { pageError = message(e); } finally { loadingPage = false; wakeHud(); }
	}

	function applySavedPrefs(sv: WatchEntry | null) {
		if (!sv) return;
		if (sv.quality === '1080p' || sv.quality === '720p' || sv.quality === '480p' || sv.quality === 'original') browserQuality = sv.quality;
		audioIdx = sv.audio_index ?? 0;
		playbackRate = sv.playback_rate && sv.playback_rate > 0 ? sv.playback_rate : 1;
		if (sv.subtitle) {
			subDelayMS = sv.subtitle.delay_ms ?? 0;
			if (sv.subtitle.size === 'sm' || sv.subtitle.size === 'md' || sv.subtitle.size === 'lg' || sv.subtitle.size === 'xl') subSize = sv.subtitle.size;
			subLang = sv.subtitle.language ?? ''; subLabel = sv.subtitle.label ?? '';
			subSiteId = sv.subtitle.site_id ?? ''; subSiteKey = sv.subtitle.site_key ?? '';
			subIndex = sv.subtitle.index ?? 0;
			subMode = sv.subtitle.mode === 'embedded' ? 'embedded' : 'off';
		}
	}

	async function runSearch() {
		const q = (queryText || details?.name || activeSaved?.title || '').trim();
		if (!q && !imdbId && !tmdb) return;
		searching = true; searchError = '';
		try {
			const res = await searchStreamReleases({
				query: q,
				kind,
				year: details?.year || activeSaved?.year || 0,
				season: kind === 'show' ? season : 0,
				episode: kind === 'show' ? episode : 0,
				imdb: imdbId,
				tmdb: Number(tmdb) || 0,
				original_title: details?.original_title,
				genres: details?.genres,
				original_language: details?.original_language,
				origin_country: details?.origin_country
			});
			if (destroyed) return;
			candidates = res.candidates ?? [];
			counts = res.counts ?? { local: 0, instant: 0, debrid: 0, torrent: candidates.length };
		} catch (e) { searchError = message(e); candidates = []; } finally { searching = false; }
	}

	function pickBestCandidate(): StreamCandidate | null { return candidates.length > 0 ? candidates[0] : null; }

	async function resumeFromSaved(sv: WatchEntry, startAt: number) {
		if (!sv.release_link) return;
		const synthetic: StreamCandidate = {
			source_id: sv.source_id ?? 0, source_name: sv.source_name || 'Saved Source', title: sv.release_title || sv.title,
			link: sv.release_link, info_hash: sv.info_hash || '', protocol: 'torrent', size: 0, seeders: 1, peers: 0,
			resolution: sv.resolution || '', resolution_num: 0, source_tag: '', codec: '', hdr: '', audio: '', languages: []
		};
		pendingFileIdx = typeof sv.file_index === 'number' && sv.file_index > 0 ? sv.file_index : null;
		await startCandidate(synthetic, 'browser', startAt, true);
		if (sv.subtitle?.mode === 'site' && sv.subtitle.site_id) await loadSiteSubtitleById(sv.subtitle.site_id, sv.subtitle.language ?? '', sv.subtitle.label ?? '', true);
	}

	async function startCandidate(c: StreamCandidate, mode: 'browser' | 'mpv', startAt = 0, keepExternalSubs = true) {
		if (pipelineMode !== 'none' && currentPos > 5) await persistProgress(currentPos);
		destroyVideoPipeline(); stopPolling();
		session = null; attachedSessionKey = ''; activeCandidate = c; triedCandKeys.add(candKey(c));
		playMode = mode; pendingMPV = mode === 'mpv'; pendingStartSec = Math.max(0, startAt);
		starting = true; playerError = ''; seekOffset = pendingStartSec; videoTime = pendingStartSec;
		if (!keepExternalSubs || subMode === 'embedded') { subMode = 'off'; parsedCues = []; subFile = ''; }

		try {
			const st = await startStream({
				candidate_id: c.id, local_file_id: c.local_file_id, debrid_provider: c.debrid_provider,
				source_id: c.source_id, source_name: c.provider || c.source_name, link: c.link,
				title: headerSub ? `${headerTitle} · ${headerSub}` : headerTitle, release_title: c.release_name || c.title,
				kind, season: kind === 'show' ? season : 0, episode: kind === 'show' ? episode : 0, keep_file: false,
				tmdb: Number(tmdb) || 0, imdb: imdbId
			});
			if (destroyed) return;
			session = st; startPolling(st.id);
			if (st.state === 'ready' || (pendingMPV && st.state !== 'resolving')) void pollSession(st.id);
			wakeHud();
		} catch (e) {
			pendingMPV = false; playerError = message(e);
			toasts.push('Could not start stream', 'fail', { body: playerError });
			tryFallbackSource();
		} finally { starting = false; }
	}

	function tryFallbackSource() {
		if ((prefs && !prefs.auto_fallback) || autoFallbacks >= 5) return;
		const next = candidates.find((c) => !triedCandKeys.has(candKey(c)) && (c.instant || c.seeders > 0));
		if (!next) return;
		autoFallbacks++;
		fallbackNotice = `Switching to fallback: ${next.provider || next.source_name}`;
		void startCandidate(next, playMode, currentPos > 10 ? currentPos : 0, true);
	}

	async function attachPlayback(startSec: number, forceHLS = false) {
		const st = session;
		if (!st || !videoEl) return;
		const req = ++loadReqSeq;
		destroyVideoPipeline(); loadReqSeq = req; loadingMedia = true; playerError = ''; seekOffset = startSec; videoTime = 0;

		try {
			const tr = st.transport ?? 'torrent';
			const playURL = to(st.direct_url || st.media_url);
			const isCancelled = () => req !== loadReqSeq || destroyed || !videoEl;
			const hlsOpts = (url: string, at: number) => attachHlsMedia({
				video: videoEl!, srcUrl: url, startSec: at, playbackRate, volume, muted, isCancelled,
				onFatalError: (d) => { playerError = `Playback error (${d})`; tryFallbackSource(); },
				onStarted: () => void sendScrobble('start')
			});

			if (tr === 'hls_remote') {
				pipelineMode = 'hls'; seekOffset = 0;
				hlsInstance = await hlsOpts(playURL, startSec);
				return;
			}
			const canDirect = !forceHLS && browserQuality === 'original' && audioIdx === 0 && !st.needs_transcode && (tr === 'local' || tr === 'direct' ? st.probe?.direct_playable !== false : !!st.probe?.direct_playable);
			if (canDirect) {
				pipelineMode = 'direct';
				if (startSec > 0.5) videoEl.addEventListener('loadedmetadata', () => { if (videoEl) videoEl.currentTime = startSec; }, { once: true });
				videoEl.src = playURL; videoEl.playbackRate = playbackRate; videoEl.volume = volume; videoEl.muted = muted;
				await videoEl.play().catch(() => {});
				void sendScrobble('start'); wakeHud(); return;
			}
			if (!ffmpegAvailable) throw new Error('ffmpeg is not installed for transcoding');
			const hlsRes = await ensureStreamHLS(st.id, browserQuality, audioIdx, startSec);
			if (isCancelled() || !session || session.id !== st.id) return;
			seekOffset = hlsRes.start_sec; pipelineMode = 'hls';
			hlsInstance = await hlsOpts(to(hlsRes.playlist_url), 0);
		} catch (e) { if (req === loadReqSeq) { playerError = message(e); tryFallbackSource(); } } finally { if (req === loadReqSeq) loadingMedia = false; }
	}

	async function triggerMPV(id: string, atSec = currentPos) {
		destroyVideoPipeline(); playMode = 'mpv';
		try { session = await launchStreamMPV(id, { start_sec: atSec > 10 ? atSec : 0, sub_file: subFile }); }
		catch (e) { playerError = message(e); toasts.push('Could not open MPV', 'fail', { body: playerError }); }
	}

	function togglePlayPause() {
		if (!videoEl || pipelineMode === 'none') return;
		if (videoEl.paused) { void videoEl.play(); showFeedback('Play'); void sendScrobble('start'); }
		else { videoEl.pause(); showFeedback('Pause'); void persistProgress(); void sendScrobble('pause'); }
	}
	function seekBy(delta: number) {
		if (!session || pipelineMode === 'none') return;
		const next = Math.max(0, Math.min(totalDuration || currentPos + delta, currentPos + delta));
		showFeedback(`${delta > 0 ? '+' : ''}${delta}s`); commitSeek(next);
	}
	function commitSeek(target: number) {
		isScrubbing = false;
		if (!session || !videoEl) return;
		const t = Math.max(0, target);
		if (pipelineMode === 'direct' || session.transport === 'hls_remote') { videoEl.currentTime = t; videoTime = t; return; }
		void attachPlayback(t);
	}
	function cycleSpeed() {
		const speeds = [1, 1.25, 1.5, 2, 0.75];
		playbackRate = speeds[(speeds.indexOf(playbackRate) + 1) % speeds.length] ?? 1;
		if (videoEl) videoEl.playbackRate = playbackRate;
		showFeedback(`${playbackRate}x`);
	}
	function setVolumeLevel(v: number) { volume = Math.min(1, Math.max(0, v)); muted = volume === 0; if (videoEl) { videoEl.volume = volume; videoEl.muted = muted; } }
	function toggleMute() { muted = !muted; if (videoEl) videoEl.muted = muted; showFeedback(muted ? 'Muted' : 'Unmuted'); }
	async function toggleFullscreen() {
		if (typeof document === 'undefined' || !stageEl) return;
		try { if (document.fullscreenElement) await document.exitFullscreen(); else await stageEl.requestFullscreen(); } catch { /* Ignore fullscreen denial. */ }
	}

	function turnOffSubtitles() { subMode = 'off'; subLabel = ''; subSiteId = ''; subFile = ''; parsedCues = []; void persistProgress(); }
	async function loadEmbeddedSubtitle(idx: number, silent = false) {
		if (!session) return;
		loadingSub = true; subError = '';
		try {
			const res = await fetch(to(`/api/v1/stream/${encodeURIComponent(session.id)}/sub/${encodeURIComponent(session.token)}/${idx}`), { credentials: 'same-origin' });
			if (!res.ok) throw new Error(await res.text());
			parsedCues = parseWebVTT(await res.text()); subMode = 'embedded'; subIndex = idx; subSiteId = ''; subFile = '';
			const tr = session.probe?.subtitle_tracks.find((t) => t.index === idx);
			subLang = tr?.language ?? ''; subLabel = [tr?.language?.toUpperCase(), tr?.title].filter(Boolean).join(' · ') || `Track ${idx + 1}`;
			if (!silent) showFeedback(`Subs: ${subLabel}`);
			void persistProgress();
		} catch (e) { subError = message(e); } finally { loadingSub = false; }
	}
	async function searchSubtitlesOnline() {
		searchingSubs = true; subError = '';
		try {
			const res = await searchStreamSubtitles({
				kind, title: headerTitle, year: details?.year ?? 0, season: kind === 'show' ? season : 0, episode: kind === 'show' ? episode : 0,
				imdb: imdbId, tmdb, release_name: activeCandidate?.release_name || activeCandidate?.title || session?.release_title || '',
				languages: siteSubLangsInput.split(',').map((s) => s.trim()).filter(Boolean)
			});
			siteSubCandidates = res.candidates ?? [];
		} catch (e) { subError = message(e); } finally { searchingSubs = false; }
	}
	async function loadSiteSubtitleById(id: string, lang: string, label: string, silent: boolean) {
		loadingSub = true; subError = '';
		try {
			const res = await fetchStreamSubtitle(id, session?.id ?? '');
			parsedCues = parseWebVTT(res.vtt); subMode = 'site'; subSiteId = id; subLang = res.language || lang; subLabel = label || subLang.toUpperCase(); subFile = res.sub_file ?? '';
			if (!silent) showFeedback(`Subs: ${subLabel}`);
			void persistProgress();
		} catch (e) { subError = message(e); } finally { loadingSub = false; }
	}
	async function handleSubtitleFileUpload(e: Event) {
		const f = (e.currentTarget as HTMLInputElement).files?.[0];
		if (!f) return;
		loadingSub = true; subError = '';
		try {
			const res = await uploadStreamSubtitle(f.name, await f.text(), '', session?.id ?? '');
			parsedCues = parseWebVTT(res.vtt); subMode = 'upload'; subLabel = f.name; subFile = res.sub_file ?? '';
			showFeedback(`Subs: ${f.name}`);
		} catch (err) { subError = message(err); } finally { loadingSub = false; }
	}

	async function switchEpisode(nextSeason: number, nextEpisode: number, autoPlayBest = true) {
		if (kind !== 'show') return;
		if (pipelineMode !== 'none' && currentPos > 5) await persistProgress(currentPos);
		destroyVideoPipeline(); stopPolling();
		if (session) { void stopStream(session.id).catch(() => {}); session = null; }
		season = nextSeason; drawerSeason = nextSeason; episode = nextEpisode;
		attachedSessionKey = ''; pendingStartSec = 0; seekOffset = 0; videoTime = 0; drawer = 'none'; triedCandKeys.clear(); fallbackNotice = '';
		await loadSeasonEpisodes(nextSeason);
		const savedEp = episodeProgressMap[`${nextSeason}:${nextEpisode}`];
		if (savedEp) applySavedPrefs(savedEp);
		replaceState(to(`/watch/show/${encodeURIComponent(tmdb)}?s=${nextSeason}&e=${nextEpisode}${backHref ? `&from=${encodeURIComponent(backHref)}` : ''}`), {});
		void fetchSkips(); await runSearch();
		if (!autoPlayBest) return;
		if (savedEp?.release_link && !savedEp.completed && savedEp.position_sec > 10) { await resumeFromSaved(savedEp, savedEp.position_sec); return; }
		const best = pickBestCandidate();
		if (best) await startCandidate(best, 'browser', 0, true); else drawer = 'sources';
	}
	async function shuffleEpisode() {
		if (kind !== 'show' || curSeasonEps.length <= 1) return;
		const pool = curSeasonEps.filter((e) => e.number !== episode);
		const pick = pool[Math.floor(Math.random() * pool.length)];
		if (pick) await switchEpisode(season, pick.number, true);
	}
	function executeSkip(seg: SkipSegment) {
		if (!videoEl) return;
		if (skipTimer) { clearInterval(skipTimer); skipTimer = null; }
		skipCountdown = 0; autoSkipDismissed = { ...autoSkipDismissed, [seg.kind]: true };
		commitSeek(seg.end); showFeedback(`Skipped ${seg.kind}`);
	}
	function onKeyDown(e: KeyboardEvent) {
		const target = e.target as HTMLElement | null;
		if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT')) return;
		wakeHud();
		switch (e.key.toLowerCase()) {
			case ' ': case 'k': e.preventDefault(); togglePlayPause(); break;
			case 'arrowleft': case 'j': e.preventDefault(); seekBy(-10); break;
			case 'arrowright': case 'l': e.preventDefault(); seekBy(10); break;
			case 'm': toggleMute(); break;
			case 'f': void toggleFullscreen(); break;
			case 's': drawer = drawer === 'sources' ? 'none' : 'sources'; break;
			case 'e': if (kind === 'show') drawer = drawer === 'episodes' ? 'none' : 'episodes'; break;
			case 'escape': if (drawer !== 'none') drawer = 'none'; break;
		}
	}

	$effect(() => { if (drawer === 'audioSubs' && siteSubCandidates.length === 0 && !searchingSubs && details) void searchSubtitlesOnline(); });
	$effect(() => {
		if (nearEpisodeEnd && !nextTimer) {
			nextCountdown = 10;
			nextTimer = setInterval(() => { if (nextCountdown <= 1 && nextEpisodeTarget) { clearInterval(nextTimer!); nextTimer = null; void switchEpisode(nextEpisodeTarget.season, nextEpisodeTarget.episode, true); } else nextCountdown--; }, 1000);
		} else if (!nearEpisodeEnd && nextTimer) { clearInterval(nextTimer); nextTimer = null; }
	});
	$effect(() => {
		const seg = activeSkip;
		if (!seg || !prefs || autoSkipDismissed[seg.kind]) { if (skipTimer) { clearInterval(skipTimer); skipTimer = null; } skipCountdown = 0; return; }
		const wantAuto = (seg.kind === 'intro' && prefs.auto_skip_intro) || (seg.kind === 'recap' && prefs.auto_skip_recap) || (seg.kind === 'outro' && prefs.auto_skip_outro);
		if (!wantAuto || skipTimer) return;
		skipCountdown = 3;
		skipTimer = setInterval(() => { if (skipCountdown <= 1) executeSkip(seg); else skipCountdown--; }, 1000);
	});

	onMount(() => {
		void loadInitial();
		progressTimer = setInterval(() => { if (!paused && pipelineMode !== 'none' && currentPos > 5) void persistProgress(); }, 8000);
		scrobbleTimer = setInterval(() => { if (!paused && pipelineMode !== 'none' && currentPos > 10) void sendScrobble('start'); }, 60000);
		const onFs = () => { isFullscreen = !!document.fullscreenElement; };
		const onUnload = () => { if (pipelineMode !== 'none' && currentPos > 5) { void persistProgress(currentPos, true); void sendScrobble('stop'); } if (session && !session.mpv_running) void stopStream(session.id).catch(() => {}); };
		document.addEventListener('fullscreenchange', onFs);
		window.addEventListener('beforeunload', onUnload);
		return () => { document.removeEventListener('fullscreenchange', onFs); window.removeEventListener('beforeunload', onUnload); };
	});
	onDestroy(() => {
		destroyed = true;
		if (pipelineMode !== 'none' && currentPos > 5) { void persistProgress(currentPos, true); void sendScrobble('stop'); }
		if (progressTimer) clearInterval(progressTimer);
		if (scrobbleTimer) clearInterval(scrobbleTimer);
		if (skipTimer) clearInterval(skipTimer);
		if (hudTimer) clearTimeout(hudTimer);
		if (feedbackTimer) clearTimeout(feedbackTimer);
		if (nextTimer) clearInterval(nextTimer);
		stopPolling(); destroyVideoPipeline();
		if (session && !session.mpv_running) void stopStream(session.id).catch(() => {});
	});
</script>

<svelte:head><title>{headerTitle} · Watch · HolyArrs</title></svelte:head>
<svelte:window onkeydown={onKeyDown} />

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div bind:this={stageEl} class="wp-stage" class:hide-cursor={!hudVisible && !paused && drawer === 'none'} onmousemove={wakeHud} ontouchstart={wakeHud}>
	{#if backdropUrl && pipelineMode === 'none'}
		<img class="wp-backdrop" src={backdropUrl} alt="" />
		<div class="wp-backdrop-scrim"></div>
	{/if}

	<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
	<video
		bind:this={videoEl} class="wp-video" class:cover={fitMode === 'cover'} playsinline
		onclick={() => (drawer !== 'none' ? (drawer = 'none') : togglePlayPause())}
		ontimeupdate={() => { if (videoEl) { videoTime = videoEl.currentTime; if (videoEl.buffered.length > 0) bufferedEnd = videoEl.buffered.end(videoEl.buffered.length - 1); } }}
		onplay={() => { paused = false; wakeHud(); }}
		onpause={() => { paused = true; wakeHud(); }}
		onloadedmetadata={() => { if (videoEl && Number.isFinite(videoEl.duration) && videoEl.duration > 0) videoDuration = videoEl.duration; }}
		ondurationchange={() => { if (videoEl && Number.isFinite(videoEl.duration) && videoEl.duration > 0) videoDuration = videoEl.duration; }}
		onloadeddata={() => { if (pipelineMode === 'direct' && videoEl && videoEl.videoWidth === 0 && ffmpegAvailable && session) { showFeedback('Transcoding video...'); void attachPlayback(currentPos, true); } }}
		onended={() => { paused = true; void persistProgress(totalDuration, false, true); void sendScrobble('stop'); if (nextEpisodeTarget) void switchEpisode(nextEpisodeTarget.season, nextEpisodeTarget.episode, true); }}
		onerror={() => { if (pipelineMode === 'direct' && ffmpegAvailable && session) void attachPlayback(currentPos, true); else tryFallbackSource(); }}
	></video>

	{#if visibleCues.length > 0 && pipelineMode !== 'none'}
		<div class="wp-captions size-{subSize}" style:bottom={hudVisible || paused ? `calc(${resolvedSubCSS.bottomPct}% + 72px)` : `${resolvedSubCSS.bottomPct}%`}>
			{#each visibleCues as cue, idx (idx)}
				<div class="wp-cap-cue" style:color={resolvedSubCSS.color} style:background={resolvedSubCSS.background} style:font-family={resolvedSubCSS.fontFamilyCss}>
					{#each cue.lines as line, li (li)}{#if li > 0}<br />{/if}{line}{/each}
				</div>
			{/each}
		</div>
	{/if}

	{#if feedbackText}<div class="wp-ripple" aria-live="polite">{feedbackText}</div>{/if}

	{#if pipelineMode === 'none'}
		<PlayerLandingOverlay
			{loadingPage} {pageError} {playMode} {session} {loadingMedia} {headerTitle} {headerSub}
			overview={details?.overview ?? ''} {activeSaved} bestCandidate={pickBestCandidate()}
			candidatesCount={candidates.length} {starting} {searching} {playerError}
			onRetryLoad={() => void loadInitial()}
			onSwitchToBrowser={() => { playMode = 'browser'; void attachPlayback(currentPos); }}
			onResumeSaved={(pos) => { const b = pickBestCandidate(); if (b) void startCandidate(b, 'browser', pos, true); }}
			onPlayBest={() => { const b = pickBestCandidate(); if (b) void startCandidate(b, 'browser', 0, true); }}
			onOpenSources={() => { drawer = 'sources'; }}
		/>
	{/if}

	<PlayerHud
		{hudVisible} {drawer} {kind} {headerTitle} {headerSub} {session} {pipelineMode} {paused} {mpvAvailable}
		{playbackRate} {volume} {muted} {fitMode} {isFullscreen} {totalDuration} {currentPos} {scrubbedPct} {bufferedPct}
		{remainingText} {endsAtText} chapters={session?.chapters ?? session?.probe?.chapters ?? []} {skipSegments} {activeSkip} {skipCountdown}
		hasNextEpisode={!!nextEpisodeTarget}
		onLeave={() => void goto(backHref || to(detailPath(kind, tmdb)))}
		onToggleDrawer={(d) => { drawer = drawer === d ? 'none' : d; }}
		onTriggerMPV={() => { if (session) void triggerMPV(session.id, currentPos); }}
		onTogglePlayPause={togglePlayPause} onSeekBy={seekBy}
		onScrubInput={(sec) => { isScrubbing = true; scrubValue = sec; }}
		onScrubCommit={commitSeek} onToggleMute={toggleMute} onVolumeChange={setVolumeLevel}
		onCycleSpeed={cycleSpeed} onToggleFit={() => { fitMode = fitMode === 'contain' ? 'cover' : 'contain'; }}
		onNextEpisode={() => { if (nextEpisodeTarget) void switchEpisode(nextEpisodeTarget.season, nextEpisodeTarget.episode, true); }}
		onShuffleEpisode={() => void shuffleEpisode()} onToggleFullscreen={() => void toggleFullscreen()}
		onExecuteSkip={executeSkip} onCancelAutoSkip={() => { if (activeSkip) autoSkipDismissed = { ...autoSkipDismissed, [activeSkip.kind]: true }; }}
	/>

	<PlayerSourcesDrawer
		open={drawer === 'sources'} bind:queryText bind:sourceTab bind:resFilter {searching} {searchError} {fallbackNotice} {candidates} {counts}
		activeCandidateKey={activeCandidate ? candKey(activeCandidate) : ''} {session} {ffmpegAvailable} {mpvAvailable} {browserQuality} {audioIdx}
		onClose={() => { drawer = 'none'; }} onSearch={() => void runSearch()}
		onStartCandidate={(c, m) => { autoFallbacks = 0; void startCandidate(c, m, 0, true); }}
		onSelectFile={(idx) => { if (session) void selectStreamFile(session.id, idx).then((s) => { session = s; attachedSessionKey = ''; }); }}
		onToggleKeep={() => { if (session) void setStreamKeep(session.id, !session.keep_file).then((s) => { session = s; }); }}
		onChangeQuality={(q) => { browserQuality = q; if (session && playMode === 'browser') void attachPlayback(currentPos); }}
		onChangeAudio={(idx) => { audioIdx = idx; if (session && playMode === 'browser') void attachPlayback(currentPos); }}
	/>

	<PlayerSubsDrawer
		open={drawer === 'audioSubs'} {session} {subMode} {subIndex} {subLabel} {subDelayMS} {subSize} bind:subStyle bind:siteSubLangsInput
		{siteSubCandidates} {searchingSubs} {loadingSub} {subError}
		onClose={() => { drawer = 'none'; }} onTurnOff={turnOffSubtitles}
		onLoadEmbedded={(idx) => void loadEmbeddedSubtitle(idx)} onSearchOnline={() => void searchSubtitlesOnline()}
		onLoadSiteSub={(c) => void loadSiteSubtitleById(c.id, c.language, `${c.language.toUpperCase()} · ${c.site_name}`, false)}
		onAdjustDelay={(d) => { subDelayMS += d; showFeedback(`Subs ${subDelayMS}ms`); void persistProgress(); }}
		onChangeSize={(sz) => { subSize = sz; void persistProgress(); }} onUploadFile={(e) => void handleSubtitleFileUpload(e)}
		onStyleChange={(next) => { if (prefs) { prefs = { ...prefs, sub_style: next }; void saveStreamingPrefs(prefs).catch(() => {}); } }}
	/>

	<PlayerEpisodesDrawer
		open={drawer === 'episodes'} seasons={regularSeasons} selectedSeason={drawerSeason} currentSeason={season} currentEpisode={episode}
		episodes={drawerEps} {loadingEpisodes} progressMap={episodeProgressMap}
		onClose={() => { drawer = 'none'; }} onPickSeason={(s) => { drawerSeason = s; void loadSeasonEpisodes(s); }}
		onPickEpisode={(s, e) => void switchEpisode(s, e, true)} onShuffleEpisode={() => void shuffleEpisode()}
	/>
</div>

<style>
	.wp-stage { position: fixed; inset: 0; width: 100vw; height: 100dvh; overflow: hidden; background: #000; color: #fff; font-family: var(--font-ui); user-select: none; }
	.wp-stage.hide-cursor { cursor: none; }
	.wp-backdrop { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; filter: blur(18px) brightness(0.4); transform: scale(1.06); }
	.wp-backdrop-scrim { position: absolute; inset: 0; background: radial-gradient(circle at center, rgb(0 0 0 / 35%), rgb(0 0 0 / 88%)); }
	.wp-video { position: relative; width: 100%; height: 100%; object-fit: contain; background: transparent; cursor: pointer; }
	.wp-video.cover { object-fit: cover; }
	.wp-captions { position: absolute; left: 50%; transform: translateX(-50%); z-index: 6; display: flex; flex-direction: column; align-items: center; gap: 6px; max-width: min(82vw, 980px); text-align: center; pointer-events: none; transition: bottom 180ms ease; }
	.wp-cap-cue { padding: 4px 14px; border-radius: 6px; text-shadow: 0 2px 6px rgb(0 0 0 / 90%); line-height: 1.35; font-weight: 600; }
	.wp-captions.size-sm .wp-cap-cue { font-size: clamp(14px, 1.5vw, 18px); }
	.wp-captions.size-md .wp-cap-cue { font-size: clamp(18px, 2.2vw, 26px); }
	.wp-captions.size-lg .wp-cap-cue { font-size: clamp(22px, 2.8vw, 34px); }
	.wp-captions.size-xl .wp-cap-cue { font-size: clamp(28px, 3.4vw, 42px); }
	.wp-ripple { position: absolute; top: 50%; left: 50%; transform: translate(-50%, -50%); z-index: 8; padding: 12px 22px; border-radius: 999px; background: rgb(15 15 18 / 82%); border: 1px solid rgb(255 255 255 / 20%); font: 700 18px/1 var(--font-ui); pointer-events: none; }
</style>
