import { describe, expect, it } from 'vitest';
import { hasLandedAt, matchSavedCandidate, resumeTarget, type ResumeState } from './player-resume';
import type { StreamCandidate, WatchEntry } from './stream-api';

const saved = (position_sec: number, completed = false) => ({ position_sec, completed }) as WatchEntry;
const base: ResumeState = { pipelineMode: 'direct', pendingSeekTarget: null, currentPos: 0, pendingStartSec: 0, landed: false, lastGood: 0, saved: null };

describe('resumeTarget', () => {
	it('keeps a pending seek target', () => {
		expect(resumeTarget({ ...base, pendingSeekTarget: 600, currentPos: 3, landed: true })).toBe(600);
	});
	it('uses the playhead once the source reached its start', () => {
		expect(resumeTarget({ ...base, landed: true, currentPos: 1234, pendingStartSec: 900 })).toBe(1234);
	});
	it('ignores a playhead stuck at 0 when the resume seek never landed', () => {
		expect(resumeTarget({ ...base, currentPos: 7, pendingStartSec: 1500 })).toBe(1500);
	});
	it('falls back to the saved position when nothing is playing', () => {
		expect(resumeTarget({ ...base, pipelineMode: 'none', saved: saved(1800) })).toBe(1800);
	});
	it('does not resume a completed entry', () => {
		expect(resumeTarget({ ...base, pipelineMode: 'none', saved: saved(1800, true) })).toBe(0);
	});
	it('treats the first ten seconds as the start', () => {
		expect(resumeTarget({ ...base, landed: true, currentPos: 8 })).toBe(0);
	});
	it('keeps the last good position after a playing source failed', () => {
		expect(resumeTarget({ ...base, pipelineMode: 'none', landed: true, currentPos: 0, lastGood: 2400, pendingStartSec: 900, saved: saved(900) })).toBe(2400);
	});
});

describe('hasLandedAt', () => {
	it('accepts any position for a start near zero', () => expect(hasLandedAt(4, 0)).toBe(true));
	it('needs the playhead near a resume point', () => {
		expect(hasLandedAt(1500, 2)).toBe(false);
		expect(hasLandedAt(1500, 1498)).toBe(true);
	});
});

describe('matchSavedCandidate', () => {
	const cand = (over: Partial<StreamCandidate>) => ({ link: '', info_hash: '', ...over }) as StreamCandidate;
	it('prefers the debrid route for the saved torrent', () => {
		const plain = cand({ info_hash: 'ABC', link: 'magnet:1' });
		const debrid = cand({ info_hash: 'abc', debrid_provider: 'rd' });
		expect(matchSavedCandidate({ info_hash: 'abc' } as WatchEntry, [plain, debrid])).toBe(debrid);
	});
	it('matches by link when no hash is saved', () => {
		const c = cand({ link: 'https://x/y.mkv' });
		expect(matchSavedCandidate({ release_link: 'https://x/y.mkv' } as WatchEntry, [c])).toBe(c);
	});
	it('returns null without a match', () => expect(matchSavedCandidate({ info_hash: 'z' } as WatchEntry, [cand({})])).toBe(null));
});
