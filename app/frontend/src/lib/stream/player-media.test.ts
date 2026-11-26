import { describe, expect, it } from 'vitest';
import {
	canSeekInLocalHlsBuffer,
	formatSyncDelta,
	pickBestSubtitleCandidate,
	resolveTotalDuration,
	shouldAcceptEnded,
	sortCandidatesForQuality
} from './player-media.ts';
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
