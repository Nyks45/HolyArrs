<script lang="ts">
	import type HlsType from 'hls.js';
	import { onDestroy, onMount } from 'svelte';
	import { goto, replaceState } from '$app/navigation';
	import Icon from '#lib/ui/Icon.svelte';
	import { api, message } from '#lib/core/api.ts';
	import { bytes } from '#lib/core/format.ts';
	import { to } from '#lib/core/paths.ts';
	import { toasts } from '#lib/core/toasts.svelte.ts';
	import { sized } from '#lib/core/tmdb-image.ts';
	import { hlsScript } from '#lib/core/trailer/hls-player.ts';
	import { detailPath, loadDetails, type TitleDetails } from '#lib/metadata/index.ts';
	import {
		ensureStreamHLS,
		fetchStreamSubtitle,
		formatDuration,
		formatSpeed,
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
		type StreamSourceCheck,
		type StreamSourceInfo,
		type StreamSubCandidate,
		type SubPref,
		type WatchEntry
	} from './stream-api.ts';
	import { activeCuesAt, parseWebVTT, type VTTCue } from './vtt.ts';

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
		backHref = ''
	}: {
		kind: 'movie' | 'show';
		tmdb: string;
		initialSeason?: number;
		initialEpisode?: number;
		autoResume?: boolean;
		backHref?: string;
	} = $props();

	let stageEl = $state<HTMLElement | undefined>(undefined);
	let videoEl = $state<HTMLVideoElement | undefined>(undefined);
	let subFileInput = $state<HTMLInputElement | undefined>(undefined);

	let details = $state<TitleDetails | null>(null);
	let loadingPage = $state(true);
	let pageError = $state('');

	let season = $state(1);
	let episode = $state(1);
	let seasonEpisodes = $state<Record<number, SeasonEp[]>>({});
	let loadingEpisodes = $state(false);

	let historyLatest = $state<WatchEntry | null>(null);
	let historyEpisodes = $state<WatchEntry[]>([]);

	let availableSources = $state<StreamSourceInfo[]>([]);
	let selectedSourceIds = $state<number[]>([]);
	let mpvAvailable = $state(false);
	let ffmpegAvailable = $state(false);

	let queryText = $state('');
	let searching = $state(false);
	let searchError = $state('');
	let candidates = $state<StreamCandidate[]>([]);
	let sourceChecks = $state<StreamSourceCheck[]>([]);
	let resFilter = $state<'all' | '2160p' | '1080p' | '720p' | 'sd'>('all');
	let onlySeeded = $state(true);
	let sortBy = $state<'seeders' | 'quality' | 'size_asc' | 'size_desc'>('seeders');

	let session = $state<SessionStatus | null>(null);
	let activeCandidate = $state<StreamCandidate | null>(null);
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
	let bufferedEnd = $state(0);
	let isScrubbing = $state(false);
	let scrubValue = $state(0);
	let hoverTime = $state<number | null>(null);
	let hoverPct = $state(0);
	let pipelineMode = $state<'none' | 'direct' | 'hls'>('none');
	let loadingMedia = $state(false);
	let playerError = $state('');

	let drawer = $state<'none' | 'sources' | 'audioSubs' | 'episodes'>('none');
	let hudVisible = $state(true);
	let feedbackText = $state('');

	// Subtitles state
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

	// Next-episode countdown state
	let nextCanceled = $state(false);
	let nextCountdown = $state(10);

	let hlsInstance: HlsType | null = null;
	let pollTimer: ReturnType<typeof setInterval> | null = null;
	let progressTimer: ReturnType<typeof setInterval> | null = null;
	let hudTimer: ReturnType<typeof setTimeout> | null = null;
	let feedbackTimer: ReturnType<typeof setTimeout> | null = null;
	let nextTimer: ReturnType<typeof setInterval> | null = null;
	let attachedSessionKey = '';
	let loadReqSeq = 0;
	let didInitialResumeDecision = false;
	let destroyed = false;

	let cachedHlsCtor: Promise<typeof HlsType> | null = null;
	function loadHlsCtor(): Promise<typeof HlsType> {
		cachedHlsCtor ??= (import(/* @vite-ignore */ to(hlsScript)) as Promise<{ default: typeof HlsType }>).then((m) => m.default);
		cachedHlsCtor.catch(() => (cachedHlsCtor = null));
		return cachedHlsCtor;
	}

	const extIds = $derived(
		(kind === 'movie' ? details?.movie?.ids : details?.show?.ids) as Record<string, string | undefined> | undefined
	);
	const imdbId = $derived(extIds?.imdb ?? '');

	const regularSeasons = $derived.by(() => {
		const list = details?.show?.seasons ?? [];
		const nonZero = list.filter((s) => s.number > 0);
		return nonZero.length > 0 ? nonZero : list;
	});

	const curSeasonEps = $derived(seasonEpisodes[season] ?? []);
	const curEpObj = $derived(curSeasonEps.find((e) => e.number === episode));

	const activeSaved = $derived.by(() => {
		if (kind === 'show') {
			return historyEpisodes.find((h) => (h.season ?? 0) === season && (h.episode ?? 0) === episode) ?? null;
		}
		return historyLatest;
	});

	const episodeProgressMap = $derived.by(() => {
		const m: Record<string, WatchEntry> = {};
		for (const ep of historyEpisodes) {
			m[`${ep.season ?? 0}:${ep.episode ?? 0}`] = ep;
		}
		return m;
	});

	const nextEpisodeTarget = $derived.by(() => {
		if (kind !== 'show') return null;
		const idx = curSeasonEps.findIndex((e) => e.number === episode);
		if (idx >= 0 && idx + 1 < curSeasonEps.length) {
			return { season, episode: curSeasonEps[idx + 1].number, name: curSeasonEps[idx + 1].name };
		}
		const sIdx = regularSeasons.findIndex((s) => s.number === season);
		if (sIdx >= 0 && sIdx + 1 < regularSeasons.length) {
			const nextS = regularSeasons[sIdx + 1];
			return { season: nextS.number, episode: 1, name: '' };
		}
		return null;
	});

	const backdropUrl = $derived.by(() => {
		const raw = details?.backdrop || details?.poster || activeSaved?.backdrop || activeSaved?.poster || '';
		return raw ? sized(raw, 'large') : '';
	});

	const headerTitle = $derived(details?.name || activeSaved?.title || 'Watch');
	const headerSub = $derived(
		kind === 'show'
			? `S${String(season).padStart(2, '0')} E${String(episode).padStart(2, '0')}${curEpObj?.name ? ` · ${curEpObj.name}` : ''}`
			: details?.year
				? String(details.year)
				: ''
	);

	const totalDuration = $derived(session?.probe?.duration_sec || activeSaved?.duration_sec || 0);
	const currentPos = $derived(
		isScrubbing ? scrubValue : pipelineMode === 'hls' ? seekOffset + videoTime : videoTime
	);
	const remainingText = $derived(formatTimeLeft(currentPos, totalDuration));
	const scrubbedPct = $derived(totalDuration > 0 ? Math.min(100, Math.max(0, (currentPos / totalDuration) * 100)) : 0);
	const bufferedPct = $derived.by(() => {
		if (totalDuration <= 0) return 0;
		const absBuf = pipelineMode === 'hls' ? seekOffset + bufferedEnd : bufferedEnd;
		return Math.min(100, Math.max(scrubbedPct, (absBuf / totalDuration) * 100));
	});

	const visibleCues = $derived(
		subMode === 'off' || parsedCues.length === 0 ? [] : activeCuesAt(parsedCues, currentPos, subDelayMS)
	);

	const nearEpisodeEnd = $derived(
		kind === 'show' &&
			!!nextEpisodeTarget &&
			!nextCanceled &&
			pipelineMode !== 'none' &&
			totalDuration > 90 &&
			totalDuration - currentPos <= 25 &&
			totalDuration - currentPos >= 0
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

	function candKey(c: StreamCandidate): string {
		return `${c.source_id}:${c.info_hash || c.link}:${c.title}`;
	}

	function showFeedback(text: string) {
		feedbackText = text;
		if (feedbackTimer) clearTimeout(feedbackTimer);
		feedbackTimer = setTimeout(() => (feedbackText = ''), 1400);
	}

	function wakeHud() {
		hudVisible = true;
		if (hudTimer) clearTimeout(hudTimer);
		if (!paused && drawer === 'none' && pipelineMode !== 'none' && !isScrubbing) {
			hudTimer = setTimeout(() => {
				if (!paused && drawer === 'none' && !isScrubbing) hudVisible = false;
			}, 3000);
		}
	}

	function currentSubPref(): SubPref {
		return {
			mode: subMode,
			language: subLang || undefined,
			label: subLabel || undefined,
			index: subMode === 'embedded' ? subIndex : undefined,
			site_id: subMode === 'site' ? subSiteId : undefined,
			site_key: subMode === 'site' ? subSiteKey : undefined,
			delay_ms: subDelayMS,
			size: subSize
		};
	}

	function buildWatchEntry(posOverride?: number, completedOverride?: boolean): WatchEntry | null {
		const id = tmdb.trim();
		if (!id) return null;
		const pos = posOverride ?? currentPos;
		const dur = session?.probe?.duration_sec || activeSaved?.duration_sec || 0;
		return {
			kind,
			tmdb: id,
			imdb: imdbId || activeSaved?.imdb || '',
			title: details?.name || activeSaved?.title || activeCandidate?.title || '',
			year: details?.year || activeSaved?.year || 0,
			poster: details?.poster || activeSaved?.poster || '',
			backdrop: details?.backdrop || activeSaved?.backdrop || '',
			season: kind === 'show' ? season : 0,
			episode: kind === 'show' ? episode : 0,
			episode_name: kind === 'show' ? curEpObj?.name || activeSaved?.episode_name || '' : '',
			position_sec: Math.max(0, pos),
			duration_sec: dur,
			completed: completedOverride ?? false,
			source_id: activeCandidate?.source_id ?? session?.source_id ?? activeSaved?.source_id ?? 0,
			source_name: activeCandidate?.source_name ?? session?.source_name ?? activeSaved?.source_name ?? '',
			release_title: activeCandidate?.title ?? session?.release_title ?? activeSaved?.release_title ?? '',
			release_link: activeCandidate?.link ?? activeSaved?.release_link ?? '',
			info_hash: activeCandidate?.info_hash ?? activeSaved?.info_hash ?? '',
			resolution: activeCandidate?.resolution ?? activeSaved?.resolution ?? '',
			file_index: session?.selected_file ?? activeSaved?.file_index ?? 0,
			quality: browserQuality,
			audio_index: audioIdx,
			playback_rate: playbackRate,
			subtitle: currentSubPref()
		};
	}

	async function persistProgress(posOverride?: number, keepalive = false, completedOverride?: boolean) {
		if (!tmdb.trim()) return;
		if (posOverride === undefined && pipelineMode === 'none' && !session?.mpv_running) return;
		const payload = buildWatchEntry(posOverride, completedOverride);
		if (!payload) return;
		try {
			const saved = await saveWatchProgress(payload, keepalive);
			if (destroyed) return;
			historyLatest = saved;
			if (kind === 'show') {
				const idx = historyEpisodes.findIndex(
					(e) => (e.season ?? 0) === (saved.season ?? 0) && (e.episode ?? 0) === (saved.episode ?? 0)
				);
				if (idx >= 0) {
					historyEpisodes = [...historyEpisodes.slice(0, idx), saved, ...historyEpisodes.slice(idx + 1)];
				} else {
					historyEpisodes = [...historyEpisodes, saved];
				}
			}
		} catch {
			// Non-fatal background persistence error.
		}
	}

	function destroyVideoPipeline() {
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
		paused = true;
		bufferedEnd = 0;
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
			if (destroyed || !session || session.id !== id) return;
			session = st;

			if (st.state === 'error') {
				pendingMPV = false;
				return;
			}

			if (pendingMPV && st.files.length > 0 && st.state !== 'resolving') {
				pendingMPV = false;
				await triggerMPV(st.id, pendingStartSec);
				return;
			}

			if (playMode === 'browser' && st.state === 'ready' && !st.mpv_running) {
				const key = `${st.id}:${st.selected_file}`;
				if (attachedSessionKey !== key) {
					attachedSessionKey = key;
					if (pendingFileIdx !== null && pendingFileIdx !== st.selected_file && pendingFileIdx < st.files.length) {
						const wantedIdx = pendingFileIdx;
						pendingFileIdx = null;
						await selectStreamFile(st.id, wantedIdx);
						return;
					}
					pendingFileIdx = null;
					await attachPlayback(pendingStartSec);
					if (subMode === 'embedded' && st.probe?.subtitle_tracks.some((t) => t.index === subIndex)) {
						await loadEmbeddedSubtitle(subIndex, true);
					}
				}
			}
		} catch {
			// Ignore transient poll errors while warming up.
		}
	}

	async function loadSeasonEpisodes(sNum: number) {
		if (kind !== 'show' || !tmdb || seasonEpisodes[sNum]) return;
		loadingEpisodes = true;
		try {
			const res = await api.get<{ episodes?: SeasonEp[] }>(
				`/metadata/show/${encodeURIComponent(tmdb)}/seasons/${sNum}`
			);
			if (!destroyed) {
				seasonEpisodes = { ...seasonEpisodes, [sNum]: res.episodes ?? [] };
			}
		} catch {
			// Episode list still works with numbers if metadata lookup fails.
		} finally {
			loadingEpisodes = false;
		}
	}

	async function loadInitial() {
		loadingPage = true;
		pageError = '';
		try {
			const [d, hist, srcs] = await Promise.all([
				loadDetails(api.get, kind, tmdb),
				getTitleWatchHistory(kind, tmdb).catch(() => ({ latest: null, episodes: [] })),
				listStreamSources(kind)
			]);
			if (destroyed) return;
			details = d ?? null;
			historyLatest = hist.latest;
			historyEpisodes = hist.episodes ?? [];
			availableSources = srcs.sources ?? [];
			mpvAvailable = srcs.mpv_available;
			ffmpegAvailable = srcs.ffmpeg_available;
			queryText = d?.name || hist.latest?.title || '';

			if (kind === 'show') {
				const sDefault =
					initialSeason > 0
						? initialSeason
						: hist.latest?.season && hist.latest.season > 0
							? hist.latest.season
							: regularSeasons[0]?.number ?? 1;
				const eDefault =
					initialEpisode > 0
						? initialEpisode
						: hist.latest?.episode && hist.latest.episode > 0
							? hist.latest.episode
							: 1;
				season = sDefault;
				episode = eDefault;
				await loadSeasonEpisodes(sDefault);
			}

			const sv =
				kind === 'show'
					? (historyEpisodes.find((h) => (h.season ?? 0) === season && (h.episode ?? 0) === episode) ?? null)
					: historyLatest;
			applySavedPrefs(sv);
			void runSearch();

			if (!didInitialResumeDecision) {
				didInitialResumeDecision = true;
				if (autoResume && sv && sv.release_link && !sv.completed) {
					void resumeFromSaved(sv, sv.position_sec);
				}
			}
		} catch (e) {
			pageError = message(e);
		} finally {
			loadingPage = false;
			wakeHud();
		}
	}

	function applySavedPrefs(sv: WatchEntry | null) {
		if (!sv) return;
		if (sv.quality === '1080p' || sv.quality === '720p' || sv.quality === '480p' || sv.quality === 'original') {
			browserQuality = sv.quality;
		}
		if (typeof sv.audio_index === 'number' && sv.audio_index >= 0) audioIdx = sv.audio_index;
		if (typeof sv.playback_rate === 'number' && sv.playback_rate >= 0.5 && sv.playback_rate <= 2) {
			playbackRate = sv.playback_rate;
		}
		if (sv.subtitle) {
			subDelayMS = sv.subtitle.delay_ms ?? 0;
			subSize = sv.subtitle.size ?? 'md';
			if (sv.subtitle.mode === 'off') {
				subMode = 'off';
				parsedCues = [];
			} else if (sv.subtitle.mode === 'site' && sv.subtitle.site_id) {
				subMode = 'site';
				subSiteId = sv.subtitle.site_id;
				subLang = sv.subtitle.language ?? '';
				subLabel = sv.subtitle.label ?? '';
			} else if (sv.subtitle.mode === 'embedded' && typeof sv.subtitle.index === 'number') {
				subMode = 'embedded';
				subIndex = sv.subtitle.index;
				subLabel = sv.subtitle.label ?? '';
			}
		}
	}

	async function runSearch() {
		const q = (queryText || details?.name || activeSaved?.title || '').trim();
		if (!q && !imdbId && !tmdb) return;
		searching = true;
		searchError = '';
		try {
			const res = await searchStreamReleases({
				query: q,
				kind,
				year: details?.year || activeSaved?.year || 0,
				season: kind === 'show' ? season : 0,
				episode: kind === 'show' ? episode : 0,
				imdb: imdbId,
				tmdb: Number(tmdb) || 0,
				source_ids: selectedSourceIds
			});
			if (destroyed) return;
			candidates = res.candidates ?? [];
			sourceChecks = res.sources ?? [];
		} catch (e) {
			searchError = message(e);
			candidates = [];
		} finally {
			searching = false;
		}
	}

	function pickBestCandidate(): StreamCandidate | null {
		if (filteredCandidates.length > 0) return filteredCandidates[0];
		if (candidates.length > 0) return candidates[0];
		return null;
	}

	async function resumeFromSaved(sv: WatchEntry, startAt: number) {
		if (!sv.release_link) return;
		const synthetic: StreamCandidate = {
			source_id: sv.source_id ?? 0,
			source_name: sv.source_name || 'Saved Source',
			title: sv.release_title || sv.title,
			link: sv.release_link,
			info_hash: sv.info_hash || '',
			protocol: 'torrent',
			size: 0,
			seeders: 1,
			peers: 0,
			resolution: sv.resolution || '',
			resolution_num: 0,
			source_tag: '',
			codec: '',
			hdr: '',
			audio: '',
			languages: []
		};
		pendingFileIdx = typeof sv.file_index === 'number' && sv.file_index > 0 ? sv.file_index : null;
		await startCandidate(synthetic, 'browser', startAt, true);
		if (sv.subtitle?.mode === 'site' && sv.subtitle.site_id) {
			await loadSiteSubtitleById(sv.subtitle.site_id, sv.subtitle.language ?? '', sv.subtitle.label ?? '', true);
		}
	}

	async function startCandidate(
		c: StreamCandidate,
		mode: 'browser' | 'mpv',
		startAt = 0,
		keepExternalSubs = true
	) {
		if (pipelineMode !== 'none' && currentPos > 5) {
			await persistProgress(currentPos);
		}
		destroyVideoPipeline();
		stopPolling();
		session = null;
		attachedSessionKey = '';
		activeCandidate = c;
		playMode = mode;
		pendingMPV = mode === 'mpv';
		pendingStartSec = Math.max(0, startAt);
		starting = true;
		playerError = '';
		seekOffset = pendingStartSec;
		videoTime = pendingStartSec;
		nextCanceled = false;

		if (!keepExternalSubs || subMode === 'embedded') {
			subMode = 'off';
			parsedCues = [];
			subFile = '';
		}

		try {
			const st = await startStream({
				source_id: c.source_id,
				source_name: c.source_name,
				link: c.link,
				title: headerSub ? `${headerTitle} · ${headerSub}` : headerTitle,
				release_title: c.title,
				kind,
				season: kind === 'show' ? season : 0,
				episode: kind === 'show' ? episode : 0,
				keep_file: false
			});
			if (destroyed) return;
			session = st;
			startPolling(st.id);
			if (st.state === 'ready' || (pendingMPV && st.files.length > 0 && st.state !== 'resolving')) {
				void pollSession(st.id);
			}
			wakeHud();
		} catch (e) {
			pendingMPV = false;
			playerError = message(e);
			toasts.push('Could not start stream', 'fail', { body: playerError });
		} finally {
			starting = false;
		}
	}

	async function attachPlayback(startSec: number) {
		const st = session;
		if (!st || !videoEl) return;
		const req = ++loadReqSeq;
		destroyVideoPipeline();
		loadReqSeq = req;
		loadingMedia = true;
		playerError = '';
		seekOffset = startSec;
		videoTime = 0;

		try {
			const canDirect = !!st.probe?.direct_playable && browserQuality === 'original' && audioIdx === 0;
			if (canDirect) {
				pipelineMode = 'direct';
				if (startSec > 0.5) {
					videoEl.addEventListener(
						'loadedmetadata',
						() => {
							if (videoEl) videoEl.currentTime = startSec;
						},
						{ once: true }
					);
				}
				videoEl.src = to(st.media_url);
				videoEl.playbackRate = playbackRate;
				videoEl.volume = volume;
				videoEl.muted = muted;
				await videoEl.play().catch(() => {});
				wakeHud();
				return;
			}

			if (!ffmpegAvailable) {
				throw new Error('ffmpeg is not installed for streaming remux/transcode');
			}
			const hlsRes = await ensureStreamHLS(st.id, browserQuality, audioIdx, startSec);
			if (req !== loadReqSeq || destroyed || !session || session.id !== st.id || !videoEl) return;

			seekOffset = hlsRes.start_sec;
			pipelineMode = 'hls';
			const srcUrl = to(hlsRes.playlist_url);

			const Hls = await loadHlsCtor();
			if (req !== loadReqSeq || destroyed || !videoEl) return;

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
					videoEl.volume = volume;
					videoEl.muted = muted;
					void videoEl.play().catch(() => {});
					wakeHud();
				});
			} else if (videoEl.canPlayType('application/vnd.apple.mpegurl')) {
				videoEl.src = srcUrl;
				videoEl.playbackRate = playbackRate;
				videoEl.volume = volume;
				videoEl.muted = muted;
				await videoEl.play().catch(() => {});
			} else {
				throw new Error('HLS playback is not supported in this browser');
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

	async function triggerMPV(id: string, atSec = currentPos) {
		const resumeAt = Math.max(0, atSec);
		if (resumeAt > 2) await persistProgress(resumeAt);
		destroyVideoPipeline();
		playMode = 'mpv';
		playerError = '';
		try {
			const updated = await launchStreamMPV(id, {
				audio_lang: 'eng,en,tur,tr',
				sub_lang: 'tur,tr,eng,en',
				start_sec: resumeAt,
				sub_file: subFile,
				watch: buildWatchEntry(resumeAt) ?? undefined
			});
			session = updated;
			toasts.push('Playing in desktop mpv', 'ok', {
				body: resumeAt > 5 ? `Resumed from ${formatDuration(resumeAt)}` : undefined
			});
		} catch (e) {
			playerError = message(e);
			toasts.push('Could not open mpv', 'fail', { body: playerError });
		}
	}

	function togglePlayPause() {
		if (!videoEl || pipelineMode === 'none') return;
		if (videoEl.paused) {
			void videoEl.play().catch(() => {});
			showFeedback('Play');
		} else {
			videoEl.pause();
			showFeedback('Pause');
		}
		wakeHud();
	}

	function seekBy(deltaSec: number) {
		if (!session || pipelineMode === 'none') return;
		const target = Math.max(0, Math.min(totalDuration || Infinity, currentPos + deltaSec));
		commitSeek(target);
		showFeedback(deltaSec > 0 ? `+${deltaSec}s` : `${deltaSec}s`);
		wakeHud();
	}

	function commitSeek(targetSec: number) {
		isScrubbing = false;
		const clamped = Math.max(0, totalDuration > 0 ? Math.min(totalDuration - 0.5, targetSec) : targetSec);
		if (!session || !videoEl) return;

		if (pipelineMode === 'direct') {
			videoEl.currentTime = clamped;
			videoTime = clamped;
			void persistProgress(clamped);
			return;
		}
		if (pipelineMode === 'hls') {
			let maxEnd = 0;
			for (let i = 0; i < videoEl.buffered.length; i++) {
				if (videoEl.buffered.end(i) > maxEnd) maxEnd = videoEl.buffered.end(i);
			}
			if (clamped >= seekOffset && clamped <= seekOffset + maxEnd) {
				videoEl.currentTime = clamped - seekOffset;
				videoTime = clamped - seekOffset;
				void persistProgress(clamped);
				return;
			}
			void attachPlayback(clamped).then(() => void persistProgress(clamped));
		}
	}

	function changeQuality(q: 'original' | '1080p' | '720p' | '480p') {
		const resumeAt = Math.max(0, currentPos);
		browserQuality = q;
		void persistProgress(resumeAt);
		if (session && playMode === 'browser' && session.state === 'ready') {
			void attachPlayback(resumeAt);
		}
	}

	function changeAudio(idx: number) {
		const resumeAt = Math.max(0, currentPos);
		audioIdx = idx;
		void persistProgress(resumeAt);
		if (session && playMode === 'browser' && session.state === 'ready') {
			void attachPlayback(resumeAt);
		}
	}

	function changeSpeed(rate: number) {
		playbackRate = rate;
		if (videoEl) videoEl.playbackRate = rate;
		void persistProgress();
	}

	function setVolumeLevel(v: number) {
		volume = Math.max(0, Math.min(1, v));
		if (volume > 0 && muted) muted = false;
		if (videoEl) {
			videoEl.volume = volume;
			videoEl.muted = muted;
		}
	}

	function toggleMute() {
		muted = !muted;
		if (videoEl) videoEl.muted = muted;
		showFeedback(muted ? 'Muted' : 'Unmuted');
	}

	async function toggleFullscreen() {
		if (typeof document === 'undefined' || !stageEl) return;
		try {
			if (document.fullscreenElement) {
				await document.exitFullscreen();
			} else {
				await stageEl.requestFullscreen();
			}
		} catch {
			// Ignore fullscreen denial.
		}
	}

	// Subtitle loading helpers
	function turnOffSubtitles() {
		subMode = 'off';
		subLabel = '';
		subSiteId = '';
		subFile = '';
		parsedCues = [];
		void persistProgress();
	}

	async function loadEmbeddedSubtitle(idx: number, silent = false) {
		if (!session) return;
		loadingSub = true;
		subError = '';
		try {
			const res = await fetch(
				to(`/api/v1/stream/${encodeURIComponent(session.id)}/sub/${encodeURIComponent(session.token)}/${idx}`),
				{ credentials: 'same-origin' }
			);
			if (!res.ok) throw new Error(await res.text());
			const vtt = await res.text();
			parsedCues = parseWebVTT(vtt);
			subMode = 'embedded';
			subIndex = idx;
			subSiteId = '';
			subFile = '';
			const tr = session.probe?.subtitle_tracks.find((t) => t.index === idx);
			subLang = tr?.language ?? '';
			subLabel = formatSubTrackLabel(idx, tr?.language, tr?.title, tr?.codec);
			if (!silent) toasts.push(`Subtitles: ${subLabel}`, 'ok');
			void persistProgress();
		} catch (e) {
			subError = message(e);
		} finally {
			loadingSub = false;
		}
	}

	async function searchSubtitlesOnline() {
		searchingSubs = true;
		subError = '';
		const langs = siteSubLangsInput
			.split(/[,\s]+/)
			.map((s) => s.trim().toLowerCase())
			.filter(Boolean);
		try {
			const res = await searchStreamSubtitles({
				kind,
				title: headerTitle,
				year: details?.year || activeSaved?.year || 0,
				season: kind === 'show' ? season : 0,
				episode: kind === 'show' ? episode : 0,
				imdb: imdbId,
				tmdb,
				release_name: activeCandidate?.title ?? session?.release_title ?? '',
				languages: langs.length ? langs : undefined
			});
			siteSubCandidates = res.candidates ?? [];
		} catch (e) {
			subError = message(e);
			siteSubCandidates = [];
		} finally {
			searchingSubs = false;
		}
	}

	async function loadSiteSubtitle(c: StreamSubCandidate) {
		const label = `${c.language.toUpperCase()} · ${c.site_name}${c.release_names?.[0] ? ` (${c.release_names[0].slice(0, 32)})` : ''}`;
		subSiteKey = c.release_names?.[0] ?? '';
		await loadSiteSubtitleById(c.id, c.language, label, false);
	}

	async function loadSiteSubtitleById(id: string, lang: string, label: string, silent: boolean) {
		loadingSub = true;
		subError = '';
		try {
			const res = await fetchStreamSubtitle(id, session?.id ?? '');
			parsedCues = parseWebVTT(res.vtt);
			subMode = 'site';
			subSiteId = id;
			subLang = res.language || lang;
			subLabel = label || (subLang ? subLang.toUpperCase() : 'Online subtitle');
			subFile = res.sub_file ?? '';
			if (!silent) toasts.push(`Loaded ${subLabel} (${parsedCues.length} cues)`, 'ok');
			void persistProgress();
		} catch (e) {
			subError = message(e);
		} finally {
			loadingSub = false;
		}
	}

	async function handleSubtitleFileUpload(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		if (!file) return;
		loadingSub = true;
		subError = '';
		try {
			const text = await file.text();
			const res = await uploadStreamSubtitle(file.name, text, '', session?.id ?? '');
			parsedCues = parseWebVTT(res.vtt);
			subMode = 'upload';
			subLang = res.language ?? '';
			subLabel = file.name;
			subFile = res.sub_file ?? '';
			toasts.push(`Loaded ${file.name} (${parsedCues.length} cues)`, 'ok');
			void persistProgress();
		} catch (err) {
			subError = message(err);
		} finally {
			loadingSub = false;
			input.value = '';
		}
	}

	function adjustSubDelay(deltaMS: number) {
		subDelayMS += deltaMS;
		showFeedback(`Sub sync ${subDelayMS >= 0 ? '+' : ''}${(subDelayMS / 1000).toFixed(2)}s`);
		void persistProgress();
	}

	async function selectPackFile(idx: number) {
		if (!session) return;
		destroyVideoPipeline();
		attachedSessionKey = '';
		pendingStartSec = 0;
		try {
			session = await selectStreamFile(session.id, idx);
		} catch (e) {
			playerError = message(e);
		}
	}

	async function switchEpisode(nextSeason: number, nextEpisode: number, autoPlayBest = true) {
		if (pipelineMode !== 'none' && currentPos > 5) {
			await persistProgress(currentPos);
		}
		stopNextCountdown();
		destroyVideoPipeline();
		stopPolling();
		if (session && !session.mpv_running) {
			void stopStream(session.id).catch(() => {});
		}
		session = null;
		activeCandidate = null;
		attachedSessionKey = '';
		season = nextSeason;
		episode = nextEpisode;
		pendingStartSec = 0;
		seekOffset = 0;
		videoTime = 0;
		drawer = 'none';
		await loadSeasonEpisodes(nextSeason);

		const savedEp = episodeProgressMap[`${nextSeason}:${nextEpisode}`];
		if (savedEp) applySavedPrefs(savedEp);

		const url = to(`/watch/show/${encodeURIComponent(tmdb)}?s=${nextSeason}&e=${nextEpisode}${backHref ? `&from=${encodeURIComponent(backHref)}` : ''}`);
		replaceState(url, {});

		await runSearch();
		if (!autoPlayBest) return;

		if (savedEp?.release_link && !savedEp.completed && savedEp.position_sec > 10) {
			await resumeFromSaved(savedEp, savedEp.position_sec);
			return;
		}
		const best = pickBestCandidate();
		if (best) {
			await startCandidate(best, 'browser', 0, true);
		} else {
			drawer = 'sources';
		}
	}

	function startNextCountdown() {
		if (!nextEpisodeTarget || nextTimer || nextCanceled) return;
		nextCountdown = 10;
		nextTimer = setInterval(() => {
			if (nextCountdown <= 1) {
				stopNextCountdown();
				if (nextEpisodeTarget) {
					void switchEpisode(nextEpisodeTarget.season, nextEpisodeTarget.episode, true);
				}
				return;
			}
			nextCountdown--;
		}, 1000);
	}

	function stopNextCountdown() {
		if (nextTimer) {
			clearInterval(nextTimer);
			nextTimer = null;
		}
	}

	function onKeyDown(e: KeyboardEvent) {
		const target = e.target as HTMLElement | null;
		if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT')) {
			return;
		}
		wakeHud();
		switch (e.key.toLowerCase()) {
			case ' ':
			case 'k':
				e.preventDefault();
				togglePlayPause();
				break;
			case 'arrowleft':
			case 'j':
				e.preventDefault();
				seekBy(-10);
				break;
			case 'arrowright':
			case 'l':
				e.preventDefault();
				seekBy(10);
				break;
			case 'arrowup':
				e.preventDefault();
				setVolumeLevel(volume + 0.1);
				showFeedback(`Vol ${Math.round(volume * 100)}%`);
				break;
			case 'arrowdown':
				e.preventDefault();
				setVolumeLevel(volume - 0.1);
				showFeedback(`Vol ${Math.round(volume * 100)}%`);
				break;
			case 'm':
				toggleMute();
				break;
			case 'f':
				void toggleFullscreen();
				break;
			case 'c':
				if (subMode === 'off') {
					drawer = 'audioSubs';
				} else {
					turnOffSubtitles();
					showFeedback('Subtitles Off');
				}
				break;
			case 'g':
				adjustSubDelay(-250);
				break;
			case 'h':
				adjustSubDelay(250);
				break;
			case 's':
				drawer = drawer === 'sources' ? 'none' : 'sources';
				break;
			case 'e':
				if (kind === 'show') drawer = drawer === 'episodes' ? 'none' : 'episodes';
				break;
			case 'escape':
				if (drawer !== 'none') drawer = 'none';
				break;
		}
	}

	function exitPlayerHref(): string {
		return backHref || to(detailPath(kind, tmdb));
	}

	function leavePlayer() {
		if (pipelineMode !== 'none' && currentPos > 5) {
			void persistProgress(currentPos, true);
		}
		void goto(exitPlayerHref());
	}

	function formatAudioLabel(idx: number, lang?: string, title?: string, codec?: string, ch?: number): string {
		const chLabel = ch === 6 ? '5.1' : ch === 8 ? '7.1' : ch === 2 ? '2.0' : ch ? `${ch}ch` : '';
		const parts = [lang?.toUpperCase(), title, codec?.toUpperCase(), chLabel].filter(Boolean);
		return parts.join(' · ') || `Audio ${idx + 1}`;
	}

	function formatSubTrackLabel(idx: number, lang?: string, title?: string, codec?: string): string {
		const parts = [lang?.toUpperCase(), title, codec?.toUpperCase()].filter(Boolean);
		return parts.join(' · ') || `Embedded Track ${idx + 1}`;
	}

	$effect(() => {
		if (drawer === 'audioSubs' && siteSubCandidates.length === 0 && !searchingSubs && details) {
			void searchSubtitlesOnline();
		}
	});

	$effect(() => {
		if (nearEpisodeEnd) {
			startNextCountdown();
		} else {
			stopNextCountdown();
		}
	});

	onMount(() => {
		void loadInitial();
		progressTimer = setInterval(() => {
			if (!paused && pipelineMode !== 'none' && currentPos > 5) {
				void persistProgress();
			}
		}, 8000);

		const onFsChange = () => {
			isFullscreen = !!document.fullscreenElement;
		};
		const onBeforeUnload = () => {
			if (pipelineMode !== 'none' && currentPos > 5) {
				void persistProgress(currentPos, true);
			}
			if (session && !session.mpv_running) {
				void stopStream(session.id).catch(() => {});
			}
		};
		document.addEventListener('fullscreenchange', onFsChange);
		window.addEventListener('beforeunload', onBeforeUnload);
		return () => {
			document.removeEventListener('fullscreenchange', onFsChange);
			window.removeEventListener('beforeunload', onBeforeUnload);
		};
	});

	onDestroy(() => {
		destroyed = true;
		if (pipelineMode !== 'none' && currentPos > 5) {
			void persistProgress(currentPos, true);
		}
		if (progressTimer) clearInterval(progressTimer);
		if (hudTimer) clearTimeout(hudTimer);
		if (feedbackTimer) clearTimeout(feedbackTimer);
		stopNextCountdown();
		destroyVideoPipeline();
		stopPolling();
		if (session && !session.mpv_running && !session.keep_file) {
			void stopStream(session.id).catch(() => {});
		}
	});
