import type HlsType from 'hls.js';
import { to } from '#lib/core/paths.ts';
import { hlsScript } from '#lib/core/trailer/hls-player.ts';
import type { TitleDetails } from '#lib/metadata/index.ts';
import {
	fetchStreamSubtitle,
	searchStreamReleases,
	uploadStreamSubtitle,
	type SessionStatus,
	type StreamCandidate,
	type StreamSearchReq,
	type SubPref,
	type WatchEntry
} from './stream-api.ts';
import { getSkipSegments, postScrobble, type SkipSegment, type SubStylePrefs } from './streaming-config-api.ts';
import { parseWebVTT, type VTTCue } from './vtt.ts';

export interface SeasonEp {
	number: number;
	name?: string;
	overview?: string;
	runtime?: number;
	still?: string;
	air_day?: string;
}

export const DEFAULT_SUB_STYLE: SubStylePrefs = {
	color: '#ffffff',
	bg_color: '#000000',
	bg_opacity: 0.65,
	size: 1.0,
	offset_pct: 6,
	hdr_dimmed: true,
	hdr_brightness: 40,
	strip_sdh: false,
	font_family: 'sans'
};

export interface HlsQualityLevel {
	index: number;
	height: number;
	bitrate: number;
	label: string;
}

let cachedHlsCtor: Promise<typeof HlsType> | null = null;
const prefetchedKeys = new Map<string, number>();
const PREFETCH_TTL_MS = 90_000;

export function loadHlsCtor(): Promise<typeof HlsType> {
	cachedHlsCtor ??= (import(/* @vite-ignore */ to(hlsScript)) as Promise<{ default: typeof HlsType }>).then((m) => m.default);
	cachedHlsCtor.catch(() => (cachedHlsCtor = null));
	return cachedHlsCtor;
}

export function canSeekInLocalHlsBuffer(video: HTMLVideoElement, seekOffset: number, targetSec: number, bufferedEnd: number): boolean {
	if (targetSec < seekOffset) return false;
	const seekableEnd = video.seekable.length > 0 ? video.seekable.end(video.seekable.length - 1) : 0;
	const dur = Number.isFinite(video.duration) ? video.duration : 0;
	const maxAvail = seekOffset + Math.max(bufferedEnd, dur, seekableEnd);
	return maxAvail > seekOffset + 1 && targetSec <= maxAvail - 0.5;
}

export function matchesQualityFilter(c: StreamCandidate, resFilter: 'all' | '2160p' | '1080p' | '720p' | 'sd'): boolean {
	if (resFilter === 'all') return true;
	const res = (c.quality || c.resolution || '').toLowerCase();
	if (resFilter === '2160p') return res.includes('2160') || res.includes('4k') || res.includes('uhd') || c.resolution_num >= 2160;
	if (resFilter === '1080p') return res.includes('1080') || c.resolution_num === 1080;
	if (resFilter === '720p') return res.includes('720') || c.resolution_num === 720;
	return (c.resolution_num > 0 && c.resolution_num < 720) || res.includes('480') || res.includes('360');
}

function kindTier(c: StreamCandidate): number {
	switch (c.kind) {
		case 'local':
			return 4;
		case 'debrid_cached':
			return 3;
		case 'http':
		case 'hls':
			return c.fast_start !== false ? 2 : 1;
		default:
			return 0;
	}
}

export function sortCandidatesForQuality(
	cands: StreamCandidate[],
	resFilter: 'all' | '2160p' | '1080p' | '720p' | 'sd' = 'all'
): StreamCandidate[] {
	return [...cands].sort((a, b) => {
		const aMatch = matchesQualityFilter(a, resFilter) ? 1 : 0;
		const bMatch = matchesQualityFilter(b, resFilter) ? 1 : 0;
		if (aMatch !== bMatch) return bMatch - aMatch;

		const ta = kindTier(a);
		const tb = kindTier(b);
		if (ta !== tb) return tb - ta;

		if (ta === 1 || ta === 2) {
			const pa = a.probe_ms ?? 0;
			const pb = b.probe_ms ?? 0;
			if (pa > 0 && pb > 0 && Math.abs(pa - pb) > 15) return pa - pb;
			if ((pa > 0) !== (pb > 0)) return pa > 0 ? -1 : 1;
		}
		const sa = a.score ?? 0;
		const sb = b.score ?? 0;
		if (sa !== sb) return sb - sa;
		if (a.seeders !== b.seeders) return (b.seeders ?? 0) - (a.seeders ?? 0);
		return (b.size ?? 0) - (a.size ?? 0);
	});
}

