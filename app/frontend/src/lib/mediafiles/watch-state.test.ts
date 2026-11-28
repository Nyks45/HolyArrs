import { describe, expect, it } from 'vitest';
import { markShare, mergeMarks, passesSeen, readSeen, seenCounts, seenOf } from './watch-state.ts';
import { watchedWords, type WatchedMark } from './watched.ts';

const done: WatchedMark = { state: 'watched', seen: 1, of: 1 };
const half: WatchedMark = { state: 'partly', seen: 4, of: 8 };
const movieHalf: WatchedMark = { state: 'partly', seen: 0, of: 0, pct: 70 };

describe('watch state', () => {
	it('reads only known tabs from the link', () => {
		expect(readSeen('watched')).toBe('watched');
		expect(readSeen('progress')).toBe('progress');
		expect(readSeen('nope')).toBe('all');
		expect(readSeen(null)).toBe('all');
	});
	it('lets either source saying watched win, else the one that got further', () => {
		expect(mergeMarks(null, null)).toBeNull();
		expect(mergeMarks(half, null)).toBe(half);
		expect(mergeMarks(null, done)).toBe(done);
		expect(mergeMarks(half, done)).toBe(done);
		expect(mergeMarks(half, movieHalf)).toBe(movieHalf);
		expect(mergeMarks(movieHalf, half)).toBe(movieHalf);
	});
	it('measures how much a mark covers', () => {
		expect(markShare(done)).toBe(100);
		expect(markShare(half)).toBe(50);
		expect(markShare(movieHalf)).toBe(70);
		expect(markShare({ state: 'partly', seen: 3, of: 0 })).toBe(0);
		expect(markShare(null)).toBe(0);
	});
	it('sorts titles into the tabs and leaves Music out of every watch-state tab', () => {
		expect(seenOf(null)).toBe('unwatched');
		expect(seenOf(half)).toBe('progress');
		expect(seenOf(done)).toBe('watched');
		expect(passesSeen('movie', done, 'watched')).toBe(true);
		expect(passesSeen('movie', done, 'unwatched')).toBe(false);
		expect(passesSeen('show', null, 'unwatched')).toBe(true);
		expect(passesSeen('artist', null, 'unwatched')).toBe(false);
		expect(passesSeen('artist', null, 'all')).toBe(true);
	});
	it('counts each tab once per title', () => {
		const list = [
			{ kind: 'movie', m: done },
			{ kind: 'movie', m: null },
			{ kind: 'show', m: half },
			{ kind: 'show', m: null },
			{ kind: 'artist', m: null }
		];
		expect(seenCounts(list, (t) => t.m)).toEqual({ all: 5, unwatched: 2, progress: 1, watched: 1 });
	});
	it('names a movie stopped part way by how far it got', () => {
		const t = (key: string, vars?: Record<string, string | number>) => `${key}${vars ? JSON.stringify(vars) : ''}`;
		expect(watchedWords(movieHalf, t)).toBe('mediafiles.watched.pct{"pct":70}');
		expect(watchedWords(half, t)).toBe('mediafiles.watched.part{"seen":4,"of":8}');
	});
});
