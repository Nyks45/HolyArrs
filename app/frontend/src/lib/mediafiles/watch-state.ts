// Watch state on the Watchlist: the media server's Watched marks and HolyArrs' own watch history, read as one mark per title.

import type { WatchedMark } from './watched.ts';

// The watch-state tabs over the list.
export type SeenFilter = 'all' | 'unwatched' | 'progress' | 'watched';
export const seenFilters: readonly SeenFilter[] = ['all', 'unwatched', 'progress', 'watched'];

export function readSeen(v: string | null | undefined): SeenFilter {
	return seenFilters.includes(v as SeenFilter) ? (v as SeenFilter) : 'all';
}

// How much of a title a mark covers, 0 to 100.
export function markShare(m: WatchedMark | null | undefined): number {
	if (!m) return 0;
	if (m.state === 'watched') return 100;
	if (m.pct !== undefined) return Math.max(0, Math.min(100, m.pct));
	return m.of > 0 ? Math.min(100, (Math.min(m.seen, m.of) / m.of) * 100) : 0;
}

// Either source saying watched wins; otherwise the mark that got further.
export function mergeMarks(a: WatchedMark | null | undefined, b: WatchedMark | null | undefined): WatchedMark | null {
	if (!a) return b ?? null;
	if (!b) return a;
	if (a.state === 'watched') return a;
	if (b.state === 'watched') return b;
	return markShare(b) > markShare(a) ? b : a;
}

export function seenOf(m: WatchedMark | null | undefined): Exclude<SeenFilter, 'all'> {
	return !m ? 'unwatched' : m.state === 'watched' ? 'watched' : 'progress';
}

// Music has no watch state, so a watch-state tab leaves artists out.
export function passesSeen(kind: string, m: WatchedMark | null | undefined, f: SeenFilter): boolean {
	if (f === 'all') return true;
	if (kind !== 'movie' && kind !== 'show') return false;
	return seenOf(m) === f;
}

export type SeenCounts = Record<SeenFilter, number>;

export function seenCounts<T extends { kind: string }>(list: T[], markOf: (t: T) => WatchedMark | null): SeenCounts {
	const out: SeenCounts = { all: list.length, unwatched: 0, progress: 0, watched: 0 };
	for (const t of list) {
		if (t.kind !== 'movie' && t.kind !== 'show') continue;
		out[seenOf(markOf(t))]++;
	}
	return out;
}
