import { describe, expect, it } from 'vitest';
import {
	canSeekInLocalHlsBuffer,
	extractRegularSeasons,
	formatSyncDelta,
	isTimeBuffered,
	mergeCandidateLists,
	pickBestSubtitleCandidate,
	resolveTotalDuration,
	selectAutoPlayCandidate,
	shouldAcceptEnded,
	shouldAcceptTimeUpdate,
	shouldFallbackDirectSeekToTranscode,
	sortCandidatesForQuality
} from './player-media.ts';
import type { TitleDetails } from '#lib/metadata/index.ts';
import type { StreamCandidate, StreamSubCandidate } from './stream-api.ts';

function cand(over: Partial<StreamCandidate>): StreamCandidate {
	return {
		source_id: 1,
		source_name: 'Src',
		title: 'Title',
		link: 'https://example.com/a',
		info_hash: '',
		protocol: 'http',
		size: 0,
		seeders: 0,
		peers: 0,
		resolution: '1080p',
		resolution_num: 1080,
		source_tag: '',
		codec: '',
		hdr: '',
		audio: '',
		languages: [],
		...over
	};
}

describe('resolveTotalDuration', () => {
	it('preserves full duration across FFmpeg event-HLS re-seeks instead of 18:46 / 0:46', () => {
		const dur = resolveTotalDuration({
			probeDuration: 3600,
			videoDuration: 46,
			knownDuration: 3600,
			savedDuration: 3600,
			metadataRuntimeMin: 60,
			isLocalEventHls: true,
			seekOffset: 1080,
			currentPos: 1126
		});
		expect(dur).toBe(3600);
	});

	it('falls back to knownDuration, savedDuration, then metadata runtime on event HLS without probe duration', () => {
		expect(
			resolveTotalDuration({
				videoDuration: 46,
				knownDuration: 2400,
				savedDuration: 2300,
				metadataRuntimeMin: 40,
				isLocalEventHls: true,
				seekOffset: 1080,
				currentPos: 1126
			})
		).toBe(2400);

		expect(
			resolveTotalDuration({
				videoDuration: 46,
				knownDuration: 0,
				metadataRuntimeMin: 42,
				isLocalEventHls: true,
				seekOffset: 1080,
				currentPos: 1126
			})
		).toBe(2520);
	});

	it('clamps up to currentPos if currentPos exceeds known fallback', () => {
		expect(
			resolveTotalDuration({
				videoDuration: 46,
				knownDuration: 0,
				isLocalEventHls: true,
				seekOffset: 1080,
				currentPos: 1126
			})
		).toBe(1126);
	});
});

describe('shouldAcceptEnded', () => {
	it('blocks premature ended events during source switching or far before the end', () => {
		expect(
			shouldAcceptEnded({
				switchingSource: true,
				seamlessStatus: '',
				pipelineMode: 'hls',
				currentPos: 3590,
				totalDuration: 3600
			})
		).toBe(false);
		expect(
			shouldAcceptEnded({
				switchingSource: false,
				seamlessStatus: 'Switching source...',
				pipelineMode: 'hls',
				currentPos: 3590,
				totalDuration: 3600
			})
		).toBe(false);
		expect(
			shouldAcceptEnded({
				switchingSource: false,
				seamlessStatus: '',
				pipelineMode: 'hls',
				currentPos: 1126,
				totalDuration: 3600
			})
		).toBe(false);
	});

	it('accepts natural completion near the end of the title', () => {
		expect(
			shouldAcceptEnded({
				switchingSource: false,
				seamlessStatus: '',
				pipelineMode: 'hls',
				currentPos: 3588,
				totalDuration: 3600
			})
		).toBe(true);
	});
});

describe('canSeekInLocalHlsBuffer', () => {
	it('allows seeking inside already-buffered local HLS segment window and rejects outside', () => {
		const fakeVideo = {
			duration: 120,
			seekable: {
				length: 1,
				end: () => 120
			}
		} as unknown as HTMLVideoElement;

		expect(canSeekInLocalHlsBuffer(fakeVideo, 600, 650, 90)).toBe(true);
		expect(canSeekInLocalHlsBuffer(fakeVideo, 600, 580, 90)).toBe(false);
		expect(canSeekInLocalHlsBuffer(fakeVideo, 600, 750, 90)).toBe(false);
	});
});