</script>

<svelte:head>
	<title>{headerTitle} · Watch · HolyArrs</title>
</svelte:head>

<svelte:window onkeydown={onKeyDown} />

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
	bind:this={stageEl}
	class="wp-stage"
	class:hide-cursor={!hudVisible && !paused && drawer === 'none'}
	onmousemove={wakeHud}
	ontouchstart={wakeHud}
	data-testid="watch-player"
>
	<!-- Backdrop image behind idle/loading player -->
	{#if backdropUrl && pipelineMode === 'none'}
		<img class="wp-backdrop" src={backdropUrl} alt="" />
		<div class="wp-backdrop-scrim"></div>
	{/if}

	<!-- Video Stage -->
	<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions, a11y_media_has_caption -->
	<video
		bind:this={videoEl}
		playsinline
		class="wp-video"
		class:cover={fitMode === 'cover'}
		onclick={togglePlayPause}
		ondblclick={() => void toggleFullscreen()}
		ontimeupdate={() => {
			if (!videoEl || isScrubbing) return;
			videoTime = videoEl.currentTime;
			let maxB = 0;
			for (let i = 0; i < videoEl.buffered.length; i++) {
				if (videoEl.buffered.end(i) > maxB) maxB = videoEl.buffered.end(i);
			}
			bufferedEnd = maxB;
		}}
		onseeked={() => {
			if (videoEl && !isScrubbing) videoTime = videoEl.currentTime;
		}}
		onplay={() => {
			paused = false;
			wakeHud();
		}}
		onpause={() => {
			paused = true;
			hudVisible = true;
			if (pipelineMode !== 'none' && currentPos > 3) void persistProgress();
		}}
		onended={() => {
			paused = true;
			hudVisible = true;
			void persistProgress(totalDuration || currentPos, false, true);
			if (nextEpisodeTarget) startNextCountdown();
		}}
		onloadedmetadata={() => {
			if (videoEl) {
				videoEl.playbackRate = playbackRate;
				videoEl.volume = volume;
				videoEl.muted = muted;
			}
		}}
	></video>

	<!-- Custom Netflix Subtitle Renderer (supports embedded, online sites, uploads & sync offset) -->
	{#if visibleCues.length > 0 && pipelineMode !== 'none'}
		<div class="wp-captions size-{subSize}" class:hud-up={hudVisible}>
			{#each visibleCues as cue, idx (cue.start + ':' + idx)}
				<div class="wp-cap-cue">
					{#each cue.lines as line, li (li)}
						{#if li > 0}<br />{/if}{line}
					{/each}
				</div>
			{/each}
		</div>
	{/if}

	<!-- Center Feedback Ripple -->
	{#if feedbackText}
		<div class="wp-ripple" aria-live="polite">{feedbackText}</div>
	{/if}

	<!-- Buffering / Landing / Resume / mpv Overlay -->
	{#if pipelineMode === 'none'}
		<div class="wp-center-panel">
			{#if loadingPage}
				<div class="wp-loader">
					<div class="wp-spin"></div>
					<p>Loading title…</p>
				</div>
			{:else if pageError}
				<div class="wp-card">
					<h2>Could not load player</h2>
					<p class="wp-muted">{pageError}</p>
					<div class="wp-card-acts">
						<button type="button" class="wp-cta wp-cta-white" onclick={() => void loadInitial()}>Retry</button>
						<button type="button" class="wp-cta wp-cta-glass" onclick={leavePlayer}>Go Back</button>
					</div>
				</div>
			{:else if playMode === 'mpv' && session}
				<div class="wp-card">
					<h2>{session.mpv_running ? 'Playing in Desktop mpv' : pendingMPV ? 'Starting mpv…' : 'mpv Exited'}</h2>
					<p class="wp-muted">{session.release_title} · {session.seeders} seeds · {formatSpeed(session.download_bps)}</p>
					<div class="wp-card-acts">
						<button
							type="button"
							class="wp-cta wp-cta-white"
							onclick={() => {
								playMode = 'browser';
								attachedSessionKey = '';
								void pollSession(session!.id);
							}}
						>
							<Icon name="play" size={18} />
							<span>Play in Browser</span>
						</button>
						{#if mpvAvailable && session.files.length > 0}
							<button type="button" class="wp-cta wp-cta-glass" onclick={() => void triggerMPV(session!.id, currentPos)}>
								Relaunch mpv
							</button>
						{/if}
					</div>
				</div>
			{:else if session || starting}
				<div class="wp-loader">
					<div class="wp-spin"></div>
					<p class="wp-load-title">
						{session?.state === 'resolving'
							? 'Connecting to torrent peers…'
							: loadingMedia
								? 'Starting video stream…'
								: `Buffering stream (${bytes(session?.downloaded_bytes ?? 0)})…`}
					</p>
					{#if session}
						<p class="wp-muted">
							{session.source_name} · {session.seeders} seeds / {session.peers} peers · {formatSpeed(session.download_bps)}
						</p>
					{/if}
					{#if session?.state === 'error' || playerError}
						<div class="wp-err-inline">
							<Icon name="alert" size={16} />
							<span>{session?.error || playerError}</span>
						</div>
						<button type="button" class="wp-cta wp-cta-glass" onclick={() => (drawer = 'sources')}>
							Choose Another Source
						</button>
					{/if}
				</div>
			{:else}
				<div class="wp-card wp-landing">
					<h1>{headerTitle}</h1>
					{#if headerSub}<p class="wp-ep-tag">{headerSub}</p>{/if}
					{#if details?.overview}<p class="wp-overview">{details.overview}</p>{/if}

					{#if activeSaved && !activeSaved.completed && activeSaved.position_sec > 10}
						<div class="wp-resume-bar">
							<div class="wp-resume-track"><i style="width: {activeSaved.progress_pct ?? 0}%"></i></div>
							<span>
								Paused at {formatDuration(activeSaved.position_sec)}
								{#if activeSaved.duration_sec > 0}
									/ {formatDuration(activeSaved.duration_sec)} · {formatTimeLeft(activeSaved.position_sec, activeSaved.duration_sec)}
								{/if}
								{#if activeSaved.source_name}
									· Remembered {activeSaved.source_name} {activeSaved.resolution}
								{/if}
							</span>
						</div>
					{/if}

					<div class="wp-card-acts">
						{#if activeSaved && !activeSaved.completed && activeSaved.position_sec > 10 && activeSaved.release_link}
							<button
								type="button"
								class="wp-cta wp-cta-white"
								onclick={() => void resumeFromSaved(activeSaved!, activeSaved!.position_sec)}
							>
								<Icon name="play" size={18} />
								<span>Resume from {formatDuration(activeSaved.position_sec)}</span>
							</button>
							<button
								type="button"
								class="wp-cta wp-cta-glass"
								onclick={() => void resumeFromSaved(activeSaved!, 0)}
							>
								<Icon name="replay" size={16} />
								<span>Start Over</span>
							</button>
						{:else}
							{@const best = pickBestCandidate()}
							<button
								type="button"
								class="wp-cta wp-cta-white"
								disabled={searching || !best}
								onclick={() => best && void startCandidate(best, 'browser', 0, false)}
							>
								<Icon name="play" size={18} />
								<span>{searching ? 'Finding Sources…' : best ? `Play (${best.resolution || best.source_name})` : 'No Releases Yet'}</span>
							</button>
						{/if}

						<button type="button" class="wp-cta wp-cta-glass" onclick={() => (drawer = 'sources')}>
							<Icon name="source" size={16} />
							<span>Sources ({candidates.length})</span>
						</button>

						{#if kind === 'show'}
							<button type="button" class="wp-cta wp-cta-glass" onclick={() => (drawer = 'episodes')}>
								<Icon name="tv" size={16} />
								<span>Episodes</span>
							</button>
						{/if}
					</div>
				</div>
			{/if}
		</div>
	{/if}

	<!-- Next Episode Countdown Overlay (Shows) -->
	{#if nearEpisodeEnd && nextEpisodeTarget}
		<div class="wp-next-card">
			<div class="wp-next-meta">
				<span class="wp-next-kicker">Up Next in {nextCountdown}s</span>
				<b>S{String(nextEpisodeTarget.season).padStart(2, '0')} E{String(nextEpisodeTarget.episode).padStart(2, '0')} {nextEpisodeTarget.name || ''}</b>
			</div>
			<div class="wp-next-acts">
				<button
					type="button"
					class="wp-cta wp-cta-white wp-cta-sm"
					onclick={() => nextEpisodeTarget && void switchEpisode(nextEpisodeTarget.season, nextEpisodeTarget.episode, true)}
				>
					<Icon name="play" size={14} />
					<span>Next Episode</span>
				</button>
				<button
					type="button"
					class="wp-cta wp-cta-glass wp-cta-sm"
					onclick={() => {
						nextCanceled = true;
						stopNextCountdown();
					}}
				>
					<span>Dismiss</span>
				</button>
			</div>
		</div>
	{/if}

	<!-- Top Netflix HUD Gradient Bar -->
	<header class="wp-top-bar" class:visible={hudVisible || drawer !== 'none' || pipelineMode === 'none'}>
		<div class="wp-top-left">
			<button type="button" class="wp-icon-btn" aria-label="Back to title" onclick={leavePlayer} title="Back">
				<Icon name="back" size={22} />
			</button>
			<div class="wp-title-stack">
				<span class="wp-main-title">{headerTitle}</span>
				{#if headerSub}<span class="wp-sub-title">{headerSub}</span>{/if}
			</div>
		</div>

		<div class="wp-top-right">
			{#if session}
				<span class="wp-telemetry-pill" title={session.release_title}>
					{activeCandidate?.resolution || session.source_name} · {session.seeders}s/{session.peers}p · {formatSpeed(session.download_bps)} · {session.progress_pct.toFixed(0)}%
				</span>
			{/if}
			<a class="wp-hud-pill" href={to('/watch/history')} title="Watch history">
				<Icon name="clock" size={15} />
				<span>History</span>
			</a>
			{#if mpvAvailable && session && session.files.length > 0}
				<button type="button" class="wp-hud-pill" onclick={() => void triggerMPV(session!.id, currentPos)} title="Open current stream and timestamp in mpv">
					<Icon name="play" size={15} />
					<span>{session.mpv_running ? 'mpv Active' : 'Open in mpv'}</span>
				</button>
			{/if}
		</div>
	</header>

	<!-- Bottom Netflix HUD Control Bar -->
	<footer class="wp-bottom-bar" class:visible={hudVisible || drawer !== 'none'} class:hidden={pipelineMode === 'none'}>
		<!-- Scrubber Track -->
		<div
			class="wp-scrubber-row"
			onmousemove={(e) => {
				if (!totalDuration) return;
				const rect = e.currentTarget.getBoundingClientRect();
				const pct = Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width));
				hoverPct = pct * 100;
				hoverTime = pct * totalDuration;
			}}
			onmouseleave={() => (hoverTime = null)}
		>
			{#if hoverTime !== null}
				<span class="wp-hover-time" style="left: {hoverPct}%">{formatDuration(hoverTime)}</span>
			{/if}
			<div class="wp-track-bg">
				<div class="wp-track-buf" style="width: {bufferedPct}%"></div>
				<div class="wp-track-fill" style="width: {scrubbedPct}%"></div>
			</div>
			<input
				type="range"
				class="wp-range"
				min="0"
				max={Math.max(1, Math.floor(totalDuration))}
				step="1"
				aria-label="Seek timeline"
				value={Math.floor(currentPos)}
				oninput={(e) => {
					isScrubbing = true;
					scrubValue = Number(e.currentTarget.value);
					wakeHud();
				}}
				onchange={(e) => commitSeek(Number(e.currentTarget.value))}
			/>
		</div>

		<div class="wp-controls-row">
			<div class="wp-ctrl-left">
				<button type="button" class="wp-icon-btn" onclick={togglePlayPause} aria-label={paused ? 'Play' : 'Pause'} title={paused ? 'Play (Space)' : 'Pause (Space)'}>
					<Icon name={paused ? 'play' : 'pause'} size={24} />
				</button>
				<button type="button" class="wp-icon-btn" onclick={() => seekBy(-10)} aria-label="Rewind 10 seconds" title="Back 10s (Left/J)">
					<Icon name="replay" size={20} />
				</button>
				<button type="button" class="wp-icon-btn" onclick={() => seekBy(10)} aria-label="Forward 10 seconds" title="Forward 10s (Right/L)">
					<Icon name="refresh" size={20} />
				</button>

				<div class="wp-vol-group">
					<button type="button" class="wp-icon-btn" onclick={toggleMute} aria-label={muted ? 'Unmute' : 'Mute'} title="Mute (M)">
						<Icon name={muted || volume === 0 ? 'soundOff' : 'sound'} size={20} />
					</button>
					<input
						type="range"
						class="wp-vol-slider"
						min="0"
						max="1"
						step="0.05"
						aria-label="Volume"
						value={muted ? 0 : volume}
						oninput={(e) => setVolumeLevel(Number(e.currentTarget.value))}
					/>
				</div>

				<span class="wp-clock">
					{formatDuration(currentPos)}
					{#if totalDuration > 0}
						<span class="wp-clock-dim"> / {formatDuration(totalDuration)}</span>
						{#if remainingText}<span class="wp-clock-rem"> · {remainingText}</span>{/if}
					{/if}
				</span>
			</div>

			<div class="wp-ctrl-right">
				<button
					type="button"
					class="wp-hud-pill"
					class:active={drawer === 'sources'}
					onclick={() => (drawer = drawer === 'sources' ? 'none' : 'sources')}
					title="Sources & Quality (S)"
				>
					<Icon name="source" size={16} />
					<span>Sources{activeCandidate?.resolution ? ` · ${activeCandidate.resolution}` : ''}</span>
				</button>

				<button
					type="button"
					class="wp-hud-pill"
					class:active={drawer === 'audioSubs'}
					onclick={() => (drawer = drawer === 'audioSubs' ? 'none' : 'audioSubs')}
					title="Audio & Subtitles (C)"
				>
					<Icon name="subtitle" size={16} />
					<span>Audio & Subs{subMode !== 'off' ? ` · ${subLang?.toUpperCase() || 'On'}` : ''}</span>
				</button>

				{#if kind === 'show'}
					<button
						type="button"
						class="wp-hud-pill"
						class:active={drawer === 'episodes'}
						onclick={() => (drawer = drawer === 'episodes' ? 'none' : 'episodes')}
						title="Episodes (E)"
					>
						<Icon name="tv" size={16} />
						<span>Episodes</span>
					</button>
				{/if}

				<button
					type="button"
					class="wp-icon-btn"
					onclick={() => void toggleFullscreen()}
					aria-label={isFullscreen ? 'Exit fullscreen' : 'Fullscreen'}
					title="Fullscreen (F)"
				>
					<Icon name="expand" size={20} />
				</button>
			</div>
		</div>
	</footer>

	<!-- Slide-over Drawers (Sources, Audio & Subtitles, Episodes) -->
	{#if drawer !== 'none'}
		<button type="button" class="wp-drawer-scrim" aria-label="Close panel" onclick={() => (drawer = 'none')}></button>
		<aside class="wp-drawer" aria-label={drawer} data-testid="watch-drawer">
			<header class="wp-drawer-head">
				<h2>
					{drawer === 'sources'
						? 'Sources & Releases'
						: drawer === 'audioSubs'
							? 'Audio, Video & Subtitles'
							: 'Episodes'}
				</h2>
				<button type="button" class="wp-icon-btn" aria-label="Close" onclick={() => (drawer = 'none')}>
					<Icon name="x" size={20} />
				</button>
			</header>

			<div class="wp-drawer-body">
				{#if drawer === 'sources'}
					<!-- Source Switcher & Release Picker -->
					<form class="wp-search-row" onsubmit={(e) => { e.preventDefault(); void runSearch(); }}>
						<input type="search" bind:value={queryText} placeholder="Search torrent releases…" />
						<button type="submit" class="wp-cta wp-cta-white wp-cta-sm" disabled={searching}>
							<Icon name="search" size={15} />
							<span>{searching ? '…' : 'Search'}</span>
						</button>
					</form>

					<div class="wp-chips">
						<button
							type="button"
							class="wp-chip"
							class:on={selectedSourceIds.length === 0}
							onclick={() => {
								selectedSourceIds = [];
								void runSearch();
							}}
						>
							All Sources ({availableSources.length})
						</button>
						{#each availableSources as src (src.id)}
							{@const chk = sourceChecks.find((c) => c.id === src.id)}
							<button
								type="button"
								class="wp-chip"
								class:on={selectedSourceIds.includes(src.id)}
								onclick={() => {
									selectedSourceIds = selectedSourceIds.includes(src.id)
										? selectedSourceIds.filter((x) => x !== src.id)
										: [...selectedSourceIds, src.id];
									void runSearch();
								}}
								title={chk?.error || src.name}
							>
								{src.name}{chk?.answered ? ` (${chk.count})` : chk?.error ? ' (!)' : ''}
							</button>
						{/each}
					</div>

					<div class="wp-filter-strip">
						<div class="wp-chips">
							{#each [
								{ v: 'all', l: 'All' },
								{ v: '2160p', l: '4K' },
								{ v: '1080p', l: '1080p' },
								{ v: '720p', l: '720p' },
								{ v: 'sd', l: 'SD' }
							] as r (r.v)}
								<button
									type="button"
									class="wp-chip"
									class:on={resFilter === r.v}
									onclick={() => (resFilter = r.v as typeof resFilter)}
								>
									{r.l}
								</button>
							{/each}
						</div>

						<div class="wp-inline-opts">
							<label class="wp-inline-check">
								<input type="checkbox" bind:checked={onlySeeded} />
								<span>Seeded</span>
							</label>
							<select bind:value={sortBy} aria-label="Sort releases">
								<option value="seeders">Most Seeders</option>
								<option value="quality">Highest Quality</option>
								<option value="size_asc">Smallest Size</option>
								<option value="size_desc">Largest Size</option>
							</select>
						</div>
					</div>

					{#if session && session.files.length > 1}
						<div class="wp-section">
							<h3>File Inside Torrent</h3>
							<select
								class="wp-select-full"
								value={session.selected_file}
								onchange={(e) => void selectPackFile(Number(e.currentTarget.value))}
							>
								{#each session.files as f (f.index)}
									<option value={f.index}>{f.name} ({bytes(f.size)})</option>
								{/each}
							</select>
						</div>
					{/if}

					{#if session}
						<div class="wp-session-actions">
							<button
								type="button"
								class="wp-cta wp-cta-glass wp-cta-sm"
								onclick={() => void setStreamKeep(session!.id, !session!.keep_file).then((s) => (session = s))}
							>
								<Icon name="download" size={14} />
								<span>{session.keep_file ? 'Keeping File on Stop' : 'Keep File'}</span>
							</button>
							<button
								type="button"
								class="wp-cta wp-cta-glass wp-cta-sm"
								onclick={async () => {
									const u = `${window.location.origin}${to(session!.media_url)}`;
									await navigator.clipboard.writeText(u).catch(() => {});
									toasts.push('Stream URL copied', 'ok');
								}}
							>
								<Icon name="copy" size={14} />
								<span>Copy URL</span>
							</button>
						</div>
					{/if}

					{#if searchError}
						<div class="wp-err-inline"><Icon name="alert" size={15} /><span>{searchError}</span></div>
					{/if}

					<div class="wp-cand-list">
						{#if searching && candidates.length === 0}
							<p class="wp-muted">Searching enabled sources…</p>
						{:else if filteredCandidates.length === 0}
							<p class="wp-muted">No matching releases found.</p>
							{#if candidates.length > 0 && (onlySeeded || resFilter !== 'all')}
								<button
									type="button"
									class="wp-cta wp-cta-glass wp-cta-sm"
									onclick={() => {
										onlySeeded = false;
										resFilter = 'all';
									}}
								>
									Show all {candidates.length} results
								</button>
							{/if}
						{:else}
							{#each filteredCandidates as c, i (candKey(c) + ':' + i)}
								{@const isCur = activeCandidate && candKey(activeCandidate) === candKey(c)}
								<div class="wp-cand" class:current={isCur}>
									<div class="wp-cand-info">
										<div class="wp-cand-name">{c.title}</div>
										<div class="wp-cand-meta">
											<span class="tag src">{c.source_name}</span>
											{#if c.resolution}<span class="tag res">{c.resolution}</span>{/if}
											{#if c.hdr}<span class="tag hdr">{c.hdr}</span>{/if}
											{#if c.codec}<span class="tag">{c.codec}</span>{/if}
											{#if c.audio}<span class="tag">{c.audio}</span>{/if}
											<span>{bytes(c.size)}</span>
											<span class:zero={c.seeders <= 0}>{c.seeders}s · {c.peers}p</span>
										</div>
									</div>
									<div class="wp-cand-acts">
										<button
											type="button"
											class="wp-cta wp-cta-white wp-cta-sm"
											onclick={() => {
												drawer = 'none';
												void startCandidate(c, 'browser', currentPos > 5 ? currentPos : 0, true);
											}}
										>
											{isCur ? 'Replay' : 'Play'}
										</button>
										{#if mpvAvailable}
											<button
												type="button"
												class="wp-cta wp-cta-glass wp-cta-sm"
												onclick={() => {
													drawer = 'none';
													void startCandidate(c, 'mpv', currentPos > 5 ? currentPos : 0, true);
												}}
											>
												mpv
											</button>
										{/if}
									</div>
								</div>
							{/each}
						{/if}
					</div>
				{:else if drawer === 'audioSubs'}
					<!-- Playback Quality, Speed & Audio -->
					<div class="wp-section">
						<h3>Stream Quality & Fit</h3>
						<div class="wp-grid-2">
							<label class="wp-label">
								<span>Quality</span>
								<select value={browserQuality} onchange={(e) => changeQuality(e.currentTarget.value as typeof browserQuality)}>
									<option value="original">Original Source</option>
									<option value="1080p">1080p HD</option>
									<option value="720p">720p</option>
									<option value="480p">480p</option>
								</select>
							</label>
							<label class="wp-label">
								<span>Speed</span>
								<select value={playbackRate} onchange={(e) => changeSpeed(Number(e.currentTarget.value))}>
									<option value={0.5}>0.5x</option>
									<option value={0.75}>0.75x</option>
									<option value={1}>1x (Normal)</option>
									<option value={1.25}>1.25x</option>
									<option value={1.5}>1.5x</option>
									<option value={2}>2x</option>
								</select>
							</label>
						</div>

						<div class="wp-chips" style="margin-top: 8px;">
							<button type="button" class="wp-chip" class:on={fitMode === 'contain'} onclick={() => (fitMode = 'contain')}>Fit Screen</button>
							<button type="button" class="wp-chip" class:on={fitMode === 'cover'} onclick={() => (fitMode = 'cover')}>Fill / Crop</button>
						</div>
					</div>

					{#if session?.probe?.audio_tracks.length}
						<div class="wp-section">
							<h3>Audio Track</h3>
							<div class="wp-stack">
								{#each session.probe.audio_tracks as a (a.index)}
									<button
										type="button"
										class="wp-list-pick"
										class:on={audioIdx === a.index}
										onclick={() => changeAudio(a.index)}
									>
										<span>{formatAudioLabel(a.index, a.language, a.title, a.codec, a.channels)}</span>
										{#if audioIdx === a.index}<Icon name="check" size={16} />{/if}
									</button>
								{/each}
							</div>
						</div>
					{/if}

					<!-- Subtitles: Off, Embedded, Online Search, Upload, Sync & Size -->
					<div class="wp-section">
						<div class="wp-sec-row">
							<h3>Subtitles</h3>
							{#if subMode !== 'off'}
								<button type="button" class="wp-link-small" onclick={turnOffSubtitles}>Turn Off</button>
							{/if}
						</div>

						{#if subError}
							<div class="wp-err-inline"><Icon name="alert" size={15} /><span>{subError}</span></div>
						{/if}

						<!-- Timing Sync & Caption Size -->
						<div class="wp-sub-tools">
							<div class="wp-sync-box">
								<span>Timing Sync: <b>{subDelayMS >= 0 ? '+' : ''}{(subDelayMS / 1000).toFixed(2)}s</b></span>
								<div class="wp-sync-btns">
									<button type="button" class="wp-chip" onclick={() => adjustSubDelay(-250)}>-0.25s</button>
									<button type="button" class="wp-chip" onclick={() => { subDelayMS = 0; void persistProgress(); }}>0.0s</button>
									<button type="button" class="wp-chip" onclick={() => adjustSubDelay(250)}>+0.25s</button>
								</div>
							</div>
							<div class="wp-sync-box">
								<span>Caption Size</span>
								<div class="wp-sync-btns">
									{#each ['sm', 'md', 'lg', 'xl'] as sz (sz)}
										<button
											type="button"
											class="wp-chip"
											class:on={subSize === sz}
											onclick={() => {
												subSize = sz as typeof subSize;
												void persistProgress();
											}}
										>
											{sz.toUpperCase()}
										</button>
									{/each}
								</div>
							</div>
						</div>

						<!-- Upload Custom Subtitle File -->
						<div class="wp-upload-row">
							<input
								bind:this={subFileInput}
								type="file"
								accept=".srt,.vtt,.ass,.ssa,.sub,.zip"
								class="sr-only"
								onchange={handleSubtitleFileUpload}
							/>
							<button type="button" class="wp-cta wp-cta-glass wp-cta-sm" onclick={() => subFileInput?.click()} disabled={loadingSub}>
								<Icon name="upload" size={15} />
								<span>Upload .srt / .vtt / .ass</span>
							</button>
							{#if subMode !== 'off'}
								<span class="wp-active-sub">{subLabel}</span>
							{/if}
						</div>

						<!-- Embedded Video Tracks -->
						{#if session?.probe?.subtitle_tracks.length}
							<h4>Embedded Tracks</h4>
							<div class="wp-stack">
								{#each session.probe.subtitle_tracks as st (st.index)}
									<button
										type="button"
										class="wp-list-pick"
										class:on={subMode === 'embedded' && subIndex === st.index}
										disabled={loadingSub}
										onclick={() => void loadEmbeddedSubtitle(st.index)}
									>
										<span>{formatSubTrackLabel(st.index, st.language, st.title, st.codec)}</span>
										{#if subMode === 'embedded' && subIndex === st.index}<Icon name="check" size={16} />{/if}
									</button>
								{/each}
							</div>
						{/if}

						<!-- Online Subtitle Sites Search -->
						<h4>Online Subtitle Sites</h4>
						<form class="wp-search-row" onsubmit={(e) => { e.preventDefault(); void searchSubtitlesOnline(); }}>
							<input type="text" bind:value={siteSubLangsInput} placeholder="Languages (e.g. en, tr)" aria-label="Subtitle languages" />
							<button type="submit" class="wp-cta wp-cta-glass wp-cta-sm" disabled={searchingSubs}>
								<Icon name="search" size={14} />
								<span>{searchingSubs ? 'Searching…' : 'Find Subs'}</span>
							</button>
						</form>

						<div class="wp-stack" style="margin-top: 8px;">
							{#if searchingSubs}
								<p class="wp-muted">Searching subtitle sites…</p>
							{:else if siteSubCandidates.length === 0}
								<p class="wp-muted">No online subtitles listed yet.</p>
							{:else}
								{#each siteSubCandidates as c (c.id)}
									<button
										type="button"
										class="wp-list-pick"
										class:on={subMode === 'site' && subSiteId === c.id}
										disabled={loadingSub}
										onclick={() => void loadSiteSubtitle(c)}
									>
										<span class="wp-sub-cand">
											<b>{c.language.toUpperCase()}</b>
											<span>{c.release_names?.[0] || c.site_name}</span>
											<small>{c.site_name}{c.hearing_impaired ? ' · HI' : ''}{c.downloads ? ` · ${c.downloads} dl` : ''}</small>
										</span>
										{#if subMode === 'site' && subSiteId === c.id}<Icon name="check" size={16} />{/if}
									</button>
								{/each}
							{/if}
						</div>
					</div>
				{:else if drawer === 'episodes' && kind === 'show'}
					<!-- Season & Episode Selector with Progress -->
					<div class="wp-section">
						<select
							class="wp-select-full"
							aria-label="Season"
							value={season}
							onchange={(e) => {
								const sNum = Number(e.currentTarget.value);
								season = sNum;
								void loadSeasonEpisodes(sNum);
							}}
						>
							{#if regularSeasons.length > 0}
								{#each regularSeasons as s (s.number)}
									<option value={s.number}>{s.name || `Season ${s.number}`}</option>
								{/each}
							{:else}
								<option value={season}>Season {season}</option>
							{/if}
						</select>
					</div>

					<div class="wp-ep-list">
						{#if loadingEpisodes && curSeasonEps.length === 0}
							<p class="wp-muted">Loading episodes…</p>
						{:else}
							{#each curSeasonEps as ep (ep.number)}
								{@const savedEp = episodeProgressMap[`${season}:${ep.number}`]}
								<button
									type="button"
									class="wp-ep-card"
									class:current={ep.number === episode}
									onclick={() => void switchEpisode(season, ep.number, true)}
								>
									<div class="wp-ep-thumb">
										{#if ep.still || details?.backdrop}
											<img src={sized(ep.still || details?.backdrop, 'backdrop', 300)} alt="" loading="lazy" />
										{/if}
										{#if savedEp && (savedEp.progress_pct ?? 0) > 0}
											<div class="wp-ep-prog"><i style="width: {savedEp.progress_pct}%"></i></div>
										{/if}
									</div>
									<div class="wp-ep-meta">
										<b>{ep.number}. {ep.name || `Episode ${ep.number}`}</b>
										{#if savedEp}
											<small class="wp-ep-saved">
												{savedEp.completed ? 'Watched' : `${formatDuration(savedEp.position_sec)} watched`}
											</small>
										{/if}
										{#if ep.overview}<p>{ep.overview}</p>{/if}
									</div>
								</button>
							{/each}
						{/if}
					</div>
				{/if}
			</div>
		</aside>
	{/if}
</div>

<style>
	.wp-stage {
		position: fixed; inset: 0; width: 100vw; height: 100dvh; overflow: hidden;
		background: #000; color: #fff; font-family: var(--font-ui); user-select: none;
	}
	.wp-stage.hide-cursor { cursor: none; }

	.wp-backdrop { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; filter: blur(18px) brightness(0.4); transform: scale(1.06); }
	.wp-backdrop-scrim { position: absolute; inset: 0; background: radial-gradient(circle at center, rgb(0 0 0 / 35%), rgb(0 0 0 / 88%)); }

	.wp-video { position: relative; width: 100%; height: 100%; object-fit: contain; background: transparent; cursor: pointer; }
	.wp-video.cover { object-fit: cover; }

	/* Netflix Custom Captions */
	.wp-captions {
		position: absolute; left: 50%; bottom: 48px; transform: translateX(-50%); z-index: 6;
		display: flex; flex-direction: column; align-items: center; gap: 6px;
		max-width: min(82vw, 980px); text-align: center; pointer-events: none;
		transition: bottom 180ms ease;
	}
	.wp-captions.hud-up { bottom: 118px; }
	.wp-cap-cue {
		padding: 4px 14px; border-radius: 6px; background: rgb(0 0 0 / 74%); color: #fff;
		text-shadow: 0 2px 6px rgb(0 0 0 / 90%); line-height: 1.35; font-weight: 600;
	}
	.wp-captions.size-sm .wp-cap-cue { font-size: clamp(14px, 1.5vw, 18px); }
	.wp-captions.size-md .wp-cap-cue { font-size: clamp(18px, 2.2vw, 26px); }
	.wp-captions.size-lg .wp-cap-cue { font-size: clamp(22px, 2.8vw, 34px); }
	.wp-captions.size-xl .wp-cap-cue { font-size: clamp(28px, 3.4vw, 42px); }

	.wp-ripple {
		position: absolute; top: 50%; left: 50%; transform: translate(-50%, -50%); z-index: 8;
		padding: 12px 22px; border-radius: 999px; background: rgb(15 15 18 / 82%); border: 1px solid rgb(255 255 255 / 20%);
		font: 700 18px/1 var(--font-ui); pointer-events: none; backdrop-filter: blur(10px);
	}

	.wp-center-panel {
		position: absolute; inset: 0; z-index: 5; display: grid; place-items: center; padding: 24px; pointer-events: none;
	}
	.wp-loader, .wp-card { pointer-events: auto; }
	.wp-loader { display: flex; flex-direction: column; align-items: center; gap: 12px; text-align: center; max-width: 520px; }
	.wp-load-title { font: 600 17px/1.3 var(--font-ui); }
	.wp-spin { width: 42px; height: 42px; border-radius: 50%; border: 3px solid rgb(255 255 255 / 20%); border-top-color: #e50914; animation: wp-spin 0.8s linear infinite; }
	@keyframes wp-spin { to { transform: rotate(360deg); } }

	.wp-card {
		width: min(680px, 100%); padding: 28px 32px; border-radius: 20px;
		background: rgb(16 16 20 / 78%); border: 1px solid rgb(255 255 255 / 14%); backdrop-filter: blur(20px);
		display: flex; flex-direction: column; gap: 14px;
	}
	.wp-landing h1 { margin: 0; font: 700 clamp(26px, 3.6vw, 40px)/1.12 var(--font-ui); letter-spacing: -0.02em; }
	.wp-ep-tag { margin: 0; color: rgb(255 255 255 / 80%); font: 600 15px/1.3 var(--font-ui); }
	.wp-overview { margin: 0; color: rgb(255 255 255 / 78%); font: 400 14.5px/1.5 var(--font-ui); display: -webkit-box; -webkit-line-clamp: 3; line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; }
	.wp-muted { margin: 0; color: rgb(255 255 255 / 66%); font: 500 13.5px/1.4 var(--font-ui); }

	.wp-resume-bar { display: flex; flex-direction: column; gap: 8px; padding: 10px 12px; border-radius: 10px; background: rgb(255 255 255 / 8%); font: 500 13px/1.3 var(--font-ui); }
	.wp-resume-track { height: 4px; border-radius: 999px; background: rgb(255 255 255 / 20%); overflow: hidden; }
	.wp-resume-track i { display: block; height: 100%; background: #e50914; }

	.wp-card-acts { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 4px; }
	.wp-cta {
		display: inline-flex; align-items: center; justify-content: center; gap: 8px; height: 44px; padding: 0 20px;
		border: 0; border-radius: 999px; font: 700 14.5px/1 var(--font-ui); cursor: pointer;
	}
	.wp-cta-sm { height: 34px; padding: 0 14px; font-size: 13px; border-radius: 8px; }
	.wp-cta-white { background: #fff; color: #111; }
	.wp-cta-white:hover:not(:disabled) { background: rgb(255 255 255 / 88%); }
	.wp-cta-glass { background: rgb(255 255 255 / 16%); color: #fff; border: 1px solid rgb(255 255 255 / 22%); }
	.wp-cta-glass:hover:not(:disabled) { background: rgb(255 255 255 / 26%); }
	.wp-cta:disabled { opacity: 0.55; cursor: not-allowed; }

	/* Top & Bottom Netflix HUD */
	.wp-top-bar, .wp-bottom-bar {
		position: absolute; left: 0; right: 0; z-index: 9;
		opacity: 0; pointer-events: none; transition: opacity 220ms ease;
	}
	.wp-top-bar.visible, .wp-bottom-bar.visible { opacity: 1; pointer-events: auto; }
	.wp-bottom-bar.hidden { opacity: 0 !important; pointer-events: none !important; }

	.wp-top-bar {
		top: 0; display: flex; align-items: center; justify-content: space-between; gap: 16px;
		padding: 18px 24px 36px; background: linear-gradient(to bottom, rgb(0 0 0 / 84%), rgb(0 0 0 / 0%));
	}
	.wp-top-left, .wp-top-right { display: flex; align-items: center; gap: 12px; min-width: 0; }
	.wp-title-stack { display: flex; flex-direction: column; min-width: 0; }
	.wp-main-title { font: 700 17px/1.2 var(--font-ui); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
	.wp-sub-title { font: 500 13px/1.25 var(--font-ui); color: rgb(255 255 255 / 72%); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
	.wp-telemetry-pill {
		padding: 5px 11px; border-radius: 999px; background: rgb(255 255 255 / 12%); color: rgb(255 255 255 / 86%);
		font: 600 12px/1 var(--font-ui); font-variant-numeric: tabular-nums; white-space: nowrap;
	}

	.wp-bottom-bar {
		bottom: 0; display: flex; flex-direction: column; gap: 10px;
		padding: 36px 24px calc(18px + env(safe-area-inset-bottom));
		background: linear-gradient(to top, rgb(0 0 0 / 90%), rgb(0 0 0 / 0%));
	}

	.wp-scrubber-row { position: relative; height: 22px; display: flex; align-items: center; cursor: pointer; }
	.wp-track-bg { position: relative; width: 100%; height: 5px; border-radius: 999px; background: rgb(255 255 255 / 26%); overflow: hidden; transition: height 120ms ease; }
	.wp-scrubber-row:hover .wp-track-bg { height: 8px; }
	.wp-track-buf { position: absolute; left: 0; top: 0; bottom: 0; background: rgb(255 255 255 / 40%); }
	.wp-track-fill { position: absolute; left: 0; top: 0; bottom: 0; background: #e50914; }
	.wp-range { position: absolute; inset: 0; width: 100%; height: 100%; margin: 0; opacity: 0; cursor: pointer; }
	.wp-hover-time {
		position: absolute; bottom: 26px; transform: translateX(-50%); padding: 4px 8px; border-radius: 6px;
		background: rgb(15 15 18 / 92%); font: 700 12px/1 var(--font-ui); font-variant-numeric: tabular-nums; pointer-events: none;
	}

	.wp-controls-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
	.wp-ctrl-left, .wp-ctrl-right { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }

	.wp-icon-btn {
		display: inline-grid; place-items: center; width: 40px; height: 40px; border: 0; border-radius: 999px;
		background: transparent; color: #fff; cursor: pointer; transition: background 140ms ease, transform 140ms ease;
	}
	.wp-icon-btn:hover { background: rgb(255 255 255 / 16%); transform: scale(1.06); }

	.wp-vol-group { display: flex; align-items: center; gap: 4px; }
	.wp-vol-slider { width: 78px; accent-color: #fff; cursor: pointer; }

	.wp-clock { margin-left: 6px; font: 600 13px/1 var(--font-ui); font-variant-numeric: tabular-nums; }
	.wp-clock-dim, .wp-clock-rem { color: rgb(255 255 255 / 66%); }

	.wp-hud-pill {
		display: inline-flex; align-items: center; gap: 6px; height: 34px; padding: 0 12px;
		border: 1px solid rgb(255 255 255 / 18%); border-radius: 999px; background: rgb(255 255 255 / 10%);
		color: #fff; text-decoration: none; font: 600 12.5px/1 var(--font-ui); cursor: pointer; white-space: nowrap;
	}
	.wp-hud-pill:hover, .wp-hud-pill.active { background: rgb(255 255 255 / 24%); border-color: rgb(255 255 255 / 45%); }

	/* Next Episode Countdown Card */
	.wp-next-card {
		position: absolute; right: 24px; bottom: 112px; z-index: 10; max-width: 340px; padding: 16px; border-radius: 14px;
		background: rgb(18 18 22 / 92%); border: 1px solid rgb(255 255 255 / 20%); backdrop-filter: blur(18px);
		display: flex; flex-direction: column; gap: 12px;
	}
	.wp-next-meta { display: flex; flex-direction: column; gap: 4px; }
	.wp-next-kicker { color: #f87171; font: 700 12px/1 var(--font-ui); text-transform: uppercase; letter-spacing: 0.04em; }
	.wp-next-acts { display: flex; gap: 8px; }

	/* Side Drawer */
	.wp-drawer-scrim { position: absolute; inset: 0; z-index: 15; border: 0; background: rgb(0 0 0 / 50%); padding: 0; }
	.wp-drawer {
		position: absolute; top: 0; right: 0; bottom: 0; z-index: 16; width: min(460px, 100vw);
		background: rgb(18 18 22 / 96%); border-left: 1px solid rgb(255 255 255 / 14%); backdrop-filter: blur(22px);
		display: flex; flex-direction: column;
	}
	.wp-drawer-head { display: flex; align-items: center; justify-content: space-between; padding: 16px 18px; border-bottom: 1px solid rgb(255 255 255 / 12%); }
	.wp-drawer-head h2 { margin: 0; font: 700 17px/1.2 var(--font-ui); }
	.wp-drawer-body { flex: 1; overflow: auto; padding: 16px 18px 28px; display: flex; flex-direction: column; gap: 16px; }

	.wp-section { display: flex; flex-direction: column; gap: 8px; }
	.wp-section h3, .wp-section h4 { margin: 4px 0 0; font: 700 13px/1.2 var(--font-ui); color: rgb(255 255 255 / 80%); text-transform: uppercase; letter-spacing: 0.04em; }
	.wp-sec-row { display: flex; align-items: center; justify-content: space-between; }
	.wp-link-small { border: 0; background: none; padding: 0; color: #f87171; font: 600 12.5px/1 var(--font-ui); cursor: pointer; }

	.wp-search-row { display: flex; gap: 8px; }
	.wp-search-row input {
		flex: 1; height: 34px; padding: 0 11px; border: 1px solid rgb(255 255 255 / 20%); border-radius: 8px;
		background: rgb(255 255 255 / 8%); color: #fff; font: 500 13px/1 var(--font-ui);
	}

	.wp-chips { display: flex; flex-wrap: wrap; gap: 6px; }
	.wp-chip {
		padding: 5px 10px; border: 1px solid rgb(255 255 255 / 18%); border-radius: 999px;
		background: transparent; color: rgb(255 255 255 / 78%); font: 600 12px/1 var(--font-ui); cursor: pointer;
	}
	.wp-chip.on { background: #fff; color: #111; border-color: #fff; }

	.wp-filter-strip { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px; }
	.wp-inline-opts { display: flex; align-items: center; gap: 10px; font: 500 12.5px/1 var(--font-ui); }
	.wp-inline-check { display: inline-flex; align-items: center; gap: 5px; cursor: pointer; }
	.wp-inline-opts select, .wp-select-full, .wp-label select {
		height: 34px; padding: 0 10px; border: 1px solid rgb(255 255 255 / 20%); border-radius: 8px;
		background: rgb(28 28 34); color: #fff; font: 500 13px/1 var(--font-ui);
	}
	.wp-select-full { width: 100%; }
	.wp-grid-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
	.wp-label { display: flex; flex-direction: column; gap: 4px; font: 600 12px/1 var(--font-ui); color: rgb(255 255 255 / 72%); }

	.wp-session-actions { display: flex; flex-wrap: wrap; gap: 8px; }

	.wp-cand-list, .wp-stack, .wp-ep-list { display: flex; flex-direction: column; gap: 8px; }
	.wp-cand {
		display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 10px 12px;
		border: 1px solid rgb(255 255 255 / 12%); border-radius: 10px; background: rgb(255 255 255 / 4%);
	}
	.wp-cand.current { border-color: #e50914; background: rgb(229 9 20 / 12%); }
	.wp-cand-info { min-width: 0; flex: 1; display: flex; flex-direction: column; gap: 5px; }
	.wp-cand-name { font: 600 13px/1.3 var(--font-ui); word-break: break-word; }
	.wp-cand-meta { display: flex; flex-wrap: wrap; align-items: center; gap: 5px; font: 500 11.5px/1.2 var(--font-ui); color: rgb(255 255 255 / 70%); }
	.tag { padding: 2px 6px; border-radius: 5px; background: rgb(255 255 255 / 12%); color: #fff; font-weight: 600; }
	.tag.src { background: rgb(56 189 248 / 22%); }
	.tag.res { background: rgb(34 197 94 / 22%); }
	.tag.hdr { background: rgb(245 158 11 / 22%); }
	.wp-cand-meta .zero { opacity: 0.55; }
	.wp-cand-acts { display: flex; gap: 6px; flex: none; }

	.wp-list-pick {
		display: flex; align-items: center; justify-content: space-between; gap: 10px; width: 100%; padding: 9px 12px;
		border: 1px solid rgb(255 255 255 / 14%); border-radius: 9px; background: rgb(255 255 255 / 5%);
		color: #fff; text-align: left; font: 500 13px/1.3 var(--font-ui); cursor: pointer;
	}
	.wp-list-pick.on { border-color: #e50914; background: rgb(229 9 20 / 15%); }
	.wp-sub-cand { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
	.wp-sub-cand span { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
	.wp-sub-cand small { color: rgb(255 255 255 / 62%); font-size: 11.5px; }

	.wp-sub-tools { display: flex; flex-direction: column; gap: 10px; padding: 10px; border-radius: 10px; background: rgb(255 255 255 / 5%); }
	.wp-sync-box { display: flex; align-items: center; justify-content: space-between; gap: 8px; flex-wrap: wrap; font: 500 12.5px/1.2 var(--font-ui); }
	.wp-sync-btns { display: flex; gap: 5px; }
	.wp-upload-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
	.wp-active-sub { font: 500 12px/1.2 var(--font-ui); color: #4ade80; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 220px; }

	.wp-ep-card {
		display: flex; gap: 12px; padding: 8px; border: 1px solid rgb(255 255 255 / 12%); border-radius: 10px;
		background: rgb(255 255 255 / 4%); color: #fff; text-align: left; cursor: pointer;
	}
	.wp-ep-card.current { border-color: #e50914; background: rgb(229 9 20 / 14%); }
	.wp-ep-thumb { position: relative; width: 116px; aspect-ratio: 16 / 9; flex: none; border-radius: 7px; overflow: hidden; background: rgb(255 255 255 / 10%); }
	.wp-ep-thumb img { width: 100%; height: 100%; object-fit: cover; }
	.wp-ep-prog { position: absolute; left: 0; right: 0; bottom: 0; height: 3px; background: rgb(255 255 255 / 26%); }
	.wp-ep-prog i { display: block; height: 100%; background: #e50914; }
	.wp-ep-meta { min-width: 0; flex: 1; display: flex; flex-direction: column; gap: 3px; }
	.wp-ep-meta b { font: 600 13px/1.25 var(--font-ui); }
	.wp-ep-saved { color: #4ade80; font: 600 11px/1.2 var(--font-ui); }
	.wp-ep-meta p { margin: 0; color: rgb(255 255 255 / 66%); font: 400 12px/1.35 var(--font-ui); display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }

	.wp-err-inline { display: flex; align-items: center; gap: 8px; padding: 8px 10px; border-radius: 8px; background: rgb(239 68 68 / 18%); color: #fca5a5; font: 500 12.5px/1.3 var(--font-ui); }

	@media (max-width: 720px) {
		.wp-telemetry-pill { display: none; }
		.wp-clock-rem { display: none; }
		.wp-vol-slider { display: none; }
	}
</style>
