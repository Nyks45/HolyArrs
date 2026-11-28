import { describe, expect, it } from 'vitest';
import { imdbLookupPath, mayPage, newItems, nextHundred } from './addon-pages.ts';
import type { AddonCatalogItem } from './streaming-config-api.ts';

const item = (id: string): AddonCatalogItem => ({ id, kind: 'movie', name: id });

describe('add-on catalog pages', () => {
	it('pages a shelf its add-on says pages, or one whose first page came back full', () => {
		expect(mayPage({ pages: true, items: [] })).toBe(true);
		expect(mayPage({ items: Array.from({ length: 30 }, (_, i) => item(`tt${i}`)) })).toBe(true);
		expect(mayPage({ items: [item('tt1')] })).toBe(false);
	});
	it('adds only items the shelf does not show, each once', () => {
		expect(newItems([item('tt1')], [item('tt1'), item('tt2'), item('tt2')]).map((i) => i.id)).toEqual(['tt2']);
		expect(newItems([], null)).toEqual([]);
	});
	it('jumps to the next hundred after a page that repeated', () => {
		expect(nextHundred(30)).toBe(100);
		expect(nextHundred(100)).toBe(200);
	});
	it('finds the title page of an IMDb card through the lookup of its kind', () => {
		expect(imdbLookupPath({ kind: 'movie', imdb_id: 'tt0111161' })).toBe('/titles/lookup?term=imdb%3Att0111161');
		expect(imdbLookupPath({ kind: 'show', imdb_id: 'tt0944947' })).toBe('/shows/lookup?term=imdb%3Att0944947');
	});
});
