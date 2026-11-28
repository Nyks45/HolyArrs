<script lang="ts">
	import type HlsType from 'hls.js';
	import { onDestroy, onMount, untrack } from 'svelte';
	import { goto, replaceState } from '$app/navigation';
	import { api, ApiError, message } from '#lib/core/api.ts';
	import { to } from '#lib/core/paths.ts';
	import { toasts } from '#lib/core/toasts.svelte.ts';
	import { sized } from '#lib/core/tmdb-image.ts';
	import { detailPath, loadDetails, type TitleDetails } from '#lib/metadata/index.ts';
	import { formatEndsAt, formatTimeLeft, getStreamStatus, getTitleWatchHistory, launchStreamMPV, listStreamSources, saveWatchProgress, searchStreamReleases, selectStreamFile, setStreamKeep, startStream, stopStream, type SessionStatus, type StreamCandidate, type SubPref, type WatchEntry } from './stream-api.ts';
	import { getStreamingPrefs, saveStreamingPrefs, type SkipSegment, type StreamPrefs, type SubStylePrefs } from './streaming-config-api.ts';
	import { buildEpisodeProgressMap, buildResumeCandidate, buildSearchReq, buildWatchEntryPayload, candidateKey, clearPlayerTimers, DEFAULT_SUB_STYLE, extractRegularSeasons, fetchTitleSkips, findNextEpisodeTarget, mergeWatchHistory, normQualityChoice, parseSavedPlayerPrefs, postWatchScrobble, prefetchStreamSources, resolveTotalDuration, selectAutoPlayCandidate, shouldAcceptEnded, sortCandidatesForQuality, type HlsQualityLevel, type SeasonEp } from './player-media.ts';
	import { checkMeasuredRuntimeMismatch, createVideoWatchdogState, durationMismatchTier, nextFreezeRecoveryAction, resetVideoWatchdog, type FreezeReason } from './video-watchdog.ts';
	import { resolveSubtitleStyle, stripSdhCueLines } from './subtitle-style.ts';
	import { createPlayerSourceSearch } from './player-source-search.svelte';
	import { attachPlayerWindowEvents, monitorVideo } from './player-runtime';
	import { connectPlayerTransport, type PlaybackTransport } from './player-transport';
	import { createPlayerSubtitles } from './subtitles-state.svelte';
	import { activeCuesAt } from './vtt.ts';
	import { parseAssDocument } from './ass-parser';
	import { setAudioBoost } from './audio-boost';
	import { loadPlayerDevicePrefs, resolveScopedSubtitleStyle, savePlayerDevicePrefs, type PlayerDevicePrefs, type PlayerUtilityPanel } from './player-prefs';
	import { readPlayerShortcut, type PlayerAction } from './player-shortcuts';
	import { screenshotName, takePlayerScreenshot } from './screenshot';
	import { pictureInPictureAvailable, playerInPictureInPicture, togglePlayerPiP } from './player-window';
	import WatchPlayerProcessing from './processing/WatchPlayerProcessing.svelte';
	import { createPlayerTimeline } from './player-timeline.svelte';
	import { hasLandedAt, matchSavedCandidate, resumeTarget } from './player-resume';
	import { createPlayerCountdowns } from './player-countdowns.svelte';
	import PlayerCaptionLayer from './PlayerCaptionLayer.svelte';
	import PlayerMorePanel from './PlayerMorePanel.svelte';
	import PlayerSettingsPanel from './PlayerSettingsPanel.svelte';
	import PlayerSubtitleStylePanel from './PlayerSubtitleStylePanel.svelte';
	import { loadPlayerFonts } from './player-fonts';
	import PlayerHud from './PlayerHud.svelte';
	import PlayerLandingOverlay from './PlayerLandingOverlay.svelte';
	import PlayerSourcesDrawer from './PlayerSourcesDrawer.svelte';
	import PlayerSubsDrawer from './PlayerSubsDrawer.svelte';
	import PlayerAudioPopover from './PlayerAudioPopover.svelte';
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
	let landed = false, lastGood = 0, resumeWaiting = false, transcodeAttaching = false, startDeadline: ReturnType<typeof setTimeout> | undefined;
	let ffmpegAvailable = $state(false);
	let queryText = $state('');
	const search = createPlayerSourceSearch((matches) => {
		if (session || activeCandidate || resumeWaiting || drawer !== 'none' || pipelineMode !== 'none' || !autoPlayOn) return;
		const startAt = autoResume && activeSaved && !activeSaved.completed ? activeSaved.position_sec : 0;
		const candidate = selectAutoPlayCandidate(matches, targetQuality, true);
		if (candidate) void startCandidate(candidate, 'browser', startAt, true);
	});
	const { state: sources } = search;
	const candidates = $derived(sources.candidates), loadingMoreSources = $derived(sources.loadingMore);
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
	let device = $state(loadPlayerDevicePrefs()), utility = $state<PlayerUtilityPanel>('none'), uiHidden = $state(false);
	let captionCanvas = $state<HTMLCanvasElement>(), processedCanvas = $state<HTMLCanvasElement>();
	let captureProcessed = $state<(() => Promise<HTMLCanvasElement | undefined>)>();
	let processingStatus = $state('');
	const fitMode = $derived(device.fit);
	let playbackRate = $state(1);
	let volume = $state(1);
	let muted = $state(false);
	let paused = $state(true);
	let isFullscreen = $state(false);

	let seekOffset = $state(0);
	let videoDuration = $state(0);
	let knownDurationSec = $state(0);
	let pipelineMode = $state<'none' | 'direct' | 'hls'>('none');
	let switchingSource = $state(false);
	let loadingMedia = $state(false);
	const { state: timeline, clearStall, onVideoWaiting, onVideoTimeUpdate, onVideoSeeked, commitSeek, seekBy, scrub } = createPlayerTimeline(() => ({
		video: videoEl, active: !!session, mode: pipelineMode, switching: switchingSource, reseeking: reseekingHls,
		paused, canTranscode: ffmpegAvailable, localHls: usingLocalHls, offset: seekOffset, pos: currentPos, duration: totalDuration, videoDuration
	}), { feedback: showFeedback, restart: (at, status) => void attachPlayback(at, true, true, status), landed: () => { sameSourceRetries = 0; }, userSeek: () => { landed = true; } });
	let hlsLevels = $state<HlsQualityLevel[]>([]);
	let hlsLevelIdx = $state(-1);
	let isPiP = $state(false);
	let playerError = $state('');

	let drawer = $state<'none' | 'sources' | 'quality' | 'audioSubs' | 'audio' | 'episodes'>('none');
	let hudVisible = $state(true);
	let feedbackText = $state('');

	const { state: subs, clear: clearCaptions, turnOffSubtitles, loadEmbeddedSubtitle, loadSiteSubtitleById, handleSubtitleFileUpload, searchSubtitlesOnline, autoFindAndSyncSubtitle, syncCurrentSubtitleToAudio } = createPlayerSubtitles(() => ({
		session, position: currentPos, preferred: prefs?.preferred_sub_langs ?? [], autoSync: !!prefs?.auto_sync_subtitles,
		title: { kind, title: headerTitle, year: details?.year ?? 0, season: kind === 'show' ? season : 0, episode: kind === 'show' ? episode : 0, imdb: imdbId, tmdb, release_name: activeCandidate?.release_name || activeCandidate?.title || session?.release_title || '' }
	}), () => void persistProgress(), showFeedback);

	let skipSegments = $state<SkipSegment[]>([]);
	const countdown = createPlayerCountdowns({ activeSkip: () => activeSkip, prefs: () => prefs,
		episodeKey: () => `${tmdb}:${season}:${episode}:${session?.id ?? ''}`, paused: () => paused, nearEpisodeEnd: () => nearEpisodeEnd, next: nextEpisode, skip: executeSkip });

	let hlsInstance: HlsType | null = null;
	let pollTimer: ReturnType<typeof setInterval> | null = null, progressTimer: ReturnType<typeof setInterval> | null = null, scrobbleTimer: ReturnType<typeof setInterval> | null = null, watchdogTimer: ReturnType<typeof setInterval> | null = null;
	let hudTimer: ReturnType<typeof setTimeout> | null = null, feedbackTimer: ReturnType<typeof setTimeout> | null = null;
	let attachedSessionKey = '', autoSubSessionId = '', usingLocalHls = $state(false);
	let loadReqSeq = 0, startSeq = 0, sameSourceRetries = 0;
	let freezeRecoveries = $state(0);
	let didInitialResumeDecision = false, destroyed = false, reseekingHls = false, sourceStarted = false, usingForceReencode = false, userSelectedCurrent = false;
	const watchdog = createVideoWatchdogState();

	const pipAvailable = pictureInPictureAvailable();
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
	const headerSub = $derived(kind === 'show' && !loadingPage ? `S${String(season).padStart(2, '0')}E${String(episode).padStart(2, '0')}${curEpObj?.name ? ` · ${curEpObj.name}` : ''}` : '');
	const isLocalEventHls = $derived(pipelineMode === 'hls' && usingLocalHls);
	const currentPos = $derived(timeline.isScrubbing ? timeline.scrubValue : timeline.pendingSeekTarget !== null ? timeline.pendingSeekTarget : pipelineMode === 'hls' ? seekOffset + timeline.videoTime : timeline.videoTime);
	const totalDuration = $derived(resolveTotalDuration({ probeDuration: session?.probe?.duration_sec, videoDuration, knownDuration: knownDurationSec, savedDuration: activeSaved?.duration_sec, metadataRuntimeMin: curEpObj?.runtime || details?.runtime, isLocalEventHls, seekOffset, currentPos }));
	const remainingText = $derived(formatTimeLeft(currentPos, totalDuration));
	const endsAtText = $derived(formatEndsAt(currentPos, totalDuration, playbackRate));
	const scrubbedPct = $derived(totalDuration > 0 ? Math.min(100, Math.max(0, (currentPos / totalDuration) * 100)) : 0);
	const bufferedPct = $derived(totalDuration > 0 ? Math.min(100, Math.max(scrubbedPct, ((pipelineMode === 'hls' ? seekOffset + timeline.bufferedEnd : timeline.bufferedEnd) / totalDuration) * 100)) : 0);
	const resolvedSubCSS = $derived(resolveSubtitleStyle(subStyle));
	const visibleCues = $derived.by(() => {
		const cues = subs.subMode === 'off' || subs.parsedCues.length === 0 ? [] : activeCuesAt(subs.parsedCues, currentPos, subs.subDelayMS);
		if (!subStyle.strip_sdh) return cues;
		return cues.map((c) => ({ ...c, lines: stripSdhCueLines(c.lines) })).filter((c) => c.lines.length > 0);
	});
	const activeSkip = $derived(pipelineMode === 'none' ? null : (skipSegments.find((s) => currentPos >= s.start && currentPos < s.end - 1) ?? null));
	const subtitleContext = $derived({ kind, tmdb, season, episode });
	const scopedSub = $derived(resolveScopedSubtitleStyle(device.subtitles, subtitleContext));
	const primaryAss = $derived(subs.rawAss ? parseAssDocument(subs.rawAss) : null);
	const secondaryAss = $derived(subs.secondary?.ass ? parseAssDocument(subs.secondary.ass) : null);
	const secondaryCues = $derived(subs.secondary ? activeCuesAt(subs.secondary.cues, currentPos, subs.subDelayMS) : []);
	const nearEpisodeEnd = $derived(device.autoNext && kind === 'show' && !!nextEpisodeTarget && pipelineMode !== 'none' && totalDuration > 180 && totalDuration - currentPos <= 25 && currentPos > 30);

	function showFeedback(text: string) { feedbackText = text; if (feedbackTimer) clearTimeout(feedbackTimer); feedbackTimer = setTimeout(() => { feedbackText = ''; }, 900); }
	function wakeHud() { hudVisible = true; if (hudTimer) clearTimeout(hudTimer); if (!paused && drawer === 'none' && utility === 'none') hudTimer = setTimeout(() => { hudVisible = false; }, 2800); }
	function currentSubPref(): SubPref { return { mode: subs.subMode, language: subs.subLang, label: subs.subLabel, index: subs.subIndex, site_id: subs.subSiteId, site_key: subs.subSiteKey, delay_ms: subs.subDelayMS, size: subs.subSize }; }

	const placeNow = () => resumeTarget({ pipelineMode, pendingSeekTarget: timeline.pendingSeekTarget, currentPos, pendingStartSec, landed, lastGood, saved: activeSaved });
	$effect(() => {
		const pos = currentPos;
		if (pipelineMode === 'none' || switchingSource || !sourceStarted || timeline.pendingSeekTarget !== null) return;
		if (!landed && hasLandedAt(pendingStartSec, pos)) landed = true;
		if (landed) lastGood = pos;
	});
	const watchEntry = (pos: number, completedOverride?: boolean) => buildWatchEntryPayload({
			kind, tmdb, imdbId, title: headerTitle, year: details?.year ?? 0,
			poster: details?.poster ?? '', backdrop: details?.backdrop ?? '',
			season, episode, episodeName: curEpObj?.name ?? '', pos,
			dur: totalDuration, completedOverride, activeCandidate, session, saved: activeSaved,
			browserQuality, audioIdx, playbackRate, subPref: currentSubPref()
	});
	async function persistProgress(posOverride?: number, keepalive = false, completedOverride?: boolean) {
		// Until the source reaches its start point, its playhead would overwrite a good resume position.
		if (!landed && completedOverride === undefined) return;
		const entry = watchEntry(posOverride ?? currentPos, completedOverride);
		if (!entry) return;
		try {
			const saved = await saveWatchProgress(entry, keepalive);
			if (destroyed) return;
			if (kind === 'movie') historyLatest = saved;
			else historyEpisodes = mergeWatchHistory(historyEpisodes, saved);
		} catch { /* Ignore transient progress save failures. */ }
	}

	const sendScrobble = (action: 'start' | 'pause' | 'stop') => postWatchScrobble({ action, kind, tmdb, imdbId, season, episode, currentPos, totalDuration });

	function checkRuntimeMismatch(measuredSec: number) {
		const msg = checkMeasuredRuntimeMismatch({ kind, measuredSec, expectedRuntimeMin: curEpObj?.runtime || details?.runtime || 0, userSelectedCurrent, autoFallbackEnabled: !prefs || prefs.auto_fallback, autoFallbacks });
		if (!msg) return;
		const alt = sortCandidatesForQuality(candidates, targetQuality).find((c) => !triedCandKeys.has(candidateKey(c)) && durationMismatchTier(c) < 2 && (c.instant || c.seeders > 0));
		if (alt) tryFallbackSource(placeNow(), msg);
	}
	function syncVideoDuration() { if (!switchingSource && videoEl && Number.isFinite(videoEl.duration) && videoEl.duration > 0) { videoDuration = videoEl.duration; if (!isLocalEventHls && videoEl.duration > 10) { knownDurationSec = videoEl.duration; checkRuntimeMismatch(videoEl.duration); } } }
	function destroyVideoPipeline() {
		loadingMedia = false; switchingSource = false; clearStall(); resetVideoWatchdog(watchdog); usingLocalHls = false; usingForceReencode = false; hlsLevels = []; hlsLevelIdx = -1; videoDuration = 0; timeline.bufferedEnd = 0;
		if (hlsInstance) { hlsInstance.destroy(); hlsInstance = null; }
		if (videoEl) { videoEl.pause(); videoEl.removeAttribute('src'); videoEl.load(); }
		pipelineMode = 'none';
	}
	function stopPolling() { if (pollTimer) { clearInterval(pollTimer); pollTimer = null; } }
	function startPolling(id: string, every = 400) { stopPolling(); pollTimer = setInterval(() => void pollSession(id), every); }

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
			if (st.state === 'error') { pendingMPV = false; switchingSource = false; timeline.seamlessStatus = ''; stopPolling(); if (playMode === 'browser') tryFallbackSource(placeNow(), 'Source failed - switching...'); return; }
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
					if (session?.id === id) startPolling(id, 3000);
					if (st.chapters?.length) void fetchSkips(st.id);
					if (subs.subMode === 'embedded' && st.probe?.subtitle_tracks.some((t) => t.index === subs.subIndex)) await loadEmbeddedSubtitle(subs.subIndex, true);
				} else if (pipelineMode === 'direct' && st.probe && !st.probe.direct_playable && ffmpegAvailable && !transcodeAttaching && !loadingMedia && !reseekingHls) {
					transcodeAttaching = true;
					try { await attachPlayback(placeNow(), true, true, 'Transcoding stream...'); } finally { transcodeAttaching = false; }
				}
			}
		} catch (e) {
			// A reaped or stopped session answers 404 for good; other errors are transient.
			if (e instanceof ApiError && e.status === 404 && session?.id === id && !destroyed) { stopPolling(); tryFallbackSource(placeNow(), 'Stream ended - switching source...'); }
		}
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
			if (pf) { prefs = pf; subStyle = { ...pf.sub_style }; if (pf.preferred_sub_langs?.length) subs.siteSubLangsInput = pf.preferred_sub_langs.join(', '); }
			queryText = d?.name || hist.latest?.title || '';

			if (kind === 'show') {
				const sDef = initialSeason > 0 ? initialSeason : hist.latest?.season && hist.latest.season > 0 ? hist.latest.season : regularSeasons[0]?.number ?? 1;
				const eDef = initialEpisode > 0 ? initialEpisode : hist.latest?.episode && hist.latest.episode > 0 ? hist.latest.episode : 1;
				season = sDef; drawerSeason = sDef; episode = eDef;
				await loadSeasonEpisodes(sDef);
				if (destroyed) return;
			}
			const sv = kind === 'show' ? (historyEpisodes.find((h) => (h.season ?? 0) === season && (h.episode ?? 0) === episode) ?? null) : historyLatest;
			applySavedPrefs(sv); void fetchSkips();
			const searching = runSearch();
			if (!didInitialResumeDecision && initialDrawer !== 'sources' && autoResume && sv && !sv.completed && sv.release_link) {
				// Resume without waiting for the whole search; a short wait lets a debrid or library match win.
				didInitialResumeDecision = true;
				resumeWaiting = true;
				await Promise.race([searching, new Promise((r) => setTimeout(r, 2500))]);
				resumeWaiting = false;
				if (!destroyed && !activeCandidate) void resumeFromSaved(sv, sv.position_sec);
			}
			await searching;

			if (!didInitialResumeDecision) {
				didInitialResumeDecision = true;
				if (initialDrawer === 'sources') drawer = 'sources';
				else if (autoResume && sv && !sv.completed) {
					const b = selectAutoPlayCandidate(candidates, targetQuality, false); if (b) void startCandidate(b, 'browser', sv.position_sec, true);
				} else if (autoPlayOn) { const b = selectAutoPlayCandidate(candidates, targetQuality, false); if (b) void startCandidate(b, 'browser', 0, true); }
			}
		} catch (e) { pageError = message(e); } finally { loadingPage = false; wakeHud(); }
	}

	function applySavedPrefs(sv: WatchEntry | null) {
		if (!sv) return;
		const p = parseSavedPlayerPrefs(sv);
		if (p.browserQuality) browserQuality = p.browserQuality;
		audioIdx = p.audioIdx; playbackRate = p.playbackRate;
		if (sv.subtitle) { subs.subDelayMS = p.subDelayMS; if (p.subSize) subs.subSize = p.subSize; subs.subLang = p.subLang; subs.subLabel = p.subLabel; subs.subSiteId = p.subSiteId; subs.subSiteKey = p.subSiteKey; subs.subIndex = p.subIndex; subs.subMode = p.subMode; }
	}

	function warmNextEpisode() {
		if (kind !== 'show' || !nextEpisodeTarget || !details?.name) return;
		const nxtEp = seasonEpisodes[nextEpisodeTarget.season]?.find((e) => e.number === nextEpisodeTarget.episode);
		prefetchStreamSources(buildSearchReq({ query: details.name, kind: 'show', year: details.year || 0, season: nextEpisodeTarget.season, episode: nextEpisodeTarget.episode, imdb: imdbId, tmdb: Number(tmdb) || 0, details, preferredQuality: prefs?.preferred_quality, runtimeMin: nxtEp?.runtime || details.runtime || 0 }));
	}

	const sourceSearchRequest = (q = details?.name || activeSaved?.title || queryText) => buildSearchReq({ query: q, kind, year: details?.year || activeSaved?.year || 0, season, episode, imdb: imdbId, tmdb: Number(tmdb) || 0, details, preferredQuality: prefs?.preferred_quality, runtimeMin: curEpObj?.runtime || details?.runtime || 0 });
	function runSearch(noCache = false) {
		const query = (queryText || details?.name || activeSaved?.title || '').trim();
		if (query || imdbId || tmdb) return search.run(sourceSearchRequest(query), noCache);
	}

	function pickBestCandidate(): StreamCandidate | null {
		return sortCandidatesForQuality(candidates, targetQuality)[0] ?? null;
	}

	async function resumeFromSaved(sv: WatchEntry, startAt: number) {
		if (!sv.release_link) return;
		pendingFileIdx = typeof sv.file_index === 'number' && sv.file_index > 0 ? sv.file_index : null;
		if (sv.subtitle?.mode === 'site' && sv.subtitle.site_id) subs.subMode = 'site';
		await startCandidate(matchSavedCandidate(sv, candidates) ?? buildResumeCandidate(sv), 'browser', startAt, true);
		if (sv.subtitle?.mode === 'site' && sv.subtitle.site_id) await loadSiteSubtitleById(sv.subtitle.site_id, sv.subtitle.language ?? '', sv.subtitle.label ?? '', true);
	}

	async function startCandidate(c: StreamCandidate, mode: 'browser' | 'mpv', startAt = 0, keepExternalSubs = true, userPicked = false) {
		if (landed && currentPos > 5) void persistProgress(currentPos);
		const myStart = ++startSeq;
		landed = false; clearTimeout(startDeadline);
		if (mode === 'browser') startDeadline = setTimeout(() => { if (myStart === startSeq && !sourceStarted && !destroyed && playMode === 'browser') tryFallbackSource(placeNow(), 'Source is too slow - switching...'); }, 45000);
		const keepLiveFrame = mode === 'browser' && pipelineMode !== 'none';
		if (!keepLiveFrame) destroyVideoPipeline(); else { switchingSource = true; timeline.seamlessStatus = 'Switching source...'; }
		stopPolling();
		const prevSession = session;
		attachedSessionKey = ''; activeCandidate = c; triedCandKeys.add(candidateKey(c)); userSelectedCurrent = userPicked;
		playMode = mode; pendingMPV = mode === 'mpv'; pendingStartSec = Math.max(0, startAt);
		sourceStarted = false; sameSourceRetries = 0; freezeRecoveries = 0; reseekingHls = false; starting = true; playerError = '';
		resetVideoWatchdog(watchdog);
		if (!keepLiveFrame) { session = null; seekOffset = pendingStartSec; timeline.videoTime = pendingStartSec; knownDurationSec = (c.duration_sec ?? 0) > 30 && !c.duration_mismatch ? c.duration_sec! : 0; }
		if (!keepExternalSubs || subs.subMode === 'embedded') clearCaptions();

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
			switchingSource = false; pendingMPV = false; timeline.seamlessStatus = ''; playerError = message(e);
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
		const pos = resumeAt ?? placeNow();
		void startCandidate(next, playMode, pos > 10 ? pos : 0, true);
	}

	function handlePlaybackFailure(errText: string, preferLocalTranscode = false) {
		const resumeAt = placeNow();
		switchingSource = false; reseekingHls = false; playerError = errText;
		if (sourceStarted && session && playMode === 'browser') {
			if (preferLocalTranscode && pipelineMode === 'direct' && ffmpegAvailable && sameSourceRetries < 2) {
				sameSourceRetries++; timeline.pendingSeekTarget = resumeAt; void attachPlayback(resumeAt, true, true, 'Seeking stream...'); return;
			}
			if (sameSourceRetries < 1) {
				sameSourceRetries++; timeline.pendingSeekTarget = resumeAt;
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
			tryFallbackSource(placeNow(), msg); return;
		}
		if (action === 'reencode_hls') { void attachPlayback(currentPos, true, true, 'Re-encoding video...', true); return; }
		timeline.seamlessStatus = 'Recovering video...';
		if (action === 'kick_hls') { hlsInstance?.recoverMediaError(); videoEl.currentTime = Math.max(0, videoEl.currentTime + 0.15); hlsInstance?.startLoad(videoEl.currentTime); }
		else { videoEl.currentTime = Math.max(0, videoEl.currentTime + 0.1); void videoEl.play().catch(() => {}); }
	}

	function maybeAutoSub(sid: string) {
		if (prefs?.auto_find_subtitles && ffmpegAvailable && subs.subMode === 'off' && autoSubSessionId !== sid) { autoSubSessionId = sid; void autoFindAndSyncSubtitle(true); }
	}

	function setPlaybackTransport(choice: PlaybackTransport) {
		hlsInstance?.destroy(); hlsInstance = null;
		switchingSource = false; pipelineMode = choice.mode; seekOffset = choice.offset; timeline.videoTime = choice.time;
		usingLocalHls = choice.local; usingForceReencode = choice.reencoded; videoDuration = 0; timeline.bufferedEnd = 0;
		if (choice.mode === 'direct') hlsLevels = [];
		resetVideoWatchdog(watchdog);
	}
	function playbackStarted(id: string, direct: boolean, initialSeek = false) {
		switchingSource = false; loadingMedia = false;
		clearStall(initialSeek || !!videoEl?.seeking || timeline.pendingSeekTarget !== null);
		void sendScrobble('start'); if (direct) wakeHud(); maybeAutoSub(id);
	}

	async function attachPlayback(startSec: number, forceHLS = false, seamless = false, statusText = '', forceReencode = false) {
		const st = session;
		if (!st || !videoEl) return;
		if (/^av1$/i.test(st.probe?.video_codec ?? '') && (device.forceAv1 || !videoEl.canPlayType('video/mp4; codecs="av01.0.08M.08"'))) forceReencode = true;
		const req = ++loadReqSeq;
		const keepLive = seamless && pipelineMode !== 'none';
		if (!keepLive) {
			destroyVideoPipeline(); loadReqSeq = req; loadingMedia = true; seekOffset = startSec; timeline.videoTime = 0;
		} else {
			timeline.seamlessStatus = statusText || 'Updating stream...';
		}
		if (startSec > 0.5) timeline.pendingSeekTarget = startSec;
		playerError = '';

		try {
			const isCancelled = () => req !== loadReqSeq || destroyed || session?.id !== st.id;
			const attached = await connectPlayerTransport({
				session: st, start: startSec, hls: forceHLS, reencode: forceReencode, keepFrame: keepLive,
				canTranscode: ffmpegAvailable, quality: browserQuality, audio: audioIdx, h264: device.h264Quality,
				media: { video: videoEl, playbackRate, volume, muted, isCancelled,
					onRecovering: (status) => { if (!isCancelled()) timeline.seamlessStatus = status; },
					onVideoCodecError: () => { if (!isCancelled() && !switchingSource) handleVideoFreeze('codec_error'); },
					onFatalError: (reason) => { if (!isCancelled() && !switchingSource) handlePlaybackFailure(`Playback error (${reason})`); },
					onStarted: () => { if (!isCancelled()) playbackStarted(st.id, false); },
					onLevelsLoaded: (levels) => { if (!isCancelled()) { hlsLevels = levels; hlsLevelIdx = -1; } }
				},
				onPipeline: setPlaybackTransport, onReseek: (loading) => { reseekingHls = loading; },
				onDirect: () => playbackStarted(st.id, true, startSec > .5)
			});
			if (isCancelled()) attached?.destroy(); else hlsInstance = attached;
		} catch (e) { if (req === loadReqSeq) handlePlaybackFailure(message(e)); } finally { if (req === loadReqSeq) { loadingMedia = false; reseekingHls = false; } }
	}

	async function triggerMPV(id: string, atSec = currentPos) {
		destroyVideoPipeline(); playMode = 'mpv';
		try { session = await launchStreamMPV(id, { start_sec: atSec > 10 ? atSec : 0, sub_file: subs.subFile, watch: watchEntry(atSec) ?? undefined }); }
		catch (e) { playerError = message(e); toasts.push('Could not open MPV', 'fail', { body: playerError }); }
	}

	function togglePlayPause() {
		if (!videoEl || pipelineMode === 'none') return;
		if (videoEl.paused) { void videoEl.play(); showFeedback('Play'); void sendScrobble('start'); }
		else { videoEl.pause(); showFeedback('Pause'); void persistProgress(); void sendScrobble('pause'); }
	}
	function selectSpeed(spd: number) { playbackRate = spd; if (videoEl) videoEl.playbackRate = playbackRate; showFeedback(`${playbackRate}x`); }
	function selectHlsLevel(idx: number) { hlsLevelIdx = idx; if (hlsInstance) hlsInstance.currentLevel = idx; showFeedback(idx < 0 ? 'Quality: Auto' : `Quality: ${hlsLevels.find((l) => l.index === idx)?.label || ''}`); }
	async function togglePiP() { if (videoEl) await togglePlayerPiP(videoEl, { processed: () => processedCanvas, captions: () => captionCanvas, togglePlay: togglePlayPause, changed: () => { isPiP = playerInPictureInPicture(videoEl); } }).catch((e) => showFeedback(message(e))); }
	function setVolumeLevel(v: number) { volume = Math.min(1, Math.max(0, v)); muted = volume === 0; if (videoEl) { videoEl.volume = volume; videoEl.muted = muted; } }
	function toggleMute() { muted = !muted; if (videoEl) videoEl.muted = muted; showFeedback(muted ? 'Muted' : 'Unmuted'); }
	async function toggleFullscreen() { if (typeof document === 'undefined' || !stageEl) return; try { if (document.fullscreenElement) await document.exitFullscreen(); else await (device.fullscreen === 'document' ? document.documentElement : stageEl).requestFullscreen(); } catch {} }

	async function switchEpisode(nextSeason: number, nextEpisode: number, autoPlayBest = true) {
		if (kind !== 'show') return;
		startSeq++; loadReqSeq++; clearTimeout(startDeadline);
		if (landed && currentPos > 5) void persistProgress(currentPos);
		landed = false; lastGood = 0;
		destroyVideoPipeline(); stopPolling();
		if (session) { void stopStream(session.id).catch(() => {}); session = null; }
		season = nextSeason; drawerSeason = nextSeason; episode = nextEpisode; activeCandidate = null; userSelectedCurrent = false; freezeRecoveries = 0; resetVideoWatchdog(watchdog);
		attachedSessionKey = ''; pendingStartSec = 0; seekOffset = 0; timeline.videoTime = 0; knownDurationSec = 0; autoSubSessionId = ''; skipSegments = []; clearCaptions(); drawer = 'none'; triedCandKeys.clear(); fallbackNotice = '';
		await loadSeasonEpisodes(nextSeason);
		const savedEp = episodeProgressMap[`${nextSeason}:${nextEpisode}`];
		if (savedEp) applySavedPrefs(savedEp);
		replaceState(to(`/watch/show/${encodeURIComponent(tmdb)}?s=${nextSeason}&e=${nextEpisode}${backHref ? `&from=${encodeURIComponent(backHref)}` : ''}`), {});
		void fetchSkips();
		const searching = runSearch();
		if (autoPlayBest && savedEp?.release_link && !savedEp.completed && savedEp.position_sec > 10) {
			resumeWaiting = true;
			await Promise.race([searching, new Promise((r) => setTimeout(r, 2500))]);
			resumeWaiting = false;
			if (!destroyed && !activeCandidate) await resumeFromSaved(savedEp, savedEp.position_sec);
			return;
		}
		await searching;
		if (!autoPlayBest) return;
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
		countdown.cancel(seg); commitSeek(seg.end); showFeedback(`Skipped ${seg.kind}`);
	}
	function updateDevice(patch: Partial<PlayerDevicePrefs>) {
		device = { ...device, ...patch }; savePlayerDevicePrefs(device);
		if (patch.boost !== undefined && videoEl) void setAudioBoost(videoEl, device.boost).catch((e) => showFeedback(message(e)));
		if ((patch.forceAv1 !== undefined || patch.h264Quality !== undefined) && session && pipelineMode !== 'none' && (usingLocalHls || /^av1$/i.test(session.probe?.video_codec ?? ''))) void attachPlayback(currentPos, usingLocalHls, true);
	}
	function toggleCaptions() {
		if (subs.subMode !== 'off' || subs.secondary) { subs.subHidden = !subs.subHidden; return; }
		const first = session?.probe?.subtitle_tracks.find((track) => track.text);
		if (first) void loadEmbeddedSubtitle(first.index); else drawer = 'audioSubs';
	}
	function openUtility(next: PlayerUtilityPanel) { utility = utility === next ? 'none' : next; drawer = 'none'; wakeHud(); }
	function openDrawer(next: typeof drawer) { drawer = drawer === next ? 'none' : next; utility = 'none'; wakeHud(); }
	function nextEpisode() { if (nextEpisodeTarget) void switchEpisode(nextEpisodeTarget.season, nextEpisodeTarget.episode, true); }
	async function previousEpisode() {
		const before = curSeasonEps[curSeasonEps.findIndex((item) => item.number === episode) - 1];
		if (before) { await switchEpisode(season, before.number); return; }
		const preceding = regularSeasons[regularSeasons.findIndex((item) => item.number === season) - 1];
		if (!preceding) return;
		await loadSeasonEpisodes(preceding.number);
		const last = seasonEpisodes[preceding.number]?.at(-1);
		if (last) await switchEpisode(preceding.number, last.number);
	}
	function saveFrame(raw: boolean, withSubtitles: boolean) {
		if (!videoEl) return;
		const name = screenshotName(headerTitle, currentPos, kind === 'show' ? season : undefined, kind === 'show' ? episode : undefined);
		void takePlayerScreenshot({ video: videoEl, captions: captionCanvas, processed: captureProcessed, raw, withSubtitles, name, fit: fitMode }).then(() => showFeedback('Screenshot saved'), (e) => showFeedback(message(e)));
	}
	function onKeyDown(e: KeyboardEvent) {
		const action = readPlayerShortcut(e, kind === 'show');
		if (!action) return;
		e.preventDefault(); wakeHud();
		if (action.startsWith('jump')) { if (totalDuration > 0) commitSeek(totalDuration * Number(action.slice(4)) / 10); return; }
		const actions: Partial<Record<PlayerAction, () => void>> = {
			togglePlay: togglePlayPause, back5: () => seekBy(-5), fwd5: () => seekBy(5), back10: () => seekBy(-10), fwd10: () => seekBy(10),
			volumeUp: () => setVolumeLevel(volume + .05), volumeDown: () => setVolumeLevel(volume - .05), start: () => commitSeek(0), end: () => commitSeek(Math.max(0, totalDuration - 2)),
			mute: toggleMute, fullscreen: () => void toggleFullscreen(), immersive: () => updateDevice({ immersive: !device.immersive }),
			next: nextEpisode, previous: () => void previousEpisode(), subtitles: toggleCaptions, stats: () => openUtility('stats'), hide: () => { uiHidden = !uiHidden; utility = 'none'; drawer = 'none'; },
			pip: () => void togglePiP(), shortcuts: () => openUtility('shortcuts'), episodes: () => openDrawer('episodes'), sources: () => openDrawer('sources'),
			closeDrawer: () => { utility = 'none'; drawer = 'none'; }, screenshot: () => saveFrame(false, false), screenshotSubs: () => saveFrame(false, true),
			screenshotRaw: () => saveFrame(true, false), screenshotRawSubs: () => saveFrame(true, true)
		};
		actions[action]?.();
	}

	$effect(() => {
		if (drawer === 'audioSubs' && details) untrack(() => { if (!subs.siteSubCandidates.length && !subs.searchingSubs) void searchSubtitlesOnline(); });
	});

	onMount(() => {
		void loadInitial(); void loadPlayerFonts().catch(() => {});
		progressTimer = setInterval(() => { if (!paused && pipelineMode !== 'none' && landed && currentPos > 5) void persistProgress(); }, 8000);
		scrobbleTimer = setInterval(() => { if (!paused && pipelineMode !== 'none' && currentPos > 10) void sendScrobble('start'); }, 60000);
		watchdogTimer = setInterval(() => {
			if (videoEl && pipelineMode !== 'none') monitorVideo(watchdog, videoEl, switchingSource || reseekingHls || loadingMedia || timeline.pendingSeekTarget !== null || timeline.stalled || starting, isPiP, handleVideoFreeze);
		}, 1000);
		return attachPlayerWindowEvents(videoEl, { pauseAway: () => device.pauseAway, onFullscreen: (next) => { isFullscreen = next; }, onPiP: (next) => { isPiP = next; },
			onUnload: () => { if (pipelineMode !== 'none' && currentPos > 5) { void persistProgress(currentPos, true); void sendScrobble('stop'); } if (session && !session.mpv_running) void stopStream(session.id).catch(() => {}); } });
	});
	onDestroy(() => {
		destroyed = true; search.cancel(); clearTimeout(startDeadline);
		if (pipelineMode !== 'none' && currentPos > 5) { void persistProgress(currentPos, true); void sendScrobble('stop'); }
		clearPlayerTimers([progressTimer, scrobbleTimer, watchdogTimer], [hudTimer, feedbackTimer]);
		stopPolling(); destroyVideoPipeline();
		if (session && !session.mpv_running) void stopStream(session.id).catch(() => {});
	});
</script>

<svelte:head><title>{headerTitle} · Watch · HolyArrs</title></svelte:head>
<svelte:window onkeydown={onKeyDown} />

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div bind:this={stageEl} class="wp-stage" class:hide-cursor={!hudVisible && !paused && drawer === 'none'} onmousemove={wakeHud} ontouchstart={wakeHud} onscroll={(e) => { e.currentTarget.scrollTop = 0; e.currentTarget.scrollLeft = 0; }}>
	{#if backdropUrl && pipelineMode === 'none'}
		<img class="wp-backdrop" src={backdropUrl} alt="" />
		<div class="wp-backdrop-scrim"></div>
	{/if}

	<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
	<video
		bind:this={videoEl} class="wp-video" style:object-fit={fitMode} crossorigin="anonymous" playsinline
		onclick={() => { if (drawer !== 'none' || utility !== 'none') { drawer = 'none'; utility = 'none'; } else togglePlayPause(); }}
		ontimeupdate={onVideoTimeUpdate}
		onwaiting={onVideoWaiting}
		onplaying={() => { paused = false; if (device.boost > 100 && videoEl) void setAudioBoost(videoEl, device.boost).catch(() => {}); sourceStarted = true; const hold = switchingSource || reseekingHls || timeline.pendingSeekTarget !== null; clearStall(hold); if (!hold) sameSourceRetries = 0; wakeHud(); }}
		oncanplay={() => clearStall(switchingSource || reseekingHls || timeline.pendingSeekTarget !== null)}
		onseeked={onVideoSeeked}
		onplay={() => { paused = false; wakeHud(); }}
		onpause={() => { paused = true; clearStall(); wakeHud(); }}
		onloadedmetadata={syncVideoDuration}
		ondurationchange={syncVideoDuration}
		onloadeddata={() => { if (!switchingSource && pipelineMode !== 'none' && videoEl && videoEl.videoWidth === 0 && session) handleVideoFreeze('no_video'); }}
		onended={() => { if (!shouldAcceptEnded({ switchingSource, seamlessStatus: timeline.seamlessStatus, pipelineMode, currentPos, totalDuration })) return; paused = true; void persistProgress(totalDuration, false, true); void sendScrobble('stop'); if (device.autoNext) nextEpisode(); }}
		onerror={() => { if (switchingSource || pipelineMode === 'none' || !session) return; handlePlaybackFailure('Playback error', pipelineMode === 'direct'); }}
	></video>

	<WatchPlayerProcessing video={pipelineMode === 'none' ? undefined : videoEl} {details} fit={fitMode} panel={uiHidden ? 'none' : utility} bind:canvas={processedCanvas}
		bind:capture={captureProcessed} bind:status={processingStatus} onTab={(next) => { utility = next; }} onClose={() => { utility = 'none'; drawer = 'quality'; }} />

	{#if !subs.subHidden && (subs.subMode !== 'off' || !!subs.secondary) && pipelineMode !== 'none'}
		<PlayerCaptionLayer bind:canvas={captionCanvas} video={videoEl} fit={fitMode} time={currentPos} primary={visibleCues} secondary={secondaryCues} {primaryAss} {secondaryAss} delay={subs.subDelayMS} base={resolvedSubCSS} scoped={scopedSub} size={subs.subSize} raised={!uiHidden && (hudVisible || paused)} overlap={device.overlap} blendFix={device.blendFix} renderer={device.renderer} />
	{/if}

	{#if feedbackText}<div class="wp-ripple" aria-live="polite">{feedbackText}</div>{/if}

	{#if pipelineMode === 'none'}
		<PlayerLandingOverlay
			{loadingPage} {pageError} {playMode} {session} {loadingMedia} {loadingMoreSources} {autoPlayOn} sourcesOpen={drawer === 'sources'} {headerTitle} {headerSub}
			overview={details?.overview ?? ''} {activeSaved} bestCandidate={pickBestCandidate()}
			candidatesCount={candidates.length} {starting} searching={sources.searching} {playerError}
			onRetryLoad={() => void loadInitial()}
			onSwitchToBrowser={() => { playMode = 'browser'; void attachPlayback(currentPos); }}
			onResumeSaved={(pos) => { const b = pickBestCandidate(); if (b) void startCandidate(b, 'browser', pos, true); }}
			onPlayBest={() => { const b = pickBestCandidate(); if (b) void startCandidate(b, 'browser', 0, true); }}
			onOpenSources={() => { drawer = 'sources'; }}
		/>
	{/if}

	{#if !uiHidden}
	<PlayerHud
		{hudVisible} {drawer} {kind} {headerTitle} immersive={device.immersive} captionsOn={!subs.subHidden && (subs.subMode !== 'off' || !!subs.secondary)} onCaptions={toggleCaptions} onMore={() => openUtility('more')} onImmersive={() => updateDevice({ immersive: !device.immersive })} episodeLabel={kind === 'show' ? `EP ${episode} / ${curSeasonEps.length || episode}` : ''} episodeName={curEpObj?.name ?? ''} {headerSub} previewSource={pipelineMode === 'direct' && (session?.transport === 'direct' || session?.transport === 'local') ? to(session.direct_url || session.media_url) : ''} runtimeMin={curEpObj?.runtime || details?.runtime || 0}
		{session} {activeCandidate} candidatesCount={candidates.length} {loadingMoreSources} seamlessStatus={timeline.seamlessStatus} stalled={timeline.stalled}
		{pipelineMode} {paused} {mpvAvailable} {pipAvailable} {isPiP}
		{volume} {muted} {isFullscreen} {totalDuration} {currentPos} {scrubbedPct} {bufferedPct}
		{remainingText} {endsAtText} chapters={session?.chapters ?? session?.probe?.chapters ?? []} {skipSegments} {activeSkip} skipCountdown={countdown.skipSeconds} nextCountdown={countdown.nextSeconds}
		hasNextEpisode={!!nextEpisodeTarget} {hlsLevels} {hlsLevelIdx} {browserQuality}
		onLeave={() => void goto(backHref || to(detailPath(kind, tmdb)))}
		onToggleDrawer={openDrawer}
		onTogglePlayPause={togglePlayPause} onSeekBy={seekBy}
		onScrubInput={scrub}
		onScrubCommit={commitSeek} onToggleMute={toggleMute} onVolumeChange={setVolumeLevel}
		onTogglePiP={() => void togglePiP()}
		onNextEpisode={nextEpisode}
		onToggleFullscreen={() => void toggleFullscreen()}
		onExecuteSkip={executeSkip} onCancelAutoSkip={() => countdown.cancel(activeSkip)}
	/>

	{/if}
	{#if drawer === 'quality' && !uiHidden}
		<PlayerSettingsPanel {device} {prefs} speed={playbackRate} remote={session?.transport === 'hls_remote'} canTranscode={!!session && ffmpegAvailable} levels={hlsLevels} level={hlsLevelIdx} quality={browserQuality} source={session?.source_name ?? ''} sourceContext={`${tmdb}:${season}:${episode}`} onTestSources={() => searchStreamReleases({ ...sourceSearchRequest(), mode: 'full', no_cache: true }).then((result) => result.sources ?? [])} onDevice={updateDevice} onPrefs={(patch) => { if (prefs) { prefs = { ...prefs, ...patch }; void saveStreamingPrefs(prefs).catch((e) => showFeedback(message(e))); } }} onSpeed={selectSpeed} onLevel={selectHlsLevel} onQuality={(q) => { browserQuality = q; if (session && playMode === 'browser') void attachPlayback(currentPos, q !== 'original', true, `Switching to ${q}...`); }} onOpen={(name) => { if (name === 'sources') openDrawer('sources'); else if (name === 'audio' || name === 'subtitles') openDrawer('audioSubs'); else openUtility(name); }} onClose={() => { drawer = 'none'; }} />
	{/if}
	{#if utility === 'more' || utility === 'stats' || utility === 'shortcuts'}
		<PlayerMorePanel panel={utility} video={videoEl} {session} position={currentPos} pipeline={pipelineMode} recoveries={freezeRecoveries} error={playerError} boost={device.boost} processing={processingStatus} canPip={pipAvailable} canMpv={mpvAvailable && !!session} onOpen={openUtility} onClose={() => { utility = 'none'; }} onSave={saveFrame} onPip={() => void togglePiP()} onMpv={() => { if (session) void triggerMPV(session.id); }} onKeep={() => { if (session) void setStreamKeep(session.id, !session.keep_file).then((s) => { session = s; }).catch((e) => showFeedback(message(e))); }} />
	{/if}

	<PlayerSourcesDrawer
		open={drawer === 'sources'} bind:queryText bind:sourceTab bind:resFilter preferredQuality={prefQuality} searching={sources.searching} {loadingMoreSources} searchError={sources.error} {fallbackNotice} {candidates} counts={sources.counts}
		activeCandidateKey={activeCandidate ? candidateKey(activeCandidate) : ''} {session} {mpvAvailable}
		onClose={() => { drawer = 'none'; }} onSearch={() => void runSearch(true)}
		onStartCandidate={(c, m) => { const pos = placeNow(); autoFallbacks = 0; drawer = 'none'; void startCandidate(c, m, pos, true, true); }}
		onSelectFile={(idx) => { pendingStartSec = 0; landed = false; if (session) void selectStreamFile(session.id, idx).then((s) => { session = s; attachedSessionKey = ''; }); }}
		onToggleKeep={() => { if (session) void setStreamKeep(session.id, !session.keep_file).then((s) => { session = s; }); }}
	/>

	{#if drawer === 'audio'}
		<PlayerAudioPopover tracks={session?.probe?.audio_tracks ?? []} {audioIdx} canSwitch={ffmpegAvailable && !!session} boost={device.boost} onBoost={(boost) => updateDevice({ boost })} onClose={() => { drawer = 'none'; }}
			onChangeAudio={(idx) => { if (idx === audioIdx) return; audioIdx = idx; if (session && playMode === 'browser') void attachPlayback(placeNow(), true, true, 'Switching audio...'); }} />
	{/if}
	<PlayerSubsDrawer
		open={drawer === 'audioSubs'} boost={device.boost} target={subs.target} secondary={subs.secondary?.label ?? ''} onBoost={(boost) => updateDevice({ boost })} onTarget={(target) => { subs.target = target; }} onClearSecondary={() => { subs.secondary = null; subs.target = 'primary'; }} onOpenStyle={() => openUtility('style')} {session} {audioIdx} {ffmpegAvailable} subMode={subs.subMode} subIndex={subs.subIndex} subLabel={subs.subLabel} subDelayMS={subs.subDelayMS} subSize={subs.subSize} activeSubVtt={subs.activeSubVtt} syncingSub={subs.syncingSub} syncNote={subs.syncNote} bind:subStyle bind:siteSubLangsInput={subs.siteSubLangsInput}
		siteSubCandidates={subs.siteSubCandidates} searchingSubs={subs.searchingSubs} loadingSub={subs.loadingSub} subError={subs.subError}
		onClose={() => { drawer = 'none'; }}
		onChangeAudio={(idx) => { audioIdx = idx; if (session && playMode === 'browser') void attachPlayback(currentPos, true, true, 'Switching audio...'); }}
		onTurnOff={turnOffSubtitles}
		onLoadEmbedded={(idx) => void loadEmbeddedSubtitle(idx)} onSearchOnline={() => void searchSubtitlesOnline()}
		onLoadSiteSub={(c) => void loadSiteSubtitleById(c.id, c.language, `${c.language.toUpperCase()} · ${c.site_name}`, false)}
		onAutoFindAndSync={() => void autoFindAndSyncSubtitle(false)} onSyncCurrentSub={() => void syncCurrentSubtitleToAudio()}
		onAdjustDelay={(d) => { subs.subDelayMS += d; showFeedback(`Subs ${subs.subDelayMS}ms`); void persistProgress(); }}
		onChangeSize={(sz) => { subs.subSize = sz; void persistProgress(); }} onUploadFile={(e) => void handleSubtitleFileUpload(e)}
		onStyleChange={(next) => { if (prefs) { prefs = { ...prefs, sub_style: next }; void saveStreamingPrefs(prefs).catch(() => {}); } }}
	/>

	{#if utility === 'style'}
		<PlayerSubtitleStylePanel context={subtitleContext} {device} styles={[...new Set([...(primaryAss?.styles.keys() ?? []), ...(secondaryAss?.styles.keys() ?? [])])]} onChange={updateDevice} onClose={() => openDrawer('audioSubs')} />
	{/if}

	<PlayerEpisodesDrawer
		open={drawer === 'episodes'} seasons={regularSeasons} selectedSeason={drawerSeason} currentSeason={season} currentEpisode={episode}
		episodes={drawerEps} {loadingEpisodes} progressMap={episodeProgressMap}
		onClose={() => { drawer = 'none'; }} onPickSeason={(s) => { drawerSeason = s; void loadSeasonEpisodes(s); }}
		onPickEpisode={(s, e) => void switchEpisode(s, e, true)} onShuffleEpisode={() => void shuffleEpisode()}
	/>
</div>

<style>
	/* The player covers the window, so the page keeps no scrollbar gutter beside it. */
	:global(html:has(.wp-stage)) { scrollbar-gutter: auto; overflow: hidden; }
	.wp-stage { position: fixed; inset: 0; height: 100dvh; overflow: hidden; background: #000; color: #fff; font-family: var(--font-ui); user-select: none; }
	.wp-stage.hide-cursor { cursor: none; }
	.wp-backdrop { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; filter: blur(18px) brightness(0.4); transform: scale(1.06); }
	.wp-backdrop-scrim { position: absolute; inset: 0; background: radial-gradient(circle at center, rgb(0 0 0 / 35%), rgb(0 0 0 / 88%)); }
	.wp-video { position: relative; width: 100%; height: 100%; object-fit: contain; background: transparent; cursor: pointer; }
	.wp-ripple { position: absolute; top: 50%; left: 50%; transform: translate(-50%, -50%); z-index: 8; padding: 12px 22px; border-radius: 999px; background: rgb(15 15 18 / 82%); border: 1px solid rgb(255 255 255 / 20%); font: 700 18px/1 var(--font-ui); pointer-events: none; }
</style>
