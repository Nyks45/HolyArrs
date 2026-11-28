import { api } from '../core/api.ts';
import type { LibraryFolder, LookupMovie, LookupShow, PickRulesView, TitleView } from '../core/types.ts';
import { loadChoices, needsFolder, startChoices, type TitleDefaults } from '../mediafiles/index.ts';
import type { ShowSettings } from '../shows/index.ts';

export interface InstantChoices {
	folders: LibraryFolder[];
	rules: PickRulesView[];
	defaults: TitleDefaults | null;
	showDefaults?: ShowSettings | null;
	gateDefault?: string;
	handsOff?: boolean;
}

/** The body for a one-click add with the saved defaults, or null when a folder must be picked by hand first. Adding only saves the title; Download starts the search. */
export function instantBody(kind: 'movie' | 'show', item: LookupMovie | LookupShow, c: InstantChoices): Record<string, unknown> | null {
	const start = startChoices(c.defaults, c.folders, c.rules, kind);
	const kindFolders = c.folders.filter((f) => f.kind === kind);
	if (!start.folderId && needsFolder(kindFolders.length, !!c.handsOff)) return null;
	const common = { follow: true, library_folder_id: start.folderId, pick_rules_id: start.rulesId, keep_up_to_date: start.keep, search_on_add: false, more_versions: [] };
	if (kind === 'movie') {
		const last = loadChoices();
		return { id: `tmdb:${item.ids.tmdb}`, ...common, release_gate: last.release_gate || c.gateDefault || 'released' };
	}
	const s = item as LookupShow;
	const d = c.showDefaults;
	const seasons = d?.season_choice ?? 'all';
	let id = '';
	for (const k of [s.source, 'tmdb', 'tvdb', 'tvmaze', 'imdb']) if (k && s.ids[k]) { id = `${k}:${s.ids[k]}`; break; }
	return { id, ...common, seasons, new_seasons: seasons === 'future' ? 'all' : (d?.new_seasons ?? 'all'), episode_order: d?.episode_order ?? 'aired', details_source: s.source || d?.details_source || 'tmdb' };
}

/** Adds a title at once, Netflix style. Returns null when the add needs the full sheet (no usable folder). */
export async function addInstantly(kind: 'movie' | 'show', item: LookupMovie | LookupShow, c: InstantChoices): Promise<TitleView | null> {
	const body = instantBody(kind, item, c);
	if (!body) return null;
	const r = await api.post<{ title: TitleView }>(kind === 'movie' ? '/titles' : '/shows', body);
	return r.title;
}
