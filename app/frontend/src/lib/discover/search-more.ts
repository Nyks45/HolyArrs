// Search results past the first page: where to read them and which of them are new.

import type { ProviderItem } from '#lib/ui/title-cards.ts';
import { cardTmdbOf, itemCard, type Card } from './search-cards.svelte.ts';

export interface SearchMoreAnswer {
	page: number;
	more: boolean;
	items: ProviderItem[] | null;
}

/** The last page Search reads. */
export const SEARCH_PAGE_LAST = 25;

export function searchMorePath(kind: 'movie' | 'show', q: string, page: number): string {
	return `/explore/search?${new URLSearchParams({ kind, q, page: String(page) }).toString()}`;
}

/** The answer's results the page does not show yet (by TMDB id and by key), each once. */
export function newResults(have: readonly Card[], items: readonly ProviderItem[] | null | undefined): Card[] {
	const ids = new Set(have.map(cardTmdbOf).filter(Boolean));
	const keys = new Set(have.map((c) => c.key));
	const out: Card[] = [];
	for (const d of items ?? []) {
		const c = itemCard(d);
		const id = cardTmdbOf(c);
		if (ids.has(id) || keys.has(c.key)) continue;
		ids.add(id);
		keys.add(c.key);
		out.push(c);
	}
	return out;
}
