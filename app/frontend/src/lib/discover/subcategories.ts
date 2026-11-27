import { api } from '#lib/core/api.ts';
import { to } from '#lib/core/paths.ts';
import { detailPath, remember } from '#lib/metadata/index.ts';
import { cardTmdb, type ExploreCard, type ProviderItem } from '#lib/ui/title-cards.ts';
import type { ExploreType } from './explore-type.ts';

export interface CategorySummary {
	id: string;
	name: string;
	section: string;
	section_title: string;
	movie: boolean;
	show: boolean;
	featured?: boolean;
	parent_genres?: string[];
	aliases?: string[];
}

export interface CategoryBrowseAnswer {
	type: string;
	id: string;
	name: string;
	section: string;
	items: ProviderItem[];
}

export interface CategorySearchAnswer {
	query: { text: string; plain: boolean };
	categories?: CategorySummary[];
	category_id?: string;
	category_name?: string;
	category_items?: ProviderItem[];
}

const SUBCAT_TTL_MS = 60_000;
const subcatCache = new Map<string, { at: number; items: CategorySummary[] }>();

export function categoryTitleHref(c: ExploreCard): string {
	const id = cardTmdb(c);
	if (!id || (c.kind !== 'movie' && c.kind !== 'show')) return to('/explore');
	remember(c.kind, id, { name: c.name, year: c.year, poster: c.image, backdrop: c.wide });
	return to(detailPath(c.kind, id));
}

export function subcatSwitch(type: ExploreType | string): string {
	if (type === 'movies' || type === 'movie') return 'movies';
	if (type === 'shows' || type === 'show') return 'shows';
	if (type === 'anime') return 'anime';
	return 'all';
}

export async function fetchSubcategories(
	type: ExploreType | string,
	genre = '',
	q = '',
	signal?: AbortSignal
): Promise<CategorySummary[]> {
	const sw = subcatSwitch(type);
	const g = genre.trim();
	const query = q.trim();
	const key = `${sw}|${g.toLowerCase()}|${query.toLowerCase()}`;
	const hit = subcatCache.get(key);
	if (hit && Date.now() - hit.at < SUBCAT_TTL_MS) {
		return hit.items;
	}
	const params = new URLSearchParams({ type: sw });
	if (g) params.set('genre', g);
	if (query) params.set('q', query);
	const res = await api.get<{ items?: CategorySummary[] }>(`/explore/subcategories?${params.toString()}`, signal);
	const items = res.items ?? [];
	if (subcatCache.size >= 48) {
		const oldest = subcatCache.keys().next().value;
		if (oldest) subcatCache.delete(oldest);
	}
	subcatCache.set(key, { at: Date.now(), items });
	return items;
}

export async function fetchCategoryBrowse(
	id: string,
	type: ExploreType | string = 'all',
	signal?: AbortSignal
): Promise<CategoryBrowseAnswer> {
	const sw = subcatSwitch(type);
	const params = new URLSearchParams({ type: sw, id: id.trim() });
	return api.get<CategoryBrowseAnswer>(`/explore/browse/category?${params.toString()}`, signal);
}

export async function fetchCategorySearch(
	query: string,
	type: ExploreType | string = 'all',
	signal?: AbortSignal
): Promise<CategorySearchAnswer> {
	const params = new URLSearchParams({ q: query.trim() });
	const sw = subcatSwitch(type);
	if (sw === 'movies' || sw === 'shows') params.set('type', sw);
	return api.get<CategorySearchAnswer>(`/search?${params.toString()}`, signal);
}
