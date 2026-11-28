// The later pages of an add-on catalog shelf, and where an IMDb card opens.

import type { AddonCatalogItem, AddonCatalogShelf } from './streaming-config-api.ts';

/** A catalog fetch keeps this many items (the server's cap), so a full page likely has more after it. */
export const ADDON_PAGE = 30;
/** Stremio add-ons page by 100; a skip they round down repeats a page, so the next ask jumps to the next hundred. */
export const ADDON_STEP = 100;

export const shelfKey = (s: Pick<AddonCatalogShelf, 'addon_id' | 'type' | 'catalog_id'>): string => `${s.addon_id}:${s.type}:${s.catalog_id}`;

/** A shelf may page when its add-on says so, or when its first page came back full. */
export function mayPage(s: Pick<AddonCatalogShelf, 'pages' | 'items'>): boolean {
	return !!s.pages || s.items.length >= ADDON_PAGE;
}

/** The items of got the shelf does not show yet, each once. */
export function newItems(have: readonly AddonCatalogItem[], got: readonly AddonCatalogItem[] | null | undefined): AddonCatalogItem[] {
	const seen = new Set(have.map((i) => i.id));
	const out: AddonCatalogItem[] = [];
	for (const it of got ?? []) {
		if (seen.has(it.id)) continue;
		seen.add(it.id);
		out.push(it);
	}
	return out;
}

/** The next skip after a page that brought nothing new: the next hundred. */
export function nextHundred(skip: number): number {
	return (Math.floor(skip / ADDON_STEP) + 1) * ADDON_STEP;
}

/** The lookup that finds a card's TMDB id from its IMDb id. */
export function imdbLookupPath(it: Pick<AddonCatalogItem, 'kind' | 'imdb_id'>): string {
	const term = encodeURIComponent(`imdb:${it.imdb_id}`);
	return it.kind === 'show' ? `/shows/lookup?term=${term}` : `/titles/lookup?term=${term}`;
}
