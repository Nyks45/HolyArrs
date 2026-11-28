import { describe, expect, it } from 'vitest';
import type { TitleView } from '#lib/core/types.ts';
import type { WatchEntry } from '#lib/stream/index.ts';
import { airedEpisodes, historyByTitle, historyMark, historyMarks, playTarget, showHistoryKeys, watchedMovieEntry, watchedShowEntries } from './title-watch.ts';
import { continueTitle, heroStats, newestTitle } from './watchlist-head.ts';

const movie = (id: number, tmdb: string, over: Partial<TitleView> = {}) => ({ id, kind: 'movie', name: `M${id}`, ids: { tmdb }, artwork: [], ...over }) as unknown as TitleView;
const show = (id: number, tmdb: string, aired: number[], over: Partial<TitleView> = {}) =>
	({ id, kind: 'show', name: `S${id}`, ids: { tmdb }, artwork: [], show: { seasons: [{ number: 0, aired: 3 }, ...aired.map((a, i) => ({ number: i + 1, aired: a }))] }, ...over }) as unknown as TitleView;
const entry = (over: Partial<WatchEntry>): WatchEntry => ({ kind: 'movie', tmdb: '1', title: 'x', position_sec: 0, duration_sec: 0, updated_at: '2026-01-01T00:00:00Z', ...over });
const ep = (season: number, episode: number, over: Partial<WatchEntry> = {}) => entry({ kind: 'show', tmdb: '50', season, episode, key: `show:50:s${season}e${episode}`, completed: true, ...over });

describe('historyMark', () => {
	it('marks a finished movie watched and a stopped one by how far it got', () => {
		expect(historyMark(movie(1, '1'), [entry({ completed: true })])).toEqual({ state: 'watched', seen: 1, of: 1 });
		expect(historyMark(movie(1, '1'), [entry({ position_sec: 600, duration_sec: 6000, progress_pct: 10 })])).toEqual({ state: 'partly', seen: 0, of: 0, pct: 10 });
		expect(historyMark(movie(1, '1'), [entry({ position_sec: 1 })])).toBeNull();
		expect(historyMark(movie(1, '1'), undefined)).toBeNull();
	});
	it('counts finished episodes outside the specials against the aired ones', () => {
		const s = show(5, '50', [2, 1]);
		expect(airedEpisodes(s)).toBe(3);
		expect(historyMark(s, [ep(1, 1), ep(1, 2), ep(2, 1)])).toEqual({ state: 'watched', seen: 3, of: 3 });
		expect(historyMark(s, [ep(1, 1), ep(0, 1)])).toEqual({ state: 'partly', seen: 1, of: 3 });
		expect(historyMark(s, [ep(1, 1, { completed: false, position_sec: 300 })])).toEqual({ state: 'partly', seen: 0, of: 3 });
		expect(historyMark(s, [ep(0, 1)])).toBeNull();
	});
	it('keys marks by Library id and ignores titles without a TMDB id', () => {
		const items = [entry({ tmdb: '1', completed: true }), ep(1, 1)];
		expect(historyMarks([movie(1, '1'), movie(2, ''), show(5, '50', [2])], items)).toEqual({ 1: { state: 'watched', seen: 1, of: 1 }, 5: { state: 'partly', seen: 1, of: 2 } });
		expect([...historyByTitle(items).keys()]).toEqual(['movie:1', 'show:50']);
	});
});