describe('sortCandidatesForQuality', () => {
	it('puts quality-matched fastest-probed streams at the top', () => {
		const cands = [
			cand({ id: 'slow-1080', kind: 'hls', resolution: '1080p', resolution_num: 1080, probe_ms: 480, fast_start: true }),
			cand({ id: 'fast-720', kind: 'hls', resolution: '720p', resolution_num: 720, probe_ms: 80, fast_start: true }),
			cand({ id: 'fast-1080', kind: 'hls', resolution: '1080p', resolution_num: 1080, probe_ms: 110, fast_start: true })
		];
		const sorted = sortCandidatesForQuality(cands, '1080p');
		expect(sorted.map((c) => c.id)).toEqual(['fast-1080', 'slow-1080', 'fast-720']);
	});
});

describe('pickBestSubtitleCandidate & formatSyncDelta', () => {
	it('prefers preferred language, non-forced/non-HI, and matching release tokens', () => {
		const sub = (over: Partial<StreamSubCandidate>): StreamSubCandidate => ({
			id: '1',
			site_id: 1,
			site_name: 'OpenSubs',
			site_kind: 'opensubs',
			language: 'en',
			...over
		});
		const best = pickBestSubtitleCandidate(
			[
				sub({ id: 'tr-1', language: 'tr', downloads: 5000 }),
				sub({ id: 'en-hi', language: 'en', hearing_impaired: true, downloads: 9000 }),
				sub({ id: 'en-webdl', language: 'en', release_names: ['Show.S01E01.1080p.WEB-DL'], downloads: 200 })
			],
			['en', 'tr'],
			'Show.S01E01.1080p.WEB-DL-GRP'
		);
		expect(best?.id).toBe('en-webdl');
	});

	it('formats sync offsets cleanly', () => {
		expect(formatSyncDelta(30)).toBe('Already in sync');
		expect(formatSyncDelta(1450)).toBe('Synced +1.45s');
		expect(formatSyncDelta(-800)).toBe('Synced -0.80s');
	});
});

describe('extractRegularSeasons', () => {
	it('extracts sorted regular seasons from details.seasons and falls back to details.show.seasons', () => {
		const fromMeta = {
			kind: 'show',
			tmdb: '220542',
			name: 'The Apothecary Diaries',
			genres: ['Animation'],
			cast: [],
			similar: [],
			seasons: [
				{ number: 3, episodes: 12 },
				{ number: 0, episodes: 58 },
				{ number: 1, episodes: 24 },
				{ number: 2, episodes: 24 }
			]
		} as TitleDetails;
		expect(extractRegularSeasons(fromMeta).map((s) => s.number)).toEqual([1, 2, 3]);

		const fromShow = {
			kind: 'show',
			tmdb: '1',
			name: 'Fallback',
			genres: [],
			cast: [],
			similar: [],
			show: {
				source: 'tmdb',
				name: 'Fallback',
				status: 'running',
				ids: { tmdb: '1' },
				seasons: [{ number: 0 }, { number: 2 }, { number: 1 }]
			}
		} as TitleDetails;
		expect(extractRegularSeasons(fromShow).map((s) => s.number)).toEqual([1, 2]);
	});
});