export function countCandidateKinds(cands: StreamCandidate[]): { local: number; instant: number; debrid: number; torrent: number } {
	const counts = { local: 0, instant: 0, debrid: 0, torrent: 0 };
	for (const c of cands) {
		if (c.kind === 'local') {
			counts.local++;
			counts.instant++;
		} else if (c.kind === 'debrid_cached') {
			counts.debrid++;
			counts.instant++;
		} else if (c.kind === 'http' || c.kind === 'hls' || c.instant) {
			counts.instant++;
		} else {
			counts.torrent++;
		}
	}
	return counts;
}

export function mergeCandidateLists(current: StreamCandidate[], incoming: StreamCandidate[]): StreamCandidate[] {
	if (incoming.length === 0) return current;
	if (current.length === 0) return incoming;
	const seen = new Set<string>();
	const merged: StreamCandidate[] = [];
	const push = (c: StreamCandidate) => {
		const k = `${c.provider || c.source_name}:${c.release_name || c.title}:${c.quality || c.resolution}:${c.info_hash || c.link}`;
		if (seen.has(k)) return;
		seen.add(k);
		merged.push(c);
	};
	for (const c of incoming) push(c);
	for (const c of current) push(c);
	return merged;
}

export function prefetchStreamSources(req: StreamSearchReq): void {
	if (!req.tmdb && !req.imdb && !req.query) return;
	const key = `${req.kind}:${req.tmdb ?? 0}:${req.season ?? 0}:${req.episode ?? 0}`;
	const now = Date.now();
	const prev = prefetchedKeys.get(key);
	if (prev && now - prev < PREFETCH_TTL_MS) return;
	prefetchedKeys.set(key, now);
	if (prefetchedKeys.size > 60) {
		const first = prefetchedKeys.keys().next().value;
		if (first) prefetchedKeys.delete(first);
	}
	void searchStreamReleases({ ...req, mode: 'fast' }).catch(() => {
		prefetchedKeys.delete(key);
	});
}

export async function attachHlsMedia(opts: {
	video: HTMLVideoElement;
	srcUrl: string;
	startSec: number;
	playbackRate: number;
	volume: number;
	muted: boolean;
	isCancelled: () => boolean;
	onFatalError: (details: string) => void;
	onStarted: () => void;
	onLevelsLoaded?: (levels: HlsQualityLevel[]) => void;
}): Promise<HlsType | null> {
	const Hls = await loadHlsCtor();
	if (opts.isCancelled()) return null;

	if (Hls.isSupported()) {
		const hls = new Hls({
			enableWorker: true,
			backBufferLength: 180,
			maxBufferLength: 60,
			maxMaxBufferLength: 120,
			startFragPrefetch: true,
			lowLatencyMode: false,
			fragLoadingTimeOut: 30000,
			manifestLoadingTimeOut: 25000,
			renderTextTracksNatively: false
		});
		hls.on(Hls.Events.ERROR, (_ev, data) => {
			if (data.fatal) opts.onFatalError(data.details);
		});
		hls.loadSource(opts.srcUrl);
		hls.attachMedia(opts.video);
		hls.on(Hls.Events.MANIFEST_PARSED, () => {
			if (opts.isCancelled()) return;
			if (opts.onLevelsLoaded && Array.isArray(hls.levels) && hls.levels.length > 1) {
				const byHeight = new Map<number, HlsQualityLevel>();
				hls.levels.forEach((lv, index) => {
					const h = lv.height || 0;
					const label = h > 0 ? `${h}p` : `Level ${index + 1}`;
					const prev = byHeight.get(h);
					if (!prev || (lv.bitrate || 0) > prev.bitrate) {
						byHeight.set(h, { index, height: h, bitrate: lv.bitrate || 0, label });
					}
				});
				opts.onLevelsLoaded([...byHeight.values()].sort((a, b) => b.height - a.height || b.bitrate - a.bitrate));
			}
			if (opts.startSec > 0.5) opts.video.currentTime = opts.startSec;
			opts.video.playbackRate = opts.playbackRate;
			opts.video.volume = opts.volume;
			opts.video.muted = opts.muted;
			void opts.video.play().catch(() => {});
			opts.onStarted();
		});
		return hls;
	}

	if (opts.video.canPlayType('application/vnd.apple.mpegurl')) {
		opts.video.src = opts.srcUrl;
		if (opts.startSec > 0.5) {
			opts.video.addEventListener(
				'loadedmetadata',
				() => {
					opts.video.currentTime = opts.startSec;
				},
				{ once: true }
			);
		}
		opts.video.playbackRate = opts.playbackRate;
		opts.video.volume = opts.volume;
		opts.video.muted = opts.muted;
		await opts.video.play().catch(() => {});
		opts.onStarted();
		return null;
	}

	throw new Error('HLS playback is not supported in this browser');
}

