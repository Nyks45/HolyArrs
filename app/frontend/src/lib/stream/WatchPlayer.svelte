<script lang="ts">
	import type HlsType from 'hls.js';
	import { onDestroy, onMount } from 'svelte';
	import { goto, replaceState } from '$app/navigation';
	import { api, message } from '#lib/core/api.ts';
	import { to } from '#lib/core/paths.ts';
	import { toasts } from '#lib/core/toasts.svelte.ts';
	import { sized } from '#lib/core/tmdb-image.ts';
	import { detailPath, loadDetails, type TitleDetails } from '#lib/metadata/index.ts';
	import { ensureStreamHLS, formatEndsAt, formatTimeLeft, getStreamStatus, getTitleWatchHistory, launchStreamMPV, listStreamSources, saveWatchProgress, searchStreamReleases, searchStreamSubtitles, selectStreamFile, setStreamKeep, startStream, stopStream, type SessionStatus, type StreamCandidate, type StreamSubCandidate, type SubPref, type WatchEntry } from './stream-api.ts';
	import { getStreamingPrefs, saveStreamingPrefs, type SkipSegment, type StreamPrefs, type SubStylePrefs } from './streaming-config-api.ts';
	import { attachHlsMedia, buildEpisodeProgressMap, buildResumeCandidate, buildSearchReq, buildWatchEntryPayload, candidateKey, canSeekInLocalHlsBuffer, clearPlayerTimers, countCandidateKinds, DEFAULT_SUB_STYLE, extractRegularSeasons, fetchTitleSkips, findNextEpisodeTarget, isTimeBuffered, matchPlayerHotkey, mergeCandidateLists, mergeWatchHistory, normQualityChoice, parseSavedPlayerPrefs, postWatchScrobble, prefetchStreamSources, resolveEmbeddedSub, resolveSiteSub, resolveTotalDuration, resolveUploadedSub, runAutoFindAndSyncSubtitle, selectAutoPlayCandidate, shouldAcceptEnded, shouldAcceptTimeUpdate, shouldFallbackDirectSeekToTranscode, sortCandidatesForQuality, syncVttToSessionAudio, type HlsQualityLevel, type SeasonEp } from './player-media.ts';
	import { checkMeasuredRuntimeMismatch, createVideoWatchdogState, durationMismatchTier, nextFreezeRecoveryAction, readDecodedVideoFrames, resetVideoWatchdog, tickVideoWatchdog, type FreezeReason } from './video-watchdog.ts';
	import { resolveSubtitleStyle, stripSdhCueLines } from './subtitle-style.ts';
	import { activeCuesAt, type VTTCue } from './vtt.ts';
	import PlayerHud from './PlayerHud.svelte';
	import PlayerLandingOverlay from './PlayerLandingOverlay.svelte';
	import PlayerSourcesDrawer from './PlayerSourcesDrawer.svelte';
	import PlayerSubsDrawer from './PlayerSubsDrawer.svelte';
	import PlayerEpisodesDrawer from './PlayerEpisodesDrawer.svelte';

	let { kind, tmdb, initialSeason = 1, initialEpisode = 1, autoResume = true, initialDrawer = 'none', backHref = '' }: { kind: 'movie' | 'show'; tmdb: string; initialSeason?: number; initialEpisode?: number; autoResume?: boolean; initialDrawer?: 'none' | 'sources'; backHref?: string } = $props();

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
	let subStyle = $state<SubStylePrefs>({ ...DEFAULT_SUB_STYLE });

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
	let knownDurationSec = $state(0);
	let bufferedEnd = $state(0);
	let isScrubbing = $state(false);
	let scrubValue = $state(0);
	let pipelineMode = $state<'none' | 'direct' | 'hls'>('none');
	let switchingSource = $state(false);
	let pendingSeekTarget = $state<number | null>(null);
	let loadingMedia = $state(false);
	let loadingMoreSources = $state(false);
	let seamlessStatus = $state('');
	let stalled = $state(false);
	let hlsLevels = $state<HlsQualityLevel[]>([]);
	let hlsLevelIdx = $state(-1);
	let isPiP = $state(false);
	let playerError = $state('');

	let drawer = $state<'none' | 'sources' | 'quality' | 'audioSubs' | 'episodes'>('none');
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
	let syncingSub = $state(false);
	let activeSubVtt = $state('');
	let syncNote = $state('');
	let subError = $state('');

	let skipSegments = $state<SkipSegment[]>([]);
	let autoSkipDismissed = $state<Record<string, boolean>>({});
	let skipCountdown = $state(0);
	let nextCountdown = $state(10);

	let hlsInstance: HlsType | null = null;
	let pollTimer: ReturnType<typeof setInterval> | null = null, progressTimer: ReturnType<typeof setInterval> | null = null, scrobbleTimer: ReturnType<typeof setInterval> | null = null, skipTimer: ReturnType<typeof setInterval> | null = null, nextTimer: ReturnType<typeof setInterval> | null = null, watchdogTimer: ReturnType<typeof setInterval> | null = null;
	let hudTimer: ReturnType<typeof setTimeout> | null = null, feedbackTimer: ReturnType<typeof setTimeout> | null = null, waitingTimer: ReturnType<typeof setTimeout> | null = null;
	let attachedSessionKey = '', autoSubSessionId = '', usingLocalHls = $state(false);
	let loadReqSeq = 0, searchSeq = 0, startSeq = 0, freezeRecoveries = 0, sameSourceRetries = 0;
	let didInitialResumeDecision = false, destroyed = false, reseekingHls = false, sourceStarted = false, usingForceReencode = false, userSelectedCurrent = false;
	const watchdog = createVideoWatchdogState();

	const pipAvailable = $derived(typeof document !== 'undefined' && !!document.pictureInPictureEnabled);
	const prefQuality = $derived(prefs ? normQualityChoice(prefs.preferred_quality) : '1080p');
	const targetQuality = $derived(resFilter !== 'all' ? resFilter : prefQuality);
	const autoPlayOn = $derived(prefs ? prefs.auto_play_fastest : true);

	const regularSeasons = $derived(extractRegularSeasons(details));
	const curSeasonEps = $derived(seasonEpisodes[season] ?? []);
	const drawerEps = $derived(seasonEpisodes[drawerSeason] ?? []);
	const curEpObj = $derived(curSeasonEps.find((e) => e.number === episode));
	const activeSaved = $derived.by(() =>
		kind === 'show' ? (historyEpisodes.find((h) => (h.season ?? 0) === season && (h.episode ?? 0) === episode) ?? null) : historyLatest
	);
	const imdbId = $derived(details?.imdb || activeSaved?.imdb || '');
	const episodeProgressMap = $derived(buildEpisodeProgressMap(historyEpisodes));
	const nextEpisodeTarget = $derived(findNextEpisodeTarget(kind, season, episode, curSeasonEps, regularSeasons));
	const backdropUrl = $derived(details?.backdrop ? sized(details.backdrop, 'backdrop') : activeSaved?.backdrop ? sized(activeSaved.backdrop, 'backdrop') : '');
	const headerTitle = $derived(details?.name || activeSaved?.title || 'Watch');
	const headerSub = $derived(kind === 'show' ? `S${String(season).padStart(2, '0')}E${String(episode).padStart(2, '0')}${curEpObj?.name ? ` · ${curEpObj.name}` : ''}` : '');
	const isLocalEventHls = $derived(pipelineMode === 'hls' && usingLocalHls);
	const currentPos = $derived(isScrubbing ? scrubValue : pendingSeekTarget !== null ? pendingSeekTarget : pipelineMode === 'hls' ? seekOffset + videoTime : videoTime);
	const totalDuration = $derived(resolveTotalDuration({ probeDuration: session?.probe?.duration_sec, videoDuration, knownDuration: knownDurationSec, savedDuration: activeSaved?.duration_sec, metadataRuntimeMin: curEpObj?.runtime || details?.runtime, isLocalEventHls, seekOffset, currentPos }));
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

	function showFeedback(text: string) { feedbackText = text; if (feedbackTimer) clearTimeout(feedbackTimer); feedbackTimer = setTimeout(() => { feedbackText = ''; }, 900); }
	function wakeHud() { hudVisible = true; if (hudTimer) clearTimeout(hudTimer); if (!paused && drawer === 'none') hudTimer = setTimeout(() => { hudVisible = false; }, 2800); }
	function currentSubPref(): SubPref { return { mode: subMode, language: subLang, label: subLabel, index: subIndex, site_id: subSiteId, site_key: subSiteKey, delay_ms: subDelayMS, size: subSize }; }

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
			else historyEpisodes = mergeWatchHistory(historyEpisodes, saved);
		} catch { /* Ignore transient progress save failures. */ }
	}

	async function sendScrobble(action: 'start' | 'pause' | 'stop') {
		await postWatchScrobble({ action, kind, tmdb, imdbId, season, episode, currentPos, totalDuration });
	}

	function clearStall(keepSeamless = false) { if (waitingTimer) { clearTimeout(waitingTimer); waitingTimer = null; } stalled = false; if (!keepSeamless) { seamlessStatus = ''; pendingSeekTarget = null; } }
	function checkRuntimeMismatch(measuredSec: number) {
		const msg = checkMeasuredRuntimeMismatch({ kind, measuredSec, expectedRuntimeMin: curEpObj?.runtime || details?.runtime || 0, userSelectedCurrent, autoFallbackEnabled: !prefs || prefs.auto_fallback, autoFallbacks });
		if (!msg) return;
		const alt = sortCandidatesForQuality(candidates, targetQuality).find((c) => !triedCandKeys.has(candidateKey(c)) && durationMismatchTier(c) < 2 && (c.instant || c.seeders > 0));
		if (alt) tryFallbackSource(pendingStartSec || currentPos, msg);
	}
	function syncVideoDuration() { if (!switchingSource && videoEl && Number.isFinite(videoEl.duration) && videoEl.duration > 0) { videoDuration = videoEl.duration; if (!isLocalEventHls && videoEl.duration > 10) { knownDurationSec = videoEl.duration; checkRuntimeMismatch(videoEl.duration); } } }
	function onVideoWaiting() {
		if (pipelineMode === 'none' || switchingSource) return;
		if (waitingTimer) clearTimeout(waitingTimer);
		waitingTimer = setTimeout(() => { if (!paused && pipelineMode !== 'none' && !switchingSource) stalled = true; }, 1200);
	}
	function onVideoTimeUpdate() {
		if (!videoEl) return;
		if (videoEl.buffered.length > 0) bufferedEnd = videoEl.buffered.end(videoEl.buffered.length - 1);
		const pos = pipelineMode === 'hls' ? seekOffset + videoEl.currentTime : videoEl.currentTime;
		const decision = shouldAcceptTimeUpdate({ switchingSource, reseekingHls, videoSeeking: videoEl.seeking, pendingSeekTarget, actualPos: pos });
		if (decision === 'ignore') return;
		videoTime = videoEl.currentTime;
		if (decision === 'landed') { clearStall(); sameSourceRetries = 0; } else { clearStall(true); }
	}
	function onVideoSeeked() {
		if (switchingSource || reseekingHls || !videoEl || videoEl.seeking) return;
		const seekableEnd = videoEl.seekable.length > 0 ? videoEl.seekable.end(videoEl.seekable.length - 1) : 0;
		if (shouldFallbackDirectSeekToTranscode({ pipelineMode, ffmpegAvailable, pendingSeekTarget, videoTime: videoEl.currentTime, videoDuration, seekableEnd })) {
			const t = videoDuration > 0 ? Math.min(pendingSeekTarget!, videoDuration) : pendingSeekTarget!;
			void attachPlayback(t, true, true, 'Seeking...');
			return;
		}
		videoTime = videoEl.currentTime;
		clearStall();
	}
	function destroyVideoPipeline() {
		loadingMedia = false; switchingSource = false; clearStall(); resetVideoWatchdog(watchdog); usingLocalHls = false; usingForceReencode = false; hlsLevels = []; hlsLevelIdx = -1; videoDuration = 0; bufferedEnd = 0;
		if (hlsInstance) { hlsInstance.destroy(); hlsInstance = null; }
		if (videoEl) { videoEl.pause(); videoEl.removeAttribute('src'); videoEl.load(); }
		pipelineMode = 'none';
	}
	function stopPolling() { if (pollTimer) { clearInterval(pollTimer); pollTimer = null; } }
	function startPolling(id: string) { stopPolling(); pollTimer = setInterval(() => void pollSession(id), 1200); }

	async function fetchSkips(sessId = '') {
		const segs = await fetchTitleSkips({ kind, imdb: imdbId, tmdb, season, episode, title: headerTitle, genres: details?.genres, sessionId: sessId });
		if (segs && !destroyed) skipSegments = segs;
	}

	async function pollSession(id: string) {
		try {
			const st = await getStreamStatus(id);
			if (destroyed || !session || session.id !== id) return;
			session = st;
			if (st.probe?.duration_sec && st.probe.duration_sec > 0) { knownDurationSec = st.probe.duration_sec; checkRuntimeMismatch(st.probe.duration_sec); }
			if (st.state === 'error') { pendingMPV = false; switchingSource = false; seamlessStatus = ''; return; }
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
					await attachPlayback(pendingStartSec, false, pipelineMode !== 'none');
					if (st.chapters?.length) void fetchSkips(st.id);
					if (subMode === 'embedded' && st.probe?.subtitle_tracks.some((t) => t.index === subIndex)) await loadEmbeddedSubtitle(subIndex, true);
				} else if (pipelineMode === 'direct' && st.probe && !st.probe.direct_playable && ffmpegAvailable) {
					await attachPlayback(currentPos, true, true, 'Transcoding stream...');
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
			const [d, hist, srcs, pf] = await Promise.all([loadDetails(api.get, kind, tmdb), getTitleWatchHistory(kind, tmdb).catch(() => ({ latest: null, episodes: [] })), listStreamSources(kind), getStreamingPrefs().catch(() => null)]);
			if (destroyed) return;
			details = d ?? null; historyLatest = hist.latest; historyEpisodes = hist.episodes ?? [];
			mpvAvailable = srcs.mpv_available; ffmpegAvailable = srcs.ffmpeg_available;
			if (pf) { prefs = pf; subStyle = { ...pf.sub_style }; if (pf.preferred_sub_langs?.length) siteSubLangsInput = pf.preferred_sub_langs.join(', '); }
			queryText = d?.name || hist.latest?.title || '';

			if (kind === 'show') {
				const sDef = initialSeason > 0 ? initialSeason : hist.latest?.season && hist.latest.season > 0 ? hist.latest.season : regularSeasons[0]?.number ?? 1;
				const eDef = initialEpisode > 0 ? initialEpisode : hist.latest?.episode && hist.latest.episode > 0 ? hist.latest.episode : 1;
				season = sDef; drawerSeason = sDef; episode = eDef;
				await loadSeasonEpisodes(sDef);
			}
			const sv = kind === 'show' ? (historyEpisodes.find((h) => (h.season ?? 0) === season && (h.episode ?? 0) === episode) ?? null) : historyLatest;
			applySavedPrefs(sv); void fetchSkips(); await runSearch();

			if (!didInitialResumeDecision) {
				didInitialResumeDecision = true;
				if (initialDrawer === 'sources') drawer = 'sources';
				else if (autoResume && sv && !sv.completed) {
					if (sv.release_link) void resumeFromSaved(sv, sv.position_sec);
					else { const b = selectAutoPlayCandidate(candidates, targetQuality, false); if (b) void startCandidate(b, 'browser', sv.position_sec, true); }
				} else if (autoPlayOn) { const b = selectAutoPlayCandidate(candidates, targetQuality, false); if (b) void startCandidate(b, 'browser', 0, true); }
			}
		} catch (e) { pageError = message(e); } finally { loadingPage = false; wakeHud(); }
	}

	function applySavedPrefs(sv: WatchEntry | null) {
		if (!sv) return;
		const p = parseSavedPlayerPrefs(sv);
		if (p.browserQuality) browserQuality = p.browserQuality;
		audioIdx = p.audioIdx; playbackRate = p.playbackRate;
		if (sv.subtitle) { subDelayMS = p.subDelayMS; if (p.subSize) subSize = p.subSize; subLang = p.subLang; subLabel = p.subLabel; subSiteId = p.subSiteId; subSiteKey = p.subSiteKey; subIndex = p.subIndex; subMode = p.subMode; }
	}

	function warmNextEpisode() {
		if (kind !== 'show' || !nextEpisodeTarget || !details?.name) return;
		const nxtEp = seasonEpisodes[nextEpisodeTarget.season]?.find((e) => e.number === nextEpisodeTarget.episode);
		prefetchStreamSources(buildSearchReq({ query: details.name, kind: 'show', year: details.year || 0, season: nextEpisodeTarget.season, episode: nextEpisodeTarget.episode, imdb: imdbId, tmdb: Number(tmdb) || 0, details, preferredQuality: prefs?.preferred_quality, runtimeMin: nxtEp?.runtime || details.runtime || 0 }));
	}

	async function runSearch(noCache = false) {
		const q = (queryText || details?.name || activeSaved?.title || '').trim();
		if (!q && !imdbId && !tmdb) return;
		const seq = ++searchSeq;
		searching = true; loadingMoreSources = false; searchError = '';
		const baseReq = buildSearchReq({ query: q, kind, year: details?.year || activeSaved?.year || 0, season, episode, imdb: imdbId, tmdb: Number(tmdb) || 0, details, preferredQuality: prefs?.preferred_quality, runtimeMin: curEpObj?.runtime || details?.runtime || 0 });
		try {
			const res = await searchStreamReleases({ ...baseReq, mode: 'fast', no_cache: noCache });
			if (destroyed || seq !== searchSeq) return;
			candidates = res.candidates ?? [];
			counts = res.counts ?? countCandidateKinds(candidates);
		} catch (e) { if (seq === searchSeq) { searchError = message(e); candidates = []; } } finally { if (seq === searchSeq) searching = false; }

		if (destroyed || seq !== searchSeq) return;
		loadingMoreSources = true;
		void searchStreamReleases({ ...baseReq, mode: 'full', no_cache: noCache })
			.then((fullRes) => {
				if (destroyed || seq !== searchSeq) return;
				candidates = mergeCandidateLists(candidates, fullRes.candidates ?? []);
				counts = countCandidateKinds(candidates);
				if (!session && !activeCandidate && drawer === 'none' && pipelineMode === 'none' && autoPlayOn) {
					const startAt = autoResume && activeSaved && !activeSaved.completed ? activeSaved.position_sec : 0;
					const b = selectAutoPlayCandidate(candidates, targetQuality, true);
					if (b) void startCandidate(b, 'browser', startAt, true);
				}
			})
			.catch((e) => { if (seq === searchSeq && candidates.length === 0) searchError = message(e); })
			.finally(() => { if (seq === searchSeq) loadingMoreSources = false; });
	}

	function pickBestCandidate(): StreamCandidate | null {
		return sortCandidatesForQuality(candidates, targetQuality)[0] ?? null;
	}

	async function resumeFromSaved(sv: WatchEntry, startAt: number) {
		if (!sv.release_link) return;
		pendingFileIdx = typeof sv.file_index === 'number' && sv.file_index > 0 ? sv.file_index : null;
		if (sv.subtitle?.mode === 'site' && sv.subtitle.site_id) subMode = 'site';
		await startCandidate(buildResumeCandidate(sv), 'browser', startAt, true);
		if (sv.subtitle?.mode === 'site' && sv.subtitle.site_id) await loadSiteSubtitleById(sv.subtitle.site_id, sv.subtitle.language ?? '', sv.subtitle.label ?? '', true);
	}

	async function startCandidate(c: StreamCandidate, mode: 'browser' | 'mpv', startAt = 0, keepExternalSubs = true, userPicked = false) {
		if (pipelineMode !== 'none' && currentPos > 5) await persistProgress(currentPos);
		const myStart = ++startSeq;
		const keepLiveFrame = mode === 'browser' && pipelineMode !== 'none';
		if (!keepLiveFrame) destroyVideoPipeline(); else { switchingSource = true; seamlessStatus = 'Switching source...'; }
		stopPolling();
		const prevSession = session;
		attachedSessionKey = ''; activeCandidate = c; triedCandKeys.add(candidateKey(c)); userSelectedCurrent = userPicked;
		playMode = mode; pendingMPV = mode === 'mpv'; pendingStartSec = Math.max(0, startAt);
		sourceStarted = false; sameSourceRetries = 0; freezeRecoveries = 0; reseekingHls = false; starting = true; playerError = '';
		resetVideoWatchdog(watchdog);
		if (!keepLiveFrame) { session = null; seekOffset = pendingStartSec; videoTime = pendingStartSec; knownDurationSec = (c.duration_sec ?? 0) > 30 && !c.duration_mismatch ? c.duration_sec! : 0; }
		if (!keepExternalSubs || subMode === 'embedded') { subMode = 'off'; parsedCues = []; subFile = ''; activeSubVtt = ''; syncNote = ''; }

		try {
			const st = await startStream({
				candidate_id: c.id, local_file_id: c.local_file_id, debrid_provider: c.debrid_provider,
				source_id: c.source_id, source_name: c.provider || c.source_name, link: c.link,
				title: headerSub ? `${headerTitle} · ${headerSub}` : headerTitle, release_title: c.release_name || c.title,
				kind, season: kind === 'show' ? season : 0, episode: kind === 'show' ? episode : 0, keep_file: false,
				tmdb: Number(tmdb) || 0, imdb: imdbId
			});
			if (destroyed || myStart !== startSeq) { void stopStream(st.id).catch(() => {}); return; }
			if (prevSession && prevSession.id !== st.id && !prevSession.mpv_running) void stopStream(prevSession.id).catch(() => {});
			if (st.probe?.duration_sec && st.probe.duration_sec > 0) { knownDurationSec = st.probe.duration_sec; checkRuntimeMismatch(st.probe.duration_sec); }
			session = st; startPolling(st.id);
			if (st.state === 'ready' || (pendingMPV && st.state !== 'resolving')) void pollSession(st.id);
			wakeHud(); warmNextEpisode();
		} catch (e) {
			if (myStart !== startSeq) return;
			switchingSource = false; pendingMPV = false; seamlessStatus = ''; playerError = message(e);
			toasts.push('Could not start stream', 'fail', { body: playerError });
			tryFallbackSource(pendingStartSec);
		} finally { if (myStart === startSeq) starting = false; }
	}

	function tryFallbackSource(resumeAt?: number, noticeOverride?: string) {
		if ((prefs && !prefs.auto_fallback) || autoFallbacks >= 5) return;
		const pool = sortCandidatesForQuality(candidates, targetQuality);
		const avail = (c: StreamCandidate) => !triedCandKeys.has(candidateKey(c)) && (c.instant || c.seeders > 0);
		const next = pool.find((c) => avail(c) && durationMismatchTier(c) < 2) ?? pool.find(avail);
		if (!next) return;
		autoFallbacks++;
		fallbackNotice = noticeOverride || `Switching to fallback: ${next.provider || next.source_name}`;
		const pos = resumeAt ?? pendingSeekTarget ?? currentPos;
		void startCandidate(next, playMode, pos > 10 ? pos : 0, true);
	}

	function handlePlaybackFailure(errText: string, preferLocalTranscode = false) {
		const resumeAt = pendingSeekTarget ?? currentPos;
		switchingSource = false; reseekingHls = false; playerError = errText;
		if (sourceStarted && session && playMode === 'browser') {
			if (preferLocalTranscode && pipelineMode === 'direct' && ffmpegAvailable && sameSourceRetries < 2) {
				sameSourceRetries++; pendingSeekTarget = resumeAt; void attachPlayback(resumeAt, true, true, 'Seeking stream...'); return;
			}
			if (sameSourceRetries < 1) {
				sameSourceRetries++; pendingSeekTarget = resumeAt;
				void attachPlayback(resumeAt, pipelineMode === 'hls' && usingLocalHls, true, 'Retrying position...');
				return;
			}
		}
		clearStall();
		tryFallbackSource(resumeAt);
	}

	function handleVideoFreeze(reason: FreezeReason) {
		if (destroyed || !session || !videoEl || playMode !== 'browser' || switchingSource || pipelineMode === 'none') return;
		const action = nextFreezeRecoveryAction({ reason, attempts: freezeRecoveries, pipelineMode, usingLocalHls, usingForceReencode, ffmpegAvailable });
		freezeRecoveries++;
		if (action === 'fallback_source') {
			const msg = reason === 'no_video' ? 'No video track - switching source...' : 'Video froze - switching source...';
			tryFallbackSource(currentPos, msg); return;
		}
		if (action === 'reencode_hls') { void attachPlayback(currentPos, true, true, 'Re-encoding video...', true); return; }
		seamlessStatus = 'Recovering video...';
		if (action === 'kick_hls') { hlsInstance?.recoverMediaError(); videoEl.currentTime = Math.max(0, videoEl.currentTime + 0.15); hlsInstance?.startLoad(videoEl.currentTime); }
		else { videoEl.currentTime = Math.max(0, videoEl.currentTime + 0.1); void videoEl.play().catch(() => {}); }
	}

	function maybeAutoSub(sid: string) {
		if (prefs?.auto_find_subtitles && ffmpegAvailable && subMode === 'off' && autoSubSessionId !== sid) { autoSubSessionId = sid; void autoFindAndSyncSubtitle(true); }
	}

	async function attachPlayback(startSec: number, forceHLS = false, seamless = false, statusText = '', forceReencode = false) {
		const st = session;
		if (!st || !videoEl) return;
		const req = ++loadReqSeq;
		const keepLive = seamless && pipelineMode !== 'none';
		if (startSec > 0.5) pendingSeekTarget = startSec;
		if (!keepLive) {
			destroyVideoPipeline(); loadReqSeq = req; loadingMedia = true; seekOffset = startSec; videoTime = 0;
		} else {
			seamlessStatus = statusText || 'Updating stream...';
		}
		playerError = '';

		try {
			const tr = st.transport ?? 'torrent';
			const playURL = to(st.direct_url || st.media_url);
			const isCancelled = () => req !== loadReqSeq || destroyed || !videoEl;
			const hlsOpts = (url: string, at: number) => attachHlsMedia({
				video: videoEl!, srcUrl: url, startSec: at, playbackRate, volume, muted, isCancelled,
				onRecovering: (s) => { if (!isCancelled()) seamlessStatus = s; },
				onVideoCodecError: () => { if (!isCancelled() && !switchingSource) handleVideoFreeze('codec_error'); },
				onFatalError: (d) => { if (isCancelled() || switchingSource) return; handlePlaybackFailure(`Playback error (${d})`); },
				onStarted: () => { switchingSource = false; clearStall(!!videoEl?.seeking || pendingSeekTarget !== null); loadingMedia = false; void sendScrobble('start'); maybeAutoSub(st.id); },
				onLevelsLoaded: (levels) => { if (!isCancelled()) { hlsLevels = levels; hlsLevelIdx = -1; } }
			});

			if (tr === 'hls_remote' && !forceHLS && !forceReencode) {
				const oldHls = hlsInstance; hlsInstance = null; oldHls?.destroy();
				switchingSource = false; pipelineMode = 'hls'; usingLocalHls = false; usingForceReencode = false; seekOffset = 0; videoTime = startSec > 0.5 ? startSec : 0; videoDuration = 0; bufferedEnd = 0; resetVideoWatchdog(watchdog);
				hlsInstance = await hlsOpts(playURL, startSec);
				return;
			}
			const canDirect = !forceHLS && !forceReencode && browserQuality === 'original' && audioIdx === 0 && !st.needs_transcode && (tr === 'local' || tr === 'direct' ? st.probe?.direct_playable !== false : !!st.probe?.direct_playable);
			if (canDirect) {
				const oldHls = hlsInstance; hlsInstance = null; oldHls?.destroy();
				switchingSource = false; pipelineMode = 'direct'; usingLocalHls = false; usingForceReencode = false; seekOffset = 0; videoTime = startSec > 0.5 ? startSec : 0; videoDuration = 0; hlsLevels = []; resetVideoWatchdog(watchdog);
				if (startSec > 0.5) videoEl.addEventListener('loadedmetadata', () => { if (videoEl) videoEl.currentTime = startSec; }, { once: true });
				videoEl.src = playURL; videoEl.playbackRate = playbackRate; videoEl.volume = volume; videoEl.muted = muted;
				await videoEl.play().catch(() => {});
				clearStall(startSec > 0.5 || !!videoEl.seeking); void sendScrobble('start'); wakeHud(); maybeAutoSub(st.id); return;
			}
			if (!ffmpegAvailable) throw new Error('ffmpeg is not installed for transcoding');
			if (keepLive) reseekingHls = true;
			const hlsRes = await ensureStreamHLS(st.id, browserQuality, audioIdx, startSec, forceReencode);
			if (req === loadReqSeq) reseekingHls = false;
			if (isCancelled() || !session || session.id !== st.id) return;
			const oldHls = hlsInstance; hlsInstance = null; oldHls?.destroy();
			switchingSource = false; seekOffset = hlsRes.start_sec; videoTime = 0; videoDuration = 0; bufferedEnd = 0; pipelineMode = 'hls'; usingLocalHls = true; usingForceReencode = forceReencode || browserQuality !== 'original'; resetVideoWatchdog(watchdog);
			hlsInstance = await hlsOpts(to(hlsRes.playlist_url), 0);
		} catch (e) { if (req === loadReqSeq) handlePlaybackFailure(message(e)); } finally { if (req === loadReqSeq) { loadingMedia = false; reseekingHls = false; } }
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
		const anchor = pendingSeekTarget ?? currentPos;
		const next = Math.max(0, Math.min(totalDuration || anchor + delta, anchor + delta));
		showFeedback(`${delta > 0 ? '+' : ''}${delta}s`); commitSeek(next);
	}
	function commitSeek(target: number) {
		isScrubbing = false;
		if (!session || !videoEl) return;
		const t = Math.max(0, totalDuration > 0 ? Math.min(totalDuration, target) : target);
		const actualNow = pipelineMode === 'hls' ? seekOffset + videoTime : videoTime;
		if (Math.abs(t - actualNow) < 0.25 && !videoEl.seeking) { clearStall(); return; }
		pendingSeekTarget = t;
		if (pipelineMode === 'direct' || (pipelineMode === 'hls' && !usingLocalHls)) {
			if (!isTimeBuffered(videoEl.buffered, t)) seamlessStatus = 'Seeking...';
			videoEl.currentTime = t; videoTime = t; return;
		}
		if (pipelineMode === 'hls' && canSeekInLocalHlsBuffer(videoEl, seekOffset, t, bufferedEnd)) {
			const rel = Math.max(0, t - seekOffset); videoEl.currentTime = rel; videoTime = rel; return;
		}
		void attachPlayback(t, true, true, 'Jumping...');
	}
	function selectSpeed(spd: number) { playbackRate = spd; if (videoEl) videoEl.playbackRate = playbackRate; showFeedback(`${playbackRate}x`); }
	function selectHlsLevel(idx: number) { hlsLevelIdx = idx; if (hlsInstance) hlsInstance.currentLevel = idx; showFeedback(idx < 0 ? 'Quality: Auto' : `Quality: ${hlsLevels.find((l) => l.index === idx)?.label || ''}`); }
	async function togglePiP() { if (typeof document === 'undefined' || !videoEl) return; try { if (document.pictureInPictureElement) await document.exitPictureInPicture(); else await videoEl.requestPictureInPicture(); } catch {} }
	function setVolumeLevel(v: number) { volume = Math.min(1, Math.max(0, v)); muted = volume === 0; if (videoEl) { videoEl.volume = volume; videoEl.muted = muted; } }
	function toggleMute() { muted = !muted; if (videoEl) videoEl.muted = muted; showFeedback(muted ? 'Muted' : 'Unmuted'); }
	async function toggleFullscreen() { if (typeof document === 'undefined' || !stageEl) return; try { if (document.fullscreenElement) await document.exitFullscreen(); else await stageEl.requestFullscreen(); } catch {} }

	function turnOffSubtitles() { subMode = 'off'; subLabel = ''; subSiteId = ''; subFile = ''; activeSubVtt = ''; syncNote = ''; parsedCues = []; void persistProgress(); }
	async function loadEmbeddedSubtitle(idx: number, silent = false) {
		if (!session) return;
		loadingSub = true; subError = '';
		try {
			const res = await resolveEmbeddedSub(session.id, session.token, idx, session.probe?.subtitle_tracks.find((t) => t.index === idx));
			parsedCues = res.cues; activeSubVtt = res.rawVtt; subDelayMS = 0; syncNote = ''; subMode = 'embedded'; subIndex = idx; subSiteId = ''; subFile = ''; subLang = res.lang; subLabel = res.label;
			if (!silent) showFeedback(`Subs: ${res.label}`);
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
			const res = await resolveSiteSub(id, lang, label, session?.id ?? '');
			parsedCues = res.cues; activeSubVtt = res.rawVtt; subDelayMS = 0; syncNote = ''; subMode = 'site'; subSiteId = id; subLang = res.lang; subLabel = res.label; subFile = res.subFile;
			if (!silent) showFeedback(`Subs: ${res.label}`);
			void persistProgress();
		} catch (e) { subError = message(e); } finally { loadingSub = false; }
	}
	async function handleSubtitleFileUpload(e: Event) {
		const f = (e.currentTarget as HTMLInputElement).files?.[0];
		if (!f) return;
		loadingSub = true; subError = '';
		try {
			const res = await resolveUploadedSub(f, session?.id ?? '');
			parsedCues = res.cues; activeSubVtt = res.rawVtt; subDelayMS = 0; syncNote = ''; subMode = 'upload'; subLabel = res.label; subFile = res.subFile;
			showFeedback(`Subs: ${f.name}`);
		} catch (err) { subError = message(err); } finally { loadingSub = false; }
	}
	async function autoFindAndSyncSubtitle(silent = false) {
		if (!session || syncingSub) return;
		syncingSub = true; subError = ''; if (!silent) syncNote = 'Finding & syncing subtitle...';
		try {
			const langs = (prefs?.preferred_sub_langs?.length ? prefs.preferred_sub_langs : siteSubLangsInput.split(',').map((s) => s.trim()).filter(Boolean));
			const res = await runAutoFindAndSyncSubtitle({ sessionId: session.id, kind, title: headerTitle, year: details?.year ?? 0, season, episode, imdb: imdbId, tmdb, releaseName: activeCandidate?.release_name || activeCandidate?.title || session.release_title || '', preferredLangs: langs, startSec: currentPos, existingCandidates: siteSubCandidates, autoSync: !silent || !!prefs?.auto_sync_subtitles });
			siteSubCandidates = res.cands; parsedCues = res.cues; activeSubVtt = res.rawVtt; subMode = 'site'; subSiteId = res.best.id; subLang = res.lang; subLabel = res.label; subFile = res.subFile; subDelayMS = res.delayMs; syncNote = res.note;
			if (!silent) showFeedback(res.note);
			void persistProgress();
		} catch (e) { if (!silent) subError = message(e); syncNote = ''; } finally { syncingSub = false; }
	}
	async function syncCurrentSubtitleToAudio() {
		if (!session || !activeSubVtt || syncingSub) return;
		syncingSub = true; subError = ''; syncNote = 'Listening to audio...';
		try { const res = await syncVttToSessionAudio({ sessionId: session.id, rawVtt: activeSubVtt, startSec: currentPos }); subDelayMS = res.offsetMs; syncNote = res.note; showFeedback(res.note); void persistProgress(); }
		catch (e) { subError = message(e); syncNote = ''; } finally { syncingSub = false; }
	}

	async function switchEpisode(nextSeason: number, nextEpisode: number, autoPlayBest = true) {
		if (kind !== 'show') return;
		if (pipelineMode !== 'none' && currentPos > 5) await persistProgress(currentPos);
		destroyVideoPipeline(); stopPolling();
		if (session) { void stopStream(session.id).catch(() => {}); session = null; }
		season = nextSeason; drawerSeason = nextSeason; episode = nextEpisode; activeCandidate = null; userSelectedCurrent = false; freezeRecoveries = 0; resetVideoWatchdog(watchdog);
		attachedSessionKey = ''; pendingStartSec = 0; seekOffset = 0; videoTime = 0; knownDurationSec = 0; autoSubSessionId = ''; activeSubVtt = ''; syncNote = ''; drawer = 'none'; triedCandKeys.clear(); fallbackNotice = '';
		await loadSeasonEpisodes(nextSeason);
		const savedEp = episodeProgressMap[`${nextSeason}:${nextEpisode}`];
		if (savedEp) applySavedPrefs(savedEp);
		replaceState(to(`/watch/show/${encodeURIComponent(tmdb)}?s=${nextSeason}&e=${nextEpisode}${backHref ? `&from=${encodeURIComponent(backHref)}` : ''}`), {});
		void fetchSkips(); await runSearch();
		if (!autoPlayBest) return;
		if (savedEp?.release_link && !savedEp.completed && savedEp.position_sec > 10) { await resumeFromSaved(savedEp, savedEp.position_sec); return; }
		const best = selectAutoPlayCandidate(candidates, targetQuality, !loadingMoreSources);
		if (best) await startCandidate(best, 'browser', 0, true); else if (!loadingMoreSources) drawer = 'sources';
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
		skipCountdown = 0; autoSkipDismissed = { ...autoSkipDismissed, [seg.kind]: true }; commitSeek(seg.end); showFeedback(`Skipped ${seg.kind}`);
	}
	function onKeyDown(e: KeyboardEvent) {
		const act = matchPlayerHotkey(e, kind === 'show');
		if (!act) return;
		wakeHud();
		if (act === 'togglePlay' || act === 'back10' || act === 'fwd10') e.preventDefault();
		const map: Record<string, () => void> = { togglePlay: togglePlayPause, back10: () => seekBy(-10), fwd10: () => seekBy(10), mute: toggleMute, fullscreen: () => void toggleFullscreen(), sources: () => { drawer = drawer === 'sources' ? 'none' : 'sources'; }, episodes: () => { drawer = drawer === 'episodes' ? 'none' : 'episodes'; }, closeDrawer: () => { if (drawer !== 'none') drawer = 'none'; } };
		map[act]?.();
	}

	$effect(() => { if (drawer === 'audioSubs' && siteSubCandidates.length === 0 && !searchingSubs && details) void searchSubtitlesOnline(); });
	$effect(() => {
		if (nearEpisodeEnd && !nextTimer) { nextCountdown = 10; nextTimer = setInterval(() => { if (nextCountdown <= 1 && nextEpisodeTarget) { clearInterval(nextTimer!); nextTimer = null; void switchEpisode(nextEpisodeTarget.season, nextEpisodeTarget.episode, true); } else nextCountdown--; }, 1000); }
		else if (!nearEpisodeEnd && nextTimer) { clearInterval(nextTimer); nextTimer = null; }
	});
	$effect(() => {
		const seg = activeSkip;
		if (!seg || !prefs || autoSkipDismissed[seg.kind]) { if (skipTimer) { clearInterval(skipTimer); skipTimer = null; } skipCountdown = 0; return; }
		const wantAuto = (seg.kind === 'intro' && prefs.auto_skip_intro) || (seg.kind === 'recap' && prefs.auto_skip_recap) || (seg.kind === 'outro' && prefs.auto_skip_outro);
		if (!wantAuto || skipTimer) return;
		skipCountdown = 3; skipTimer = setInterval(() => { if (skipCountdown <= 1) executeSkip(seg); else skipCountdown--; }, 1000);
	});

	onMount(() => {
		void loadInitial();
		progressTimer = setInterval(() => { if (!paused && pipelineMode !== 'none' && currentPos > 5) void persistProgress(); }, 8000);
		scrobbleTimer = setInterval(() => { if (!paused && pipelineMode !== 'none' && currentPos > 10) void sendScrobble('start'); }, 60000);
		watchdogTimer = setInterval(() => {
			if (!videoEl || pipelineMode === 'none') return;
			const res = tickVideoWatchdog(watchdog, { nowMs: Date.now(), visible: typeof document === 'undefined' || document.visibilityState === 'visible' || isPiP, paused: videoEl.paused, ended: videoEl.ended, seeking: videoEl.seeking, busy: switchingSource || reseekingHls || loadingMedia || pendingSeekTarget !== null || stalled || starting, readyState: videoEl.readyState, playbackRate: videoEl.playbackRate, currentTime: videoEl.currentTime, videoWidth: videoEl.videoWidth, videoHeight: videoEl.videoHeight, totalFrames: readDecodedVideoFrames(videoEl) });
			if (res !== 'ok') handleVideoFreeze(res);
		}, 1000);
		const onFs = () => { isFullscreen = !!document.fullscreenElement; };
		const onPip = () => { isPiP = typeof document !== 'undefined' && document.pictureInPictureElement === videoEl; };
		const onUnload = () => { if (pipelineMode !== 'none' && currentPos > 5) { void persistProgress(currentPos, true); void sendScrobble('stop'); } if (session && !session.mpv_running) void stopStream(session.id).catch(() => {}); };
		const vid = videoEl;
		vid?.addEventListener('enterpictureinpicture', onPip); vid?.addEventListener('leavepictureinpicture', onPip);
		document.addEventListener('fullscreenchange', onFs); window.addEventListener('beforeunload', onUnload);
		return () => { vid?.removeEventListener('enterpictureinpicture', onPip); vid?.removeEventListener('leavepictureinpicture', onPip); document.removeEventListener('fullscreenchange', onFs); window.removeEventListener('beforeunload', onUnload); };
	});
	onDestroy(() => {
		destroyed = true;
		if (pipelineMode !== 'none' && currentPos > 5) { void persistProgress(currentPos, true); void sendScrobble('stop'); }
		clearPlayerTimers([progressTimer, scrobbleTimer, skipTimer, nextTimer, watchdogTimer], [hudTimer, feedbackTimer, waitingTimer]);
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
		ontimeupdate={onVideoTimeUpdate}
		onwaiting={onVideoWaiting}
		onplaying={() => { paused = false; sourceStarted = true; const hold = switchingSource || reseekingHls || pendingSeekTarget !== null; clearStall(hold); if (!hold) sameSourceRetries = 0; wakeHud(); }}
		oncanplay={() => clearStall(switchingSource || reseekingHls || pendingSeekTarget !== null)}
		onseeked={onVideoSeeked}
		onplay={() => { paused = false; wakeHud(); }}
		onpause={() => { paused = true; clearStall(); wakeHud(); }}
		onloadedmetadata={syncVideoDuration}
		ondurationchange={syncVideoDuration}
		onloadeddata={() => { if (!switchingSource && pipelineMode !== 'none' && videoEl && videoEl.videoWidth === 0 && session) handleVideoFreeze('no_video'); }}
		onended={() => { if (!shouldAcceptEnded({ switchingSource, seamlessStatus, pipelineMode, currentPos, totalDuration })) return; paused = true; void persistProgress(totalDuration, false, true); void sendScrobble('stop'); if (nextEpisodeTarget) void switchEpisode(nextEpisodeTarget.season, nextEpisodeTarget.episode, true); }}
		onerror={() => { if (switchingSource || pipelineMode === 'none' || !session) return; handlePlaybackFailure('Playback error', pipelineMode === 'direct'); }}
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
			{loadingPage} {pageError} {playMode} {session} {loadingMedia} {loadingMoreSources} {autoPlayOn} sourcesOpen={drawer === 'sources'} {headerTitle} {headerSub}
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
		{hudVisible} {drawer} {kind} {headerTitle} {headerSub} runtimeMin={curEpObj?.runtime || details?.runtime || 0}
		{session} {activeCandidate} candidatesCount={candidates.length} {loadingMoreSources} {seamlessStatus} {stalled}
		{pipelineMode} {paused} {mpvAvailable} {ffmpegAvailable} {pipAvailable} {isPiP}
		{playbackRate} {volume} {muted} {fitMode} {isFullscreen} {totalDuration} {currentPos} {scrubbedPct} {bufferedPct}
		{remainingText} {endsAtText} chapters={session?.chapters ?? session?.probe?.chapters ?? []} {skipSegments} {activeSkip} {skipCountdown}
		hasNextEpisode={!!nextEpisodeTarget} {hlsLevels} {hlsLevelIdx} {browserQuality}
		onLeave={() => void goto(backHref || to(detailPath(kind, tmdb)))}
		onToggleDrawer={(d) => { drawer = drawer === d ? 'none' : d; }}
		onTriggerMPV={() => { if (session) void triggerMPV(session.id, currentPos); }}
		onTogglePlayPause={togglePlayPause} onSeekBy={seekBy}
		onScrubInput={(sec) => { isScrubbing = true; scrubValue = sec; }}
		onScrubCommit={commitSeek} onToggleMute={toggleMute} onVolumeChange={setVolumeLevel}
		onSelectSpeed={selectSpeed} onToggleFit={() => { fitMode = fitMode === 'contain' ? 'cover' : 'contain'; }}
		onSelectHlsLevel={selectHlsLevel}
		onChangeBrowserQuality={(q) => { browserQuality = q; if (session && playMode === 'browser') void attachPlayback(currentPos, q !== 'original', true, `Switching to ${q}...`); }}
		onTogglePiP={() => void togglePiP()}
		onNextEpisode={() => { if (nextEpisodeTarget) void switchEpisode(nextEpisodeTarget.season, nextEpisodeTarget.episode, true); }}
		onToggleFullscreen={() => void toggleFullscreen()}
		onExecuteSkip={executeSkip} onCancelAutoSkip={() => { if (activeSkip) autoSkipDismissed = { ...autoSkipDismissed, [activeSkip.kind]: true }; }}
	/>

	<PlayerSourcesDrawer
		open={drawer === 'sources'} bind:queryText bind:sourceTab bind:resFilter preferredQuality={prefQuality} {searching} {loadingMoreSources} {searchError} {fallbackNotice} {candidates} {counts}
		activeCandidateKey={activeCandidate ? candidateKey(activeCandidate) : ''} {session} {mpvAvailable}
		onClose={() => { drawer = 'none'; }} onSearch={() => void runSearch(true)}
		onStartCandidate={(c, m) => { const pos = pipelineMode !== 'none' && currentPos > 10 ? currentPos : 0; autoFallbacks = 0; drawer = 'none'; void startCandidate(c, m, pos, true, true); }}
		onSelectFile={(idx) => { if (session) void selectStreamFile(session.id, idx).then((s) => { session = s; attachedSessionKey = ''; }); }}
		onToggleKeep={() => { if (session) void setStreamKeep(session.id, !session.keep_file).then((s) => { session = s; }); }}
	/>

	<PlayerSubsDrawer
		open={drawer === 'audioSubs'} {session} {audioIdx} {ffmpegAvailable} {subMode} {subIndex} {subLabel} {subDelayMS} {subSize} {activeSubVtt} {syncingSub} {syncNote} bind:subStyle bind:siteSubLangsInput
		{siteSubCandidates} {searchingSubs} {loadingSub} {subError}
		onClose={() => { drawer = 'none'; }}
		onChangeAudio={(idx) => { audioIdx = idx; if (session && playMode === 'browser') void attachPlayback(currentPos, true, true, 'Switching audio...'); }}
		onTurnOff={turnOffSubtitles}
		onLoadEmbedded={(idx) => void loadEmbeddedSubtitle(idx)} onSearchOnline={() => void searchSubtitlesOnline()}
		onLoadSiteSub={(c) => void loadSiteSubtitleById(c.id, c.language, `${c.language.toUpperCase()} · ${c.site_name}`, false)}
		onAutoFindAndSync={() => void autoFindAndSyncSubtitle(false)} onSyncCurrentSub={() => void syncCurrentSubtitleToAudio()}
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
	.wp-captions.size-sm .wp-cap-cue { font-size: clamp(14px, 1.5vw, 18px); } .wp-captions.size-md .wp-cap-cue { font-size: clamp(18px, 2.2vw, 26px); } .wp-captions.size-lg .wp-cap-cue { font-size: clamp(22px, 2.8vw, 34px); } .wp-captions.size-xl .wp-cap-cue { font-size: clamp(28px, 3.4vw, 42px); }
	.wp-ripple { position: absolute; top: 50%; left: 50%; transform: translate(-50%, -50%); z-index: 8; padding: 12px 22px; border-radius: 999px; background: rgb(15 15 18 / 82%); border: 1px solid rgb(255 255 255 / 20%); font: 700 18px/1 var(--font-ui); pointer-events: none; }
</style>
