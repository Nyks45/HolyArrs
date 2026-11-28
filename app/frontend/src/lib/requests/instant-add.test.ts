import { describe, expect, it } from 'vitest';
import { instantBody } from './instant-add.ts';
import type { LibraryFolder, LookupMovie, LookupShow, PickRulesView } from '../core/types.ts';

const folders = [{ id: 7, kind: 'movie', path: '/m' }, { id: 8, kind: 'show', path: '/s' }] as unknown as LibraryFolder[];
const rules = [{ id: 3, name: 'Best', kinds: [] }] as unknown as PickRulesView[];

describe('instantBody', () => {
	it('adds a movie with the usual folder and no search', () => {
		const b = instantBody('movie', { ids: { tmdb: 603 } } as unknown as LookupMovie, { folders, rules, defaults: null });
		expect(b).toMatchObject({ id: 'tmdb:603', library_folder_id: 7, search_on_add: false, follow: true });
	});
	it('adds a show by its source id with the saved season choice', () => {
		const b = instantBody('show', { source: 'tmdb', ids: { tmdb: 5920 } } as unknown as LookupShow, { folders, rules, defaults: null, showDefaults: { season_choice: 'all', episode_order: 'aired' } as never });
		expect(b).toMatchObject({ id: 'tmdb:5920', library_folder_id: 8, seasons: 'all', search_on_add: false });
	});
	it('asks for the sheet when no folder exists', () => {
		expect(instantBody('movie', { ids: { tmdb: 1 } } as unknown as LookupMovie, { folders: [], rules, defaults: null })).toBeNull();
	});
	it('needs no folder in hands-off mode', () => {
		expect(instantBody('movie', { ids: { tmdb: 1 } } as unknown as LookupMovie, { folders: [], rules, defaults: null, handsOff: true })).not.toBeNull();
	});
});
