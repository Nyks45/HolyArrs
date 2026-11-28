import { describe, expect, it } from 'vitest';
import type { ProviderItem } from '#lib/ui/title-cards.ts';
import { moreCards, newCards, rowMorePath } from './row-more.ts';
import { addTailRows } from './explore-tail.svelte.ts';

const item = (id: string, kind: 'movie' | 'show' = 'movie'): ProviderItem => ({ tmdb_id: id, kind, name: `T${id}`, backdrop: `/b${id}.jpg` }) as ProviderItem;

describe('row pages', () => {
	it('asks GET /explore/row with the row word and the page', () => {
		expect(rowMorePath('a-b_c', 2)).toBe('/explore/row?more=a-b_c&page=2');
	});

	it('makes cards in the row shape, keeping the wide picture', () => {
		const [c] = moreCards('trending', [item('7')]);
		expect(c.key).toBe('trending-movie-7');
		expect(c.wide).toBe('/b7.jpg');
		expect(moreCards('trending', null)).toEqual([]);
	});

	it('adds only cards the row does not show, each once', () => {
		const have = moreCards('r', [item('1'), item('2')]);
		const got = moreCards('r', [item('2'), item('3'), item('3'), item('3', 'show')]);
		expect(newCards(have, got).map((c) => c.key)).toEqual(['r-movie-3', 'r-show-3']);
	});
});

describe('tail rows', () => {
	it('never adds a row the head or the tail already has', () => {
		const head = [{ id: 'trending' }];
		const rows = [{ id: 'more:movie:genre:35' }];
		const got = [{ id: 'trending' }, { id: 'more:movie:genre:35' }, { id: 'more:show:genre:18' }, { id: 'more:show:genre:18' }];
		expect(addTailRows(head, rows, got).map((r) => r.id)).toEqual(['more:movie:genre:35', 'more:show:genre:18']);
	});

	it('leaves out of a new row the titles the rows right above show, while it keeps 8 cards', () => {
		const cards = (ids: string[]) => moreCards('x', ids.map((i) => item(i)));
		const above = [{ id: 'a', cards: cards(['1', '2']) }];
		const full = { id: 'b', cards: cards(['1', '3', '4', '5', '6', '7', '8', '9', '10']) };
		const thin = { id: 'c', cards: cards(['2', '11', '12']) };
		const [b, c] = addTailRows(above, [], [full, thin]);
		expect(b.cards.length).toBe(8);
		expect(c.cards.length).toBe(3);
	});
});
