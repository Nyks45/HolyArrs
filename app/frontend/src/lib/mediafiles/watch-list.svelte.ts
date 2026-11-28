// The Watchlist page's watch state, shared with its cards: marks from the watch history and the quick actions, set by the page.

import type { TitleView } from '../core/types.ts';
import { titleWatched, type WatchedIndex, type WatchedMark } from './watched.ts';
import { mergeMarks } from './watch-state.ts';

export type WatchPlay = { href: string; how: 'play' | 'resume' | 'next' };
export type WatchActs = {
	play: (t: TitleView) => WatchPlay | null;
	toggle: (t: TitleView, watched: boolean) => void;
	// Whether HolyArrs' own history counts the title as watched (the media server's marks are only read).
	marked: (t: TitleView) => boolean;
};

// Empty and without actions outside the Watchlist page, so other lists show only the media server's marks.
export const watchList = $state<{ marks: Record<number, WatchedMark>; acts: WatchActs | null; busy: number[] }>({ marks: {}, acts: null, busy: [] });

export function watchedOf(t: Pick<TitleView, 'id' | 'kind' | 'show'>, ix: WatchedIndex): WatchedMark | null {
	return mergeMarks(titleWatched(t, ix), watchList.marks[t.id]);
}