describe('selectAutoPlayCandidate', () => {
	it('always prefers instant sources over torrents and picks fastest 1080p within instant', () => {
		const mixedInstant = [
			cand({ id: '1080-tor', kind: 'torrent', instant: false, resolution: '1080p', resolution_num: 1080, seeders: 400 }),
			cand({ id: '4k-fast', kind: 'hls', instant: true, resolution: '2160p', resolution_num: 2160, probe_ms: 50, fast_start: true }),
			cand({ id: '720-fast', kind: 'hls', instant: true, resolution: '720p', resolution_num: 720, probe_ms: 40, fast_start: true }),
			cand({ id: '1080-slow', kind: 'hls', instant: true, resolution: '1080p', resolution_num: 1080, probe_ms: 350, fast_start: true }),
			cand({ id: '1080-fast', kind: 'hls', instant: true, resolution: '1080p', resolution_num: 1080, probe_ms: 95, fast_start: true })
		];
		expect(selectAutoPlayCandidate(mixedInstant, '1080p', false)?.id).toBe('1080-fast');

		const non1080InstantVs1080Torrent = [
			cand({ id: '1080-tor', kind: 'torrent', instant: false, resolution: '1080p', resolution_num: 1080, seeders: 250 }),
			cand({ id: '720-inst', kind: 'hls', instant: true, resolution: '720p', resolution_num: 720, probe_ms: 80, fast_start: true }),
			cand({ id: 'auto-inst', kind: 'hls', instant: true, resolution: '', resolution_num: 0, probe_ms: 90, fast_start: true })
		];
		expect(selectAutoPlayCandidate(non1080InstantVs1080Torrent, '1080p', false)?.id).toBe('720-inst');
		expect(selectAutoPlayCandidate(non1080InstantVs1080Torrent, '1080p', true)?.id).toBe('720-inst');
		expect(sortCandidatesForQuality(non1080InstantVs1080Torrent, '1080p').map((c) => c.id)).toEqual([
			'720-inst',
			'auto-inst',
			'1080-tor'
		]);

		const torrentOnly = [
			cand({ id: '720-tor', kind: 'torrent', instant: false, resolution: '720p', resolution_num: 720, seeders: 50 }),
			cand({ id: '1080-tor', kind: 'torrent', instant: false, resolution: '1080p', resolution_num: 1080, seeders: 120 })
		];
		expect(selectAutoPlayCandidate(torrentOnly, '1080p', false)).toBeNull();
		expect(selectAutoPlayCandidate(torrentOnly, '1080p', true)?.id).toBe('1080-tor');
	});
});

describe('seek helpers', () => {
	it('ignores stale pre-seek timeupdate and accepts target once landed', () => {
		expect(
			shouldAcceptTimeUpdate({
				switchingSource: false,
				reseekingHls: false,
				videoSeeking: true,
				pendingSeekTarget: 900,
				actualPos: 12
			})
		).toBe('ignore');
		expect(
			shouldAcceptTimeUpdate({
				switchingSource: false,
				reseekingHls: false,
				videoSeeking: false,
				pendingSeekTarget: 900,
				actualPos: 12
			})
		).toBe('ignore');
		expect(
			shouldAcceptTimeUpdate({
				switchingSource: false,
				reseekingHls: false,
				videoSeeking: false,
				pendingSeekTarget: 900,
				actualPos: 899.2
			})
		).toBe('landed');
		expect(
			shouldAcceptTimeUpdate({
				switchingSource: false,
				reseekingHls: false,
				videoSeeking: false,
				pendingSeekTarget: null,
				actualPos: 45
			})
		).toBe('accept');
	});

	it('detects buffered ranges and unseekable direct streams needing ffmpeg fallback', () => {
		const fakeRanges = {
			length: 1,
			start: () => 10,
			end: () => 120
		} as unknown as TimeRanges;
		expect(isTimeBuffered(fakeRanges, 60)).toBe(true);
		expect(isTimeBuffered(fakeRanges, 900)).toBe(false);

		expect(
			shouldFallbackDirectSeekToTranscode({
				pipelineMode: 'direct',
				ffmpegAvailable: true,
				pendingSeekTarget: 900,
				videoTime: 14,
				videoDuration: 3600,
				seekableEnd: 400
			})
		).toBe(true);
		expect(
			shouldFallbackDirectSeekToTranscode({
				pipelineMode: 'direct',
				ffmpegAvailable: true,
				pendingSeekTarget: 900,
				videoTime: 899,
				videoDuration: 3600,
				seekableEnd: 3600
			})
		).toBe(false);
	});
});

describe('mergeCandidateLists', () => {
	it('keeps distinct proxied instant candidates with unique IDs while preserving fast-only providers', () => {
		const fastOnly = cand({ id: 'fast-only-1', provider: 'FastOnly', kind: 'hls', instant: true, link: '', title: 'Movie (2024)' });
		const fastDup = cand({ id: 'vr-fast', provider: 'VidRock', kind: 'hls', instant: true, link: '', title: 'Movie (2024)' });
		const fullA = cand({ id: 'vr-1', provider: 'VidRock', kind: 'hls', instant: true, link: '', title: 'Movie (2024)' });
		const fullB = cand({ id: 'vr-2', provider: 'VidRock', kind: 'hls', instant: true, link: '', title: 'Movie (2024)' });
		const merged = mergeCandidateLists([fastOnly, fastDup], [fullA, fullB]);
		expect(merged.map((c) => c.id)).toEqual(['vr-1', 'vr-2', 'fast-only-1']);
	});
});
