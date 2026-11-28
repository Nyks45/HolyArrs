import { describe, expect, it } from 'vitest';
import type { ProviderItem } from '#lib/ui/title-cards.ts';
import { itemCard } from './search-cards.svelte.ts';
import { newResults, searchMorePath } from './search-more.ts';

const item = (id: string, kind: 'movie' | 'show' = 'movie', extra: Partial<ProviderItem> = {}): ProviderItem => ({ tmdb_id: id, kind, name: `T${id}`, year: 2020, in_library: false, ...extra }) as ProviderItem;

describe('search pages', () => {
	it('asks GET /explore/search with the kind, the words and the page', () => {
		expect(searchMorePath('show', 'the office', 2)).toBe('/explore/search?kind=show&q=the+office&page=2');
	});
	it('makes a card like the first page: a title page link, or the Library for an owned one', () => {
		expect(itemCard(item('5')).href).toContain('/movie/5');
		expect(itemCard(item('6', 'movie', { in_library: true, title_id: 9 })).href).toContain('/library/9');
	});
	it('adds only results the page does not show yet, each once', () => {
		const have = [itemCard(item('1')), itemCard(item('2', 'show'))];
		const got = newResults(have, [item('1'), item('2'), item('2', 'show'), item('3'), item('3')]);
		expect(got.map((c) => c.key)).toEqual(['m2', 'm3']);
	});
});
