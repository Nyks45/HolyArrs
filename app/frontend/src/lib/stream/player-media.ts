import type HlsType from 'hls.js';
import { to } from '#lib/core/paths.ts';
import { hlsScript } from '#lib/core/trailer/hls-player.ts';
import type { StreamCandidate, SessionStatus, SubPref, WatchEntry } from './stream-api.ts';

let cachedHlsCtor: Promise<typeof HlsType> | null = null;

export function loadHlsCtor(): Promise<typeof HlsType> {
	cachedHlsCtor ??= (import(/* @vite-ignore */ to(hlsScript)) as Promise<{ default: typeof HlsType }>).then((m) => m.default);
	cachedHlsCtor.catch(() => (cachedHlsCtor = null));
	return cachedHlsCtor;
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
}): Promise<HlsType | null> {
	const Hls = await loadHlsCtor();
	if (opts.isCancelled()) return null;

	if (Hls.isSupported()) {
		const hls = new Hls({
			enableWorker: true,
			maxBufferLength: 30,
			maxMaxBufferLength: 60,
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