export function buildWatchEntryPayload(args: {
	kind: 'movie' | 'show';
	tmdb: string;
	imdbId: string;
	title: string;
	year: number;
	poster: string;
	backdrop: string;
	season: number;
	episode: number;
	episodeName: string;
	pos: number;
	dur: number;
	completedOverride?: boolean;
	activeCandidate: StreamCandidate | null;
	session: SessionStatus | null;
	saved: WatchEntry | null;
	browserQuality: string;
	audioIdx: number;
	playbackRate: number;
	subPref: SubPref;
}): WatchEntry | null {
	if (!args.tmdb) return null;
	const pct = args.dur > 0 ? Math.min(100, Math.max(0, (args.pos / args.dur) * 100)) : 0;
	const c = args.activeCandidate;
	return {
		kind: args.kind,
		tmdb: args.tmdb,
		imdb: args.imdbId,
		title: args.title,
		year: args.year || args.saved?.year || 0,
		poster: args.poster || args.saved?.poster || '',
		backdrop: args.backdrop || args.saved?.backdrop || '',
		season: args.kind === 'show' ? args.season : 0,
		episode: args.kind === 'show' ? args.episode : 0,
		episode_name: args.kind === 'show' ? args.episodeName : '',
		position_sec: args.pos,
		duration_sec: args.dur,
		progress_pct: pct,
		completed: args.completedOverride ?? (args.dur > 60 && (pct >= 92 || args.dur - args.pos <= 90)),
		source_id: c?.source_id ?? args.session?.source_id ?? args.saved?.source_id ?? 0,
		source_name: c?.provider ?? c?.source_name ?? args.session?.source_name ?? args.saved?.source_name ?? '',
		release_title: c?.release_name ?? c?.title ?? args.session?.release_title ?? args.saved?.release_title ?? '',
		release_link: c?.link ?? args.saved?.release_link ?? '',
		info_hash: c?.info_hash ?? args.saved?.info_hash ?? '',
		resolution: c?.quality ?? c?.resolution ?? args.saved?.resolution ?? '',
		file_index: args.session?.selected_file ?? args.saved?.file_index ?? 0,
		quality: args.browserQuality,
		audio_index: args.audioIdx,
		playback_rate: args.playbackRate,
		subtitle: args.subPref
	};
}

export type PlayerQualityFilter = 'all' | '2160p' | '1080p' | '720p' | 'sd';
export type PlayerHotkeyAction = 'togglePlay' | 'back10' | 'fwd10' | 'mute' | 'fullscreen' | 'sources' | 'episodes' | 'closeDrawer';

export function normQualityChoice(q?: string): PlayerQualityFilter {
	const low = (q || '').toLowerCase();
	if (low === '2160p' || low === '4k') return '2160p';
	if (low === '1080p') return '1080p';
	if (low === '720p') return '720p';
	if (low === '480p' || low === 'sd') return 'sd';
	return 'all';
}

