// Dedicated Explore content category hubs (Anime, K-Drama, Turkish, C-Drama & J-Drama, etc.).
import { discoverCard, type ExploreCard, type ExploreRow } from '#lib/ui/title-cards.ts';
import { serverRow, type ServerRow } from './explore-server.ts';
import { addTailRows } from './explore-tail.svelte.ts';
import type { ExploreType } from './explore-type.ts';
import { searchAddress } from './search-address.ts';
import { page } from '$app/state';
import { goto } from '$app/navigation';
import { api } from '#lib/core/api.ts';

export interface ExploreHubInfo {
	id: string;
	name: string;
	tagline: string;
	group: string;
	accent: string;
}

interface HubAnswer {
	hub: ExploreHubInfo;
	hero?: Parameters<typeof discoverCard>[0][];
	rows: ServerRow[];
	/** The hub's rows go on past these (page 1 on). */
	more?: boolean;
}

export const DEFAULT_HUBS: ExploreHubInfo[] = [
	{ id: 'anime', name: 'Anime', tagline: 'Japanese anime shows, movies, shonen, isekai and classics', group: 'spotlight', accent: 'rose' },
	{ id: 'kdrama', name: 'K-Drama', tagline: 'Korean dramas, romance, thrillers and Korean cinema', group: 'regional', accent: 'violet' },
	{ id: 'turkish', name: 'Turkish', tagline: 'Turkish dizi, dramas, classics and Turkish cinema', group: 'regional', accent: 'red' },
	{ id: 'asian-drama', name: 'C-Drama & J-Drama', tagline: 'Chinese, Japanese and Thai live-action dramas and films', group: 'regional', accent: 'amber' },
	{ id: 'animation', name: 'Cartoons & Animation', tagline: 'Western animated films, shows and family animation', group: 'format', accent: 'cyan' },
	{ id: 'documentary', name: 'Documentaries', tagline: 'Documentary films, docushows, true crime and nature', group: 'format', accent: 'emerald' },
	{ id: 'indian', name: 'Bollywood & Indian', tagline: 'Hindi, Tamil and Telugu cinema and Indian web shows', group: 'regional', accent: 'orange' },
	{ id: 'scifi', name: 'Sci-Fi & Fantasy', tagline: 'Space epics, cyberpunk, dystopia and fantasy worlds', group: 'genre', accent: 'indigo' },
	{ id: 'horror', name: 'Horror & Thriller', tagline: 'Supernatural horror, psychological thrillers and suspense', group: 'genre', accent: 'crimson' },
	{ id: 'action', name: 'Action & Adventure', tagline: 'Blockbusters, martial arts, heist and spy thrillers', group: 'genre', accent: 'blue' },
	{ id: 'comedy', name: 'Comedy', tagline: 'Stand-out comedies, sitcoms and romantic comedies', group: 'genre', accent: 'yellow' },
	{ id: 'crime', name: 'Crime & Mystery', tagline: 'Gritty crime dramas, whodunits and noir', group: 'genre', accent: 'slate' }
];

