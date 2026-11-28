// The server-built Explore page: the head in one request, the tail by pages.

import { discoverCard, type ExploreCard, type ExploreRow, type ProviderItem } from '#lib/ui/title-cards.ts';
import { type ExploreType } from './explore-type.ts';

export interface ServerRow {
	id: string;
	type: string;
	title: string;
	/** A small line under the title, chosen by the server. */
	subtitle?: string;
	category?: string;
	title_layer?: string;
	wide?: boolean;
	items: ProviderItem[];
	/** The word GET /explore/row takes for this row's next page; none when it cannot page. */
	more?: string;
}

export interface HeadAnswer {
	type: ExploreType;
	building: boolean;
	seeds: number;
	tail_cap: number;
	tail_rows: number;
	/** Up to 6 titles not in the Library (from Trending now and Trending this week everywhere) for the hero. */
	hero?: ProviderItem[];
	/** The household's taste genres, strongest first (hero badges). */
	taste?: string[];
	head: ServerRow[];
}

export interface TailAnswer {
	page: number;
	more: boolean;
	rows: ServerRow[];
}

/** A cold background build can take minutes. Poll less often as it warms. */
export function poolRetryDelay(attempt: number): number | undefined {
	if (attempt >= 10) return undefined;
	return Math.min(30_000, 4_000 * 2 ** attempt);
}

export interface HeadQuery {
	type: ExploreType;
	services?: number[];
	region?: string;
	/** The viewer's time zone (IANA), so row titles fit their time of day. */
	tz?: string;
}

/** Services are sent so the server builds On your services for them; empty means none picked. */
export function headPath(q: HeadQuery): string {
	const p = headParams(q);
	if (q.services && q.region) {
		p.set('services', q.services.join('|'));
		p.set('region', q.region);
	}
	return `/explore/head?${p.toString()}`;
}

export function tailPath(q: HeadQuery, page: number): string {
	const p = headParams(q);
	if (q.region) p.set('region', q.region);
	p.set('page', String(page));
	return `/explore/tail?${p.toString()}`;
}

/** Badges state facts only: no countdowns, no "leaving soon". */
export const FACTUAL_BADGES = /^(New season|New episodes|Coming .+|In library|Asked|Today|Tomorrow|Premiere|New|4K|1080p|720p|\d+p)$/;

export function factualBadges(badges: string[]): string[] {
	return badges.filter((b) => FACTUAL_BADGES.test(b));
}

/** One server card of a row: every movie and show row draws wide title cards, so the card keeps its backdrop. */
export function serverCard(d: ProviderItem, row: string): ExploreCard {
	const c = { ...discoverCard(d, row), wide: d.backdrop ?? null };
	return { ...c, badges: factualBadges(c.badges) };
}

/** Turns a server row into an Explore row; Coming soon keeps its wide pictures. The server already left out rows under their type's minimum, so every row it sends is kept. */
export function serverRow(r: ServerRow): ExploreRow & { wide: boolean; type: string } {
	return {
		id: r.id,
		type: r.type,
		title: r.title,
		...(r.subtitle ? { subtitle: r.subtitle } : {}),
		...(r.category ? { category: r.category } : {}),
		...(r.title_layer ? { titleLayer: r.title_layer } : {}),
		wide: !!r.wide,
		...(r.more ? { more: r.more } : {}),
		cards: r.items.map((d) => serverCard(d, r.id)),
		keep: true
	};
}

function headParams(q: HeadQuery): URLSearchParams {
	const p = new URLSearchParams({ type: q.type === 'music' || q.type === 'anime' ? 'all' : q.type });
	if (q.tz) p.set('tz', q.tz);
	return p;
}
