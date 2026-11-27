import { describe, expect, it } from 'vitest';
import type { WatchEntry } from '#lib/stream/index.ts';
import {
	applyWatchTrackingChange,
	buildWatchedEntry,
	classifyAirDate,
	episodeWatchKey,
	pickDefaultSeason,
	resolveShowResumeTarget,
	summarizeSeasonEpisodes
} from './episode-tracking.ts';

const refNow = new Date(2026, 10, 27, 12, 0, 0);

const mkEntry = (over: Partial<WatchEntry>): WatchEntry => ({
	kind: 'show',
	tmdb: '1396',
	title: 'Breaking Bad',
	season: 1,
	episode: 1,
	position_sec: 0,
	duration_sec: 2400,
	completed: false,
	...over
});

describe('classifyAirDate', () => {
	it('handles TBA, today, tomorrow, near upcoming, far upcoming, and recent aired', () => {
		expect(classifyAirDate(undefined, refNow)).toEqual({
			status: 'tba',
			isReleased: false,
			isNew: false,
			daysUntil: null,
			badge: 'TBA'
		});
		expect(classifyAirDate('bad-date', refNow).status).toBe('tba');
		expect(classifyAirDate('2026-11-27', refNow)).toEqual({
			status: 'today',
			isReleased: true,
			isNew: true,
			daysUntil: 0,
			badge: 'Airs today'
		});
		expect(classifyAirDate('2026-11-28', refNow)).toEqual({
			status: 'upcoming',
			isReleased: false,
			isNew: false,
			daysUntil: 1,
			badge: 'Tomorrow'
		});
		expect(classifyAirDate('2026-12-05', refNow)).toEqual({
			status: 'upcoming',
			isReleased: false,
			isNew: false,
			daysUntil: 8,
			badge: 'In 8d'
		});
		expect(classifyAirDate('2027-02-10', refNow).badge).toBe('Upcoming');
		expect(classifyAirDate('2026-11-22', refNow)).toEqual({
			status: 'aired',
			isReleased: true,
			isNew: true,
			daysUntil: -5,
			badge: 'New'
		});
		expect(classifyAirDate('2026-10-01', refNow)).toEqual({
			status: 'aired',
			isReleased: true,
			isNew: false,
			daysUntil: -57,
			badge: ''
		});
	});
});

describe('resolveShowResumeTarget & pickDefaultSeason', () => {
	const seasons = [
		{ number: 0, episodes: 4 },
		{ number: 1, episodes: 7 },
		{ number: 2, episodes: 13 }
	];

	it('resumes in-progress episode first', () => {
		const latest = mkEntry({ season: 1, episode: 4, position_sec: 840, completed: false });
		expect(resolveShowResumeTarget(seasons, latest, [latest])).toEqual({
			kind: 'resume',
			season: 1,
			episode: 4,
			positionSec: 840
		});
		expect(pickDefaultSeason(seasons, latest, [latest])).toBe(1);
	});

	it('advances to Next Up within the same season when latest is completed', () => {
		const latest = mkEntry({ season: 1, episode: 3, completed: true });
		expect(resolveShowResumeTarget(seasons, latest, [latest])).toEqual({
			kind: 'next',
			season: 1,
			episode: 4,
			positionSec: 0
		});
	});

	it('advances to S02 E01 when final episode of S01 is completed', () => {
		const latest = mkEntry({ season: 1, episode: 7, completed: true });
		expect(resolveShowResumeTarget(seasons, latest, [latest])).toEqual({
			kind: 'next',
			season: 2,
			episode: 1,
			positionSec: 0
		});
		expect(pickDefaultSeason(seasons, latest, [latest])).toBe(2);
	});

	it('returns caught_up on final episode of final season', () => {
		const latest = mkEntry({ season: 2, episode: 13, completed: true });
		expect(resolveShowResumeTarget(seasons, latest, [latest])).toEqual({
			kind: 'caught_up',
			season: 2,
			episode: 13,
			positionSec: 0
		});
	});

	it('starts at S01 E01 while opening newest season by default with no watch history', () => {
		expect(resolveShowResumeTarget(seasons, null, [])).toEqual({
			kind: 'start',
			season: 1,
			episode: 1,
			positionSec: 0
		});
		expect(pickDefaultSeason(seasons, null, [])).toBe(2);
	});
});

describe('summarizeSeasonEpisodes & payload helpers', () => {
	it('computes out vs upcoming counts and next upcoming air badge', () => {
		const eps = [
			{ number: 1, air_day: '2026-10-15' },
			{ number: 2, air_day: '2026-11-27' },
			{ number: 3, air_day: '2026-12-01' },
			{ number: 4, air_day: undefined }
		];
		const sum = summarizeSeasonEpisodes(
			eps,
			1,
			{ '1:1': { progress_pct: 100, completed: true, position_sec: 0 } },
			refNow
		);
		expect(sum).toEqual({
			total: 4,
			airedCount: 2,
			upcomingCount: 1,
			tbaCount: 1,
			watchedCount: 1,
			unwatchedAiredCount: 1,
			allAiredWatched: false,
			nextUpcoming: { episode: 3, badge: 'In 4d', airDay: '2026-12-01' }
		});
	});

	it('builds canonical episode keys and watched entries', () => {
		expect(episodeWatchKey(' 1396 ', 1, 5)).toBe('show:1396:s1e5');
		const e = buildWatchedEntry({ tmdb: '1396', title: 'Breaking Bad', year: 2008 }, 1, 5, 'Gray Matter');
		expect(e.key).toBe('show:1396:s1e5');
		expect(e.completed).toBe(true);
		expect(e.progress_pct).toBe(100);

		const e1 = buildWatchedEntry({ tmdb: '1396', title: 'Breaking Bad' }, 1, 1, 'Pilot');
		const e2 = buildWatchedEntry({ tmdb: '1396', title: 'Breaking Bad' }, 1, 2, 'Cat');
		const added = applyWatchTrackingChange([], [e1, e2], []);
		expect(added.episodes).toHaveLength(2);
		expect(added.latest?.episode).toBe(2);

		const removed = applyWatchTrackingChange(added.episodes, [], [{ season: 1, episode: 2 }]);
		expect(removed.episodes).toHaveLength(1);
		expect(removed.latest?.episode).toBe(1);
	});
});
