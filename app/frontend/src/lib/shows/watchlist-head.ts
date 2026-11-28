// The Watchlist header's facts: the counts and the title to pick up next.

import type { TitleView } from '#lib/core/types.ts';
import type { HeroStat, SeenCounts, WatchedMark } from '#lib/mediafiles/index.ts';
import type { WatchEntry } from '#lib/stream/index.ts';

type Tr = (key: string, vars?: Record<string, string | number>) => string;
type KindCount = { key: string; label: string; count: number; icon: HeroStat['icon'] };

// The watch states across movies and shows (the kind tabs carry each kind's count); Music, which has none, shows its own count.
export function heroStats(kinds: KindCount[], tally: SeenCounts, onMusic: boolean, fmt: (n: number) => string, tr: Tr): HeroStat[] {
	if (onMusic) return kinds.filter((k) => k.key === 'artist').map((k) => ({ key: k.key, label: k.label, value: fmt(k.count), icon: k.icon }));
	if (!(tally.watched + tally.progress + tally.unwatched)) return [];
	return [
		{ key: 'watched', label: tr('library.watch.seen.watched'), value: fmt(tally.watched), icon: 'check', tone: 'ok' },
		{ key: 'progress', label: tr('library.watch.seen.progress'), value: fmt(tally.progress), icon: 'clock', tone: 'info' },
		{ key: 'unwatched', label: tr('library.watch.seen.unwatched'), value: fmt(tally.unwatched), icon: 'eye', tone: 'muted' }
	];
}

// The title played most recently that is not finished (a stopped movie, a show with episodes left), else none.
export function continueTitle(pool: TitleView[], entriesOf: (t: TitleView) => WatchEntry[] | undefined, markOf: (t: TitleView) => WatchedMark | null): TitleView | undefined {
	let best: TitleView | undefined;
	let bestAt = 0;
	for (const t of pool) {
		const e = entriesOf(t)?.[0];
		if (!e || (t.kind === 'movie' && e.completed) || (t.kind === 'show' && markOf(t)?.state === 'watched')) continue;
		const at = Date.parse(e.updated_at ?? '') || 1;
		if (at > bestAt) [best, bestAt] = [t, at];
	}
	return best;
}

// The newest title added, for the header when nothing is under way.
export function newestTitle(pool: TitleView[]): TitleView | undefined {
	return [...pool].sort((a, b) => (b.added_at ?? '').localeCompare(a.added_at ?? ''))[0];
}