export function createExploreHub(ctx: {
	gone: AbortController;
	get heroAt(): number;
	set heroAt(v: number);
	get activeType(): ExploreType;
	loadHead: () => Promise<void>;
}) {
	const here = () => searchAddress(page.url, page.shallow?.url, typeof location === 'undefined' ? undefined : location.href);
	const rawHubId = $derived((here().searchParams.get('hub') ?? '').trim().toLowerCase());
	const hubId = $derived(ctx.activeType === 'anime' ? 'anime' : rawHubId);

	let hubs = $state<ExploreHubInfo[]>(DEFAULT_HUBS);
	const barHubs = $derived(hubs.filter((h) => h.id !== 'anime'));
	let hubInfo = $state<ExploreHubInfo | null>(null);
	let hubHero = $state<ExploreCard[]>([]);
	let hubRows = $state<(ExploreRow & { wide: boolean; type: string })[]>([]);
	let hubLoaded = $state<string>('');
	let hubFailed = $state(false);
	let hubAsk = 0;
	// The hub's own endless rows past its first answer.
	let tailRows = $state<(ExploreRow & { wide: boolean; type: string })[]>([]);
	let tailMore = $state(false);
	let tailNext = 1;
	let tailBusy = false;

	async function loadHubsList() {
		try {
			const res = await api.get<{ hubs?: ExploreHubInfo[] }>('/explore/hubs', ctx.gone.signal);
			if (res.hubs?.length) hubs = res.hubs;
		} catch {
			/* Keep default hub list if server request fails. */
		}
	}

	async function loadHub(id: string) {
		if (!id) return;
		const n = ++hubAsk;
		hubFailed = false;
		try {
			const res = await api.get<HubAnswer>(`/explore/hub?hub=${encodeURIComponent(id)}`, ctx.gone.signal);
			if (n !== hubAsk) return;
			hubInfo = res.hub ?? hubs.find((h) => h.id === id) ?? null;
			hubHero = (res.hero ?? []).map((d) => ({ ...discoverCard(d, 'hero'), wide: d.backdrop ?? null }));
			hubRows = (res.rows ?? []).map((r) => ({ ...serverRow(r), keep: true }));
			tailRows = [];
			tailNext = 1;
			tailMore = !!res.more;
			hubLoaded = id;
			ctx.heroAt = 0;
		} catch {
			if (n === hubAsk) {
				hubFailed = true;
				hubLoaded = id;
			}
		}
	}

	/** Reads the hub's next page of rows (the Anime tab goes on as it scrolls); says whether it read one. */
	async function loadTail(): Promise<boolean> {
		const id = hubId;
		if (tailBusy || !tailMore || !id || hubLoaded !== id) return false;
		tailBusy = true;
		const n = hubAsk;
		try {
			const res = await api.get<HubAnswer>(`/explore/hub?hub=${encodeURIComponent(id)}&page=${tailNext}`, ctx.gone.signal);
			if (n !== hubAsk) return false;
			tailRows = addTailRows(hubRows, tailRows, (res.rows ?? []).map((r) => ({ ...serverRow(r), keep: true })));
			tailNext++;
			tailMore = !!res.more;
			return true;
		} catch {
			if (n === hubAsk) tailMore = false;
			return false;
		} finally {
			tailBusy = false;
		}
	}

	$effect(() => {
		const id = hubId;
		if (!id) {
			hubInfo = null;
			hubHero = [];
			hubRows = [];
			tailRows = [];
			tailMore = false;
			hubFailed = false;
			return;
		}
		if (hubLoaded !== id || hubFailed) {
			void loadHub(id);
		}
	});

	function pickHub(id: string | null) {
		const next = id && id === hubId ? null : id;
		const url = new URL(here().href);
		url.searchParams.delete('genre');
		if (!next) {
			url.searchParams.delete('hub');
		} else {
			url.searchParams.set('hub', next);
		}
		void goto(url, { shallow: true, reset: false });
		scrollTo({ top: 0, behavior: 'instant' });
		if (!next) {
			void ctx.loadHead();
		}
	}

	const hubOn = $derived(!!hubId);
	const loadingHub = $derived(hubOn && hubLoaded !== hubId && !hubFailed);
	const activeMeta = $derived(hubInfo ?? hubs.find((h) => h.id === hubId) ?? null);

	return {
		get hubs() {
			return hubs;
		},
		get barHubs() {
			return barHubs;
		},
		get hubId() {
			return hubId;
		},
		get hubOn() {
			return hubOn;
		},
		get loading() {
			return loadingHub;
		},
		get info() {
			return activeMeta;
		},
		get hero() {
			return hubHero;
		},
		get rows() {
			return tailRows.length ? [...hubRows, ...tailRows] : hubRows;
		},
		get tailMore() {
			return tailMore;
		},
		/** The ids of the rows read past the hub's own. */
		get tailIds() {
			return tailRows.map((r) => r.id);
		},
		loadTail,
		get failed() {
			return hubFailed;
		},
		pickHub,
		reload: () => loadHub(hubId),
		loadHubsList
	};
}