describe('playTarget', () => {
	it('resumes a stopped movie and starts any other from the top', () => {
		expect(playTarget(movie(1, '1'), [entry({ position_sec: 120 })], '/library?kind=movie')).toEqual({ how: 'resume', href: '/watch/movie/1?resume=1&from=%2Flibrary%3Fkind%3Dmovie' });
		expect(playTarget(movie(1, '1'), [entry({ completed: true })], '/l')).toEqual({ how: 'play', href: '/watch/movie/1?resume=0&from=%2Fl' });
		expect(playTarget(movie(1, ''), undefined, '/l')).toBeNull();
	});
	it('picks up a show where it stopped, after a finished episode, or at its first season', () => {
		const s = show(5, '50', [0, 4]);
		expect(playTarget(s, [ep(2, 3, { completed: false, position_sec: 50 })], '/l')?.href).toBe('/watch/show/50?s=2&e=3&resume=1&from=%2Fl');
		expect(playTarget(s, [ep(2, 3)], '/l')).toEqual({ how: 'next', href: '/watch/show/50?s=2&e=3&next=1&from=%2Fl' });
		expect(playTarget(s, undefined, '/l')).toEqual({ how: 'play', href: '/watch/show/50?s=2&e=1&resume=0&from=%2Fl' });
		const all = [ep(2, 1), ep(2, 2), ep(2, 3), ep(2, 4)];
		expect(playTarget(s, all, '/l')?.how).toBe('play');
	});
});

describe('marking', () => {
	it('saves a movie as one finished entry', () => {
		const e = watchedMovieEntry({ tmdb: '603', title: 'The Matrix', year: 1999 });
		expect(e).toMatchObject({ key: 'movie:603', kind: 'movie', tmdb: '603', completed: true, progress_pct: 100 });
	});
	it('marks every aired episode of the regular seasons, counting 1 to aired where the list was not read', () => {
		const s = show(5, '50', [3, 2]);
		const read = [{ season: 1, episodes: [{ number: 1, air_day: '2020-01-01' }, { number: 2, air_day: '2020-01-08' }, { number: 3, air_day: '2999-01-01' }] }];
		const keys = watchedShowEntries(s, read, new Date('2026-01-01')).map((e) => e.key);
		expect(keys).toEqual(['show:50:s1e1', 'show:50:s1e2', 'show:50:s2e1', 'show:50:s2e2']);
	});
	it('clears every entry of a show when unmarking', () => {
		expect(showHistoryKeys(show(5, '50', [2]), [ep(1, 1), ep(1, 2, { key: '' })])).toEqual(['show:50:s1e1', 'show:50:s1e2']);
		expect(showHistoryKeys(show(5, '50', [2]), undefined)).toEqual([]);
	});
});

describe('watchlist header', () => {
	const tr = (k: string) => k;
	it('counts the watch states, or the artists on the Music page', () => {
		const kinds = [{ key: 'movie', label: 'Movies', count: 3, icon: 'film' as const }, { key: 'artist', label: 'Music', count: 2, icon: 'music' as const }];
		expect(heroStats(kinds, { all: 0, watched: 0, progress: 0, unwatched: 0 }, false, String, tr)).toEqual([]);
		expect(heroStats(kinds, { all: 3, watched: 1, progress: 1, unwatched: 1 }, false, String, tr).map((s) => `${s.key}=${s.value}`)).toEqual(['watched=1', 'progress=1', 'unwatched=1']);
		expect(heroStats(kinds, { all: 3, watched: 1, progress: 1, unwatched: 1 }, true, String, tr).map((s) => `${s.key}=${s.value}`)).toEqual(['artist=2']);
	});
	it('features the title played last that is not finished', () => {
		const a = movie(1, '1'), b = movie(2, '2'), c = show(3, '3', [2]);
		const hist: Record<number, WatchEntry[]> = { 1: [entry({ completed: true, updated_at: '2026-03-01T00:00:00Z' })], 2: [entry({ position_sec: 90, updated_at: '2026-02-01T00:00:00Z' })], 3: [ep(1, 1, { updated_at: '2026-01-01T00:00:00Z' })] };
		expect(continueTitle([a, b, c], (t) => hist[t.id], () => null)?.id).toBe(2);
		expect(continueTitle([a], (t) => hist[t.id], () => null)).toBeUndefined();
		expect(newestTitle([movie(1, '1', { added_at: '2026-01-01' }), movie(2, '2', { added_at: '2026-05-01' })])?.id).toBe(2);
	});
});
