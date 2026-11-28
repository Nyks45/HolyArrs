// The signed-in person's watch history for the Watchlist: one read for every card, and Mark watched or unwatched per title.

import { api, message } from '#lib/core/api.ts';
import type { TitleView } from '#lib/core/types.ts';
import { toasts } from '#lib/core/toasts.svelte.ts';
import { t as tr } from '#lib/core/i18n/i18n.svelte.ts';
import type { WatchActs } from '#lib/mediafiles/index.ts';
import { batchWatchHistory, deleteWatchHistory, listWatchHistory, saveWatchProgress, type WatchEntry } from '#lib/stream/index.ts';
import { historyByTitle, historyMarks, playTarget, showHistoryKeys, tmdbOf, watchedMovieEntry, watchedShowEntries, watchMetaOf } from './title-watch.ts';

type Ep = { number: number; name?: string; air_day?: string };

export function createWatchHistory(from: () => string) {
	let items = $state<WatchEntry[]>([]);
	let busy = $state<number[]>([]);
	const byTitle = $derived(historyByTitle(items));
	const entriesOf = (t: TitleView) => byTitle.get(`${t.kind === 'show' ? 'show' : 'movie'}:${tmdbOf(t)}`);

	// A failed read keeps what was there; the cards then show the media server's marks only.
	async function load() {
		try {
			items = (await listWatchHistory(2000)).items ?? [];
		} catch {
			/* Watch history is optional on this page. */
		}
	}

	function apply(saved: WatchEntry[], dropped: string[]) {
		const gone = new Set([...dropped, ...saved.map((e) => e.key ?? '')]);
		items = [...saved, ...items.filter((e) => !gone.has(e.key ?? ''))];
	}

	async function seasonsOf(t: TitleView) {
		const tmdb = encodeURIComponent(tmdbOf(t));
		const nums = (t.show?.seasons ?? []).filter((s) => s.number > 0 && (s.aired ?? 0) > 0).map((s) => s.number);
		const read = await Promise.all(nums.map((n) => api.get<{ episodes?: Ep[] }>(`/metadata/show/${tmdb}/seasons/${n}`).then((r) => ({ season: n, episodes: r.episodes ?? [] }), () => null)));
		return read.filter((r) => r !== null);
	}

	async function toggle(t: TitleView, watched: boolean) {
		if (!tmdbOf(t) || busy.includes(t.id)) return;
		busy = [...busy, t.id];
		try {
			if (t.kind === 'movie') {
				const key = `movie:${tmdbOf(t)}`;
				if (watched) apply([await saveWatchProgress(watchedMovieEntry(watchMetaOf(t)))], []);
				else {
					await deleteWatchHistory(key);
					apply([], [key]);
				}
			} else if (watched) {
				const upserts = watchedShowEntries(t, await seasonsOf(t));
				if (!upserts.length) {
					toasts.push(tr('library.watch.nothingAired', { name: t.name }), 'info');
					return;
				}
				apply((await batchWatchHistory({ upserts })).updated ?? upserts, []);
			} else {
				const keys = showHistoryKeys(t, entriesOf(t));
				if (keys.length) await batchWatchHistory({ delete_keys: keys });
				apply([], keys);
			}
			toasts.push(tr(watched ? 'library.watch.markedWatched' : 'library.watch.markedUnwatched', { name: t.name }), 'ok', { body: tr('library.watch.picksNote') });
		} catch (e) {
			toasts.push(tr('library.watch.markFailed', { name: t.name }), 'fail', { body: message(e) });
			await load();
		} finally {
			busy = busy.filter((id) => id !== t.id);
		}
	}

	const acts: WatchActs = {
		play: (t) => playTarget(t, entriesOf(t), from()),
		toggle: (t, watched) => void toggle(t, watched),
		marked: (t) => {
			const e = entriesOf(t);
			return t.kind === 'movie' ? !!e?.[0]?.completed : historyMarks([t], e ?? [])[t.id]?.state === 'watched';
		}
	};

	return {
		get items() {
			return items;
		},
		get busy() {
			return busy;
		},
		marks: (titles: TitleView[]) => historyMarks(titles, items),
		entriesOf,
		load,
		acts
	};
}

// Mark watched or unwatched for one movie (the title page): the saved entry, or null once unmarked. Toasts say how it went.
export async function setMovieWatched(meta: { tmdb: string; title: string; year?: number; imdb?: string; poster?: string | null; backdrop?: string | null }, watched: boolean): Promise<WatchEntry | null | undefined> {
	try {
		const saved = watched ? await saveWatchProgress(watchedMovieEntry(meta)) : (await deleteWatchHistory(`movie:${meta.tmdb.trim()}`), null);
		toasts.push(tr(watched ? 'library.watch.markedWatched' : 'library.watch.markedUnwatched', { name: meta.title }), 'ok', { body: tr('library.watch.picksNote') });
		return saved;
	} catch (e) {
		toasts.push(tr('library.watch.markFailed', { name: meta.title }), 'fail', { body: message(e) });
		return undefined;
	}
}