export function buildResumeCandidate(sv: WatchEntry): StreamCandidate {
	return {
		source_id: sv.source_id ?? 0,
		source_name: sv.source_name || 'Saved Source',
		title: sv.release_title || sv.title,
		link: sv.release_link ?? '',
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
}

export async function resolveEmbeddedSub(
	sessionId: string,
	token: string,
	idx: number,
	track?: { language?: string; title?: string }
): Promise<{ cues: VTTCue[]; lang: string; label: string }> {
	const res = await fetch(to(`/api/v1/stream/${encodeURIComponent(sessionId)}/sub/${encodeURIComponent(token)}/${idx}`), { credentials: 'same-origin' });
	if (!res.ok) throw new Error(await res.text());
	const cues = parseWebVTT(await res.text());
	const lang = track?.language ?? '';
	const label = [track?.language?.toUpperCase(), track?.title].filter(Boolean).join(' · ') || `Track ${idx + 1}`;
	return { cues, lang, label };
}

export async function resolveSiteSub(id: string, lang: string, label: string, sessionId: string): Promise<{ cues: VTTCue[]; lang: string; label: string; subFile: string }> {
	const res = await fetchStreamSubtitle(id, sessionId);
	const resolvedLang = res.language || lang;
	return { cues: parseWebVTT(res.vtt), lang: resolvedLang, label: label || resolvedLang.toUpperCase(), subFile: res.sub_file ?? '' };
}

export async function resolveUploadedSub(file: File, sessionId: string): Promise<{ cues: VTTCue[]; label: string; subFile: string }> {
	const res = await uploadStreamSubtitle(file.name, await file.text(), '', sessionId);
	return { cues: parseWebVTT(res.vtt), label: file.name, subFile: res.sub_file ?? '' };
}

export function matchPlayerHotkey(e: KeyboardEvent, isShow: boolean): PlayerHotkeyAction | null {
	const target = e.target as HTMLElement | null;
	if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT')) return null;
	switch (e.key.toLowerCase()) {
		case ' ':
		case 'k':
			return 'togglePlay';
		case 'arrowleft':
		case 'j':
			return 'back10';
		case 'arrowright':
		case 'l':
			return 'fwd10';
		case 'm':
			return 'mute';
		case 'f':
			return 'fullscreen';
		case 's':
			return 'sources';
		case 'e':
			return isShow ? 'episodes' : null;
		case 'escape':
			return 'closeDrawer';
		default:
			return null;
	}
}

export function clearPlayerTimers(intervals: Array<ReturnType<typeof setInterval> | null>, timeouts: Array<ReturnType<typeof setTimeout> | null>): void {
	for (const id of intervals) if (id) clearInterval(id);
	for (const id of timeouts) if (id) clearTimeout(id);
}

export function candidateKey(c: StreamCandidate): string {
	return c.id || `${c.source_id}:${c.link}`;
}

export function extractRegularSeasons(details: TitleDetails | null): { number: number; name?: string }[] {
	const raw = details && 'seasons' in details && Array.isArray(details.seasons) ? (details.seasons as { number: number; name?: string }[]) : [];
	const nonZero = raw.filter((s) => s.number > 0);
	return nonZero.length > 0 ? nonZero : raw;
}

export function buildEpisodeProgressMap(historyEpisodes: WatchEntry[]): Record<string, WatchEntry> {
	const map: Record<string, WatchEntry> = {};
	for (const h of historyEpisodes) map[`${h.season ?? 0}:${h.episode ?? 0}`] = h;
	return map;
}

export function findNextEpisodeTarget(
	kind: 'movie' | 'show',
	season: number,
	episode: number,
	curSeasonEps: SeasonEp[],
	regularSeasons: { number: number; name?: string }[]
): { season: number; episode: number } | null {
	if (kind !== 'show') return null;
	const idx = curSeasonEps.findIndex((e) => e.number === episode);
	if (idx >= 0 && idx + 1 < curSeasonEps.length) return { season, episode: curSeasonEps[idx + 1].number };
	const sIdx = regularSeasons.findIndex((s) => s.number === season);
	if (sIdx >= 0 && sIdx + 1 < regularSeasons.length) return { season: regularSeasons[sIdx + 1].number, episode: 1 };
	return null;
}

export function buildSearchReq(args: {
	query: string;
	kind: 'movie' | 'show';
	year: number;
	season: number;
	episode: number;
	imdb: string;
	tmdb: number;
	details: TitleDetails | null;
	preferredQuality?: string;
}): StreamSearchReq {
	return {
		query: args.query,
		kind: args.kind,
		year: args.year,
		season: args.kind === 'show' ? args.season : 0,
		episode: args.kind === 'show' ? args.episode : 0,
		imdb: args.imdb,
		tmdb: args.tmdb,
		original_title: args.details?.original_title,
		genres: args.details?.genres,
		original_language: args.details?.original_language,
		origin_country: args.details?.origin_country,
		preferred_quality: args.preferredQuality || ''
	};
}

export async function postWatchScrobble(args: {
	action: 'start' | 'pause' | 'stop';
	kind: 'movie' | 'show';
	tmdb: string;
	imdbId: string;
	season: number;
	episode: number;
	currentPos: number;
	totalDuration: number;
}): Promise<void> {
	const idNum = Number(args.tmdb);
	if (!idNum) return;
	const pct = args.totalDuration > 0 ? Math.min(100, Math.max(0, (args.currentPos / args.totalDuration) * 100)) : 0;
	try {
		await postScrobble({
			action: args.action, kind: args.kind, tmdb: idNum, imdb: args.imdbId,
			season: args.kind === 'show' ? args.season : 0, episode: args.kind === 'show' ? args.episode : 0, progress: pct
		});
	} catch { /* Non-fatal telemetry. */ }
}

export async function fetchTitleSkips(args: {
	kind: 'movie' | 'show';
	imdb: string;
	tmdb: string;
	season: number;
	episode: number;
	title: string;
	genres?: string[];
	sessionId?: string;
}): Promise<SkipSegment[] | null> {
	const animeHint = (args.genres ?? []).some((g) => g.toLowerCase().includes('anim'));
	try {
		const res = await getSkipSegments({
			kind: args.kind, imdb: args.imdb, tmdb: args.tmdb,
			season: args.kind === 'show' ? args.season : 0, episode: args.kind === 'show' ? args.episode : 0,
			title: args.title, anime: animeHint, session_id: args.sessionId || ''
		});
		return res.segments ?? [];
	} catch { return null; }
}

export function mergeWatchHistory(list: WatchEntry[], saved: WatchEntry): WatchEntry[] {
	const idx = list.findIndex((h) => h.key === saved.key);
	return idx >= 0 ? list.map((h, i) => (i === idx ? saved : h)) : [saved, ...list];
}

export function parseSavedPlayerPrefs(sv: WatchEntry) {
	const q = sv.quality;
	const browserQuality: 'original' | '1080p' | '720p' | '480p' | null =
		q === '1080p' || q === '720p' || q === '480p' || q === 'original' ? q : null;
	const sz = sv.subtitle?.size;
	const subSize: 'sm' | 'md' | 'lg' | 'xl' | null =
		sz === 'sm' || sz === 'md' || sz === 'lg' || sz === 'xl' ? sz : null;
	return {
		browserQuality,
		audioIdx: sv.audio_index ?? 0,
		playbackRate: sv.playback_rate && sv.playback_rate > 0 ? sv.playback_rate : 1,
		subDelayMS: sv.subtitle?.delay_ms ?? 0,
		subSize,
		subLang: sv.subtitle?.language ?? '',
		subLabel: sv.subtitle?.label ?? '',
		subSiteId: sv.subtitle?.site_id ?? '',
		subSiteKey: sv.subtitle?.site_key ?? '',
		subIndex: sv.subtitle?.index ?? 0,
		subMode: (sv.subtitle?.mode === 'embedded' ? 'embedded' : 'off') as 'embedded' | 'off'
	};
}
