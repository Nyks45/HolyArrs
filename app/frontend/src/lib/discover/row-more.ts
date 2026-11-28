// The pages of one Explore row past its first: where to read them and how new cards join the row.

import { cardIdentity, type ExploreCard, type ProviderItem } from '#lib/ui/title-cards.ts';
import { serverCard } from './explore-server.ts';

/** GET /explore/row answers one page of a row. */
export interface RowMoreAnswer {
	page: number;
	more: boolean;
	items: ProviderItem[] | null;
}

/** The first page a row reads itself (the server sent page 1 with the row). */
export const ROW_PAGE_FIRST = 2;
/** The last page a row reads. */
export const ROW_PAGE_LAST = 60;
/** Pages read in a row while every card they bring is already shown. */
export const ROW_PAGE_TRIES = 3;

export function rowMorePath(more: string, page: number): string {
	return `/explore/row?${new URLSearchParams({ more, page: String(page) }).toString()}`;
}

/** The answer's cards in the row's own shape. */
export function moreCards(rowId: string, items: readonly ProviderItem[] | null | undefined): ExploreCard[] {
	return (items ?? []).map((d) => serverCard(d, rowId));
}

/** The cards of got that the row does not show yet, each once. */
export function newCards(have: readonly ExploreCard[], got: readonly ExploreCard[]): ExploreCard[] {
	const seen = new Set(have.map(cardIdentity));
	const keys = new Set(have.map((c) => c.key));
	const out: ExploreCard[] = [];
	for (const c of got) {
		const id = cardIdentity(c);
		if (seen.has(id) || keys.has(c.key)) continue;
		seen.add(id);
		keys.add(c.key);
		out.push(c);
	}
	return out;
}
