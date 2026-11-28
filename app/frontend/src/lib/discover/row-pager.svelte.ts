// Reads an Explore row's later pages as it scrolls (sideways or in its See all grid), adding the cards it does not show yet.

import { api } from '#lib/core/api.ts';
import type { ExploreCard } from '#lib/ui/title-cards.ts';
import { moreCards, newCards, ROW_PAGE_FIRST, ROW_PAGE_LAST, ROW_PAGE_TRIES, rowMorePath, type RowMoreAnswer } from './row-more.ts';

export interface PagedRow {
	id: string;
	more?: string;
	cards: readonly ExploreCard[];
}

export type RowPager = ReturnType<typeof createRowPager>;

/** One row's pager. The row may change under it (a new head): its own pages then start over. */
export function createRowPager(row: () => PagedRow, signal?: AbortSignal) {
	let extra = $state<ExploreCard[]>([]);
	let extraFor = $state('');
	let doneFor = $state('');
	let busy = $state(false);
	let page = ROW_PAGE_FIRST;
	const keyOf = (r: PagedRow) => `${r.id}|${r.more ?? ''}`;
	const cards = $derived.by(() => {
		const r = row();
		return extraFor === keyOf(r) && extra.length ? [...r.cards, ...extra] : r.cards;
	});
	const hasMore = $derived.by(() => {
		const r = row();
		return !!r.more && doneFor !== keyOf(r);
	});

	async function load() {
		const r = row();
		const k = keyOf(r);
		if (busy || !r.more || doneFor === k) return;
		if (extraFor !== k) {
			extraFor = k;
			extra = [];
			page = ROW_PAGE_FIRST;
		}
		busy = true;
		try {
			for (let tries = 0; tries < ROW_PAGE_TRIES; tries++) {
				const a = await api.get<RowMoreAnswer>(rowMorePath(r.more, page), signal);
				if (extraFor !== k) return;
				page++;
				const fresh = newCards([...r.cards, ...extra], moreCards(r.id, a.items));
				if (fresh.length) extra = [...extra, ...fresh];
				if (!a.more || page > ROW_PAGE_LAST) {
					doneFor = k;
					break;
				}
				if (fresh.length) break;
			}
		} catch {
			// A failed page ends this row's paging; the cards it has stay.
			if (extraFor === k) doneFor = k;
		} finally {
			busy = false;
		}
	}

	return {
		get cards() {
			return cards;
		},
		get hasMore() {
			return hasMore;
		},
		get busy() {
			return busy;
		},
		load
	};
}
