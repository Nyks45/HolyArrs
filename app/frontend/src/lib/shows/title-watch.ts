// Watch history read per Watchlist title: its mark, where Play picks up, and what Mark watched writes.

import type { TitleView } from '#lib/core/types.ts';
import { to } from '#lib/core/paths.ts';
import type { WatchedMark, WatchPlay } from '#lib/mediafiles/index.ts';
import { classifyAirDate, type WatchEntry } from '#lib/stream/index.ts';
import { buildWatchedEntry, episodeWatchKey, type ShowWatchMeta } from './episode-tracking.ts';

type TitleLike = Pick<TitleView, 'id' | 'kind' | 'ids' | 'name' | 'year' | 'artwork' | 'backdrop' | 'show'>;

export const tmdbOf = (t: Pick<TitleView, 'ids'>): string => (t.ids?.tmdb ?? '').trim();
const keyOf = (kind: string, tmdb: string) => `${kind}:${tmdb}`;
const stamp = (e: WatchEntry) => Date.parse(e.updated_at ?? '') || 0;

// The history grouped per title ("movie:603", "show:1399"), newest entry first.
export function historyByTitle(items: WatchEntry[]): Map<string, WatchEntry[]> {
	const out = new Map<string, WatchEntry[]>();
	for (const e of items) {
		const tmdb = (e.tmdb ?? '').trim();
		if (!tmdb) continue;
		const k = keyOf(e.kind === 'show' ? 'show' : 'movie', tmdb);
		const list = out.get(k);
		if (list) list.push(e);
		else out.set(k, [e]);
	}
	for (const list of out.values()) list.sort((a, b) => stamp(b) - stamp(a));
	return out;
}

// Episodes out so far in the regular seasons, as the Library counts them.
export function airedEpisodes(t: Pick<TitleView, 'show'>): number {
	return (t.show?.seasons ?? []).filter((s) => s.number > 0).reduce((n, s) => n + (s.aired ?? 0), 0);
}

// A movie is watched once finished and partly watched while stopped part way. A show counts finished episodes outside the specials against the aired ones.
export function historyMark(t: TitleLike, entries: WatchEntry[] | undefined): WatchedMark | null {
	if (!entries?.length) return null;
	if (t.kind === 'movie') {
		const e = entries[0];
		if (e.completed) return { state: 'watched', seen: 1, of: 1 };
		const pct = e.progress_pct ?? (e.duration_sec > 0 ? (e.position_sec / e.duration_sec) * 100 : 0);
		return e.position_sec >= 5 || pct > 0 ? { state: 'partly', seen: 0, of: 0, pct: Math.max(1, Math.min(99, pct)) } : null;
	}
	if (t.kind !== 'show') return null;
	const done = new Set(entries.filter((e) => e.completed && (e.season ?? 0) > 0).map((e) => `${e.season}:${e.episode}`)).size;
	const started = entries.some((e) => !e.completed && e.position_sec >= 5);
	const of = airedEpisodes(t);
	if (of > 0 && done >= of) return { state: 'watched', seen: done, of };
	if (!done && !started) return null;
	return { state: 'partly', seen: Math.min(done, of || done), of };
}

// One mark per Library title with history, keyed by its id.
export function historyMarks(titles: TitleLike[], items: WatchEntry[]): Record<number, WatchedMark> {
	const by = historyByTitle(items);
	const out: Record<number, WatchedMark> = {};
	for (const t of titles) {
		const tmdb = tmdbOf(t);
		if (!tmdb || (t.kind !== 'movie' && t.kind !== 'show')) continue;
		const m = historyMark(t, by.get(keyOf(t.kind, tmdb)));
		if (m) out[t.id] = m;
	}
	return out;
}

// Where Play goes: a stopped movie or episode resumes, a finished episode leads to the next one out, anything else starts from the top.
export function playTarget(t: TitleLike, entries: WatchEntry[] | undefined, from: string): WatchPlay | null {
	const tmdb = tmdbOf(t);
	if (!tmdb || (t.kind !== 'movie' && t.kind !== 'show')) return null;
	const back = `&from=${encodeURIComponent(from)}`;
	const latest = entries?.[0];
	if (t.kind === 'movie') {
		const resume = !!latest && !latest.completed && latest.position_sec > 10;
		return { href: to(`/watch/movie/${encodeURIComponent(tmdb)}?resume=${resume ? 1 : 0}${back}`), how: resume ? 'resume' : 'play' };
	}
	const base = `/watch/show/${encodeURIComponent(tmdb)}`;
	// A show watched to its newest episode starts again from the top.
	if (latest && (latest.season ?? 0) > 0 && (latest.episode ?? 0) > 0 && historyMark(t, entries)?.state !== 'watched') {
		if (!latest.completed && latest.position_sec > 10) return { href: to(`${base}?s=${latest.season}&e=${latest.episode}&resume=1${back}`), how: 'resume' };
		if (latest.completed) return { href: to(`${base}?s=${latest.season}&e=${latest.episode}&next=1${back}`), how: 'next' };
	}
	const first = (t.show?.seasons ?? []).filter((s) => s.number > 0 && (s.aired ?? 0) > 0).sort((a, b) => a.number - b.number)[0];
	return { href: to(`${base}?s=${first?.number ?? 1}&e=1&resume=0${back}`), how: 'play' };
}

export function watchMetaOf(t: TitleLike): ShowWatchMeta {
	return { tmdb: tmdbOf(t), title: t.name, year: t.year, imdb: t.ids?.imdb, poster: t.artwork?.[0] ?? null, backdrop: t.backdrop ?? null };
}

// A finished movie, as Mark watched saves it.
export function watchedMovieEntry(meta: ShowWatchMeta): WatchEntry {
	return { key: `movie:${meta.tmdb}`, kind: 'movie', tmdb: meta.tmdb, imdb: meta.imdb, title: meta.title, year: meta.year, poster: meta.poster ?? undefined, backdrop: meta.backdrop ?? undefined, position_sec: 0, duration_sec: 0, progress_pct: 100, completed: true, updated_at: new Date().toISOString() };
}

type SeasonEps = { season: number; episodes: { number: number; name?: string; air_day?: string }[] };

// Every episode out so far in the regular seasons, finished. A season whose episode list could not be read counts 1 to its aired number.
export function watchedShowEntries(t: TitleLike, read: SeasonEps[], now = new Date()): WatchEntry[] {
	const meta = watchMetaOf(t);
	const out: WatchEntry[] = [];
	for (const s of (t.show?.seasons ?? []).filter((x) => x.number > 0)) {
		const got = read.find((r) => r.season === s.number);
		const eps = got ? got.episodes.filter((e) => e.number > 0 && classifyAirDate(e.air_day, now).isReleased) : Array.from({ length: s.aired ?? 0 }, (_, i) => ({ number: i + 1, name: undefined }));
		for (const e of eps) out.push(buildWatchedEntry(meta, s.number, e.number, e.name));
	}
	return out;
}

// Unmarking a show clears every entry of it, finished or started, so it reads as unwatched again.
export function showHistoryKeys(t: TitleLike, entries: WatchEntry[] | undefined): string[] {
	const tmdb = tmdbOf(t);
	return (entries ?? []).map((e) => e.key || episodeWatchKey(tmdb, e.season ?? 0, e.episode ?? 0));
}
