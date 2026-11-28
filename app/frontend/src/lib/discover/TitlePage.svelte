<script lang="ts">
	import PluginCards from '#lib/ui/PluginCards.svelte';
	import { titleSections, type PluginCard } from '#lib/ui/plugin-cards.ts';
	import { t, appLocale } from '#lib/core/i18n/i18n.svelte.ts';
	import '#lib/ui/title-page.css';
	import PageLoader from '#lib/ui/PageLoader.svelte';
	import { richMetadata, runtimeWords, type RichMeta } from '#lib/ui/TitleHero.svelte';
	import ActionButton from '#lib/ui/ActionButton.svelte';
	import Shelf from '#lib/ui/Shelf.svelte';
	import TitleRows from '#lib/ui/TitleRows.svelte';
	import FollowButton from '#lib/ui/FollowButton.svelte';
	import { readInfo, type InfoDetails } from '#lib/ui/title-info.ts';
	import { defaultRegion, serviceRegion } from '#lib/ui/region.ts';
	import { tmdbArtName } from '#lib/ui/title-cards.ts';
	import { loadExtras, type OutsideScore } from '#lib/ui/outside-ratings.ts';
	import { ratingPicks, shownRatings } from '#lib/ui/rating-picks.svelte.ts';
	import { loadWikiFacts } from '#lib/ui/wiki-facts.ts';
	import ActionMenu from '#lib/ui/ActionMenu.svelte';
	import PersonCircle from '#lib/ui/PersonCircle.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import LoadError from '#lib/ui/LoadError.svelte';
	import Sheet from '#lib/ui/Sheet.svelte';
	import { loadAskedBy, type AskedByAnswer } from '#lib/ui/asked-by.ts';
	import AskedByLine from '#lib/ui/AskedByLine.svelte';
	import { audienceOf, titleStatus } from '#lib/ui/title-status.ts';
	import TitleScores from './TitleScores.svelte';
	import TitleTrailers from './TitleTrailers.svelte';
	import TitleShowcase from './TitleShowcase.svelte';
	import { createOwnedTitle } from './title-owned.svelte.ts';
	import { NoDownloaderNote } from '#lib/downloads/index.ts';
	import { WatchLinks } from '#lib/mediaservers/index.ts';
	import TitleLibrary from './TitleLibrary.svelte';
	import TitleOwnedSheets from './TitleOwnedSheets.svelte';
	import { addInstantly, SearchOutcomeLine, keptSeasons, completeSeasons, seasonsToAsk, QuickAsk, AddMovieDialog, AddShowDialog, AskShowDialog, AskMovieDialog, fourKReady } from '#lib/requests/index.ts';
	import { pickDefaultSeason, resolveShowResumeTarget, seasonWords, SeasonFolds, syncEpisodeWatchedState, syncSeasonWatchedState, toEpisodeProgressMap } from '#lib/shows/index.ts';
	import { detailPath, fourKOffer, isDetailKind, loadDetails, remembered, type TitleDetails } from '#lib/metadata/index.ts';
	import type { GateDefaults } from '#lib/mediafiles/index.ts';
	import type { ShowSettings } from '#lib/shows/index.ts';
	import { savedDefaults } from '#lib/mediafiles/index.ts';
	import { goto, replaceState, snapshot } from '$app/navigation';
	import { page } from '$app/state';
	import { untrack } from 'svelte';
	import { to } from '#lib/core/paths.ts';
	import { api, message, ApiError } from '#lib/core/api.ts';
	import { session } from '#lib/core/session.svelte.ts';
	import { toasts } from '#lib/core/toasts.svelte.ts';
	import { refreshInbox } from '#lib/core/inbox.svelte.ts';
	import { preloadHero } from '#lib/core/tmdb-image.ts';
	import type { Ask, LibraryFolder, LookupMovie, PickRulesView, TitleView } from '#lib/core/types.ts';
	import type { LookupShow } from '#lib/core/types.ts';
	import { bytes, ago } from '#lib/core/format.ts';
	import { live } from '#lib/core/live.svelte.ts';
	import { date } from '#lib/core/format.ts';
	import type { MenuItem } from '#lib/ui/Menu.svelte';
	import HideSheet from './HideSheet.svelte';
	import CheckReleasesSheet from './CheckReleasesSheet.svelte';
	import { formatDuration, getTitleWatchHistory, prefetchStreamSources, type WatchEntry } from '#lib/stream/index.ts';
	import { chosenToast } from '#lib/mediafiles/index.ts';
	import type { Choice, ReleaseView } from '#lib/core/types.ts';
	import { hiddenPill, type Hide } from './hidden.ts';


	// The one title page for a movie or show, wherever it is opened from (/title/<kind>/<tmdb> and /library/<id> both render it). Not in the Library: Add (people who manage it) or Ask (members). In the Library: the same page with its Library tab.
	let { kind, tmdb, libraryId, backHref }: { kind: string; tmdb: string; libraryId?: number; backHref?: string } = $props();
	let details = $state<TitleDetails | null>(null);
	// Back from a person (or any page opened here) lands where the page was. Rows below the hero load late, so the scroll is restored as they arrive.
	snapshot<number>({
		capture: () => Math.round(scrollY),
		restore: (y) => {
			if (!y) return;
			const until = Date.now() + 4000;
			const put = () => {
				if (document.documentElement.scrollHeight >= y + innerHeight) return scrollTo({ top: y, behavior: 'instant' });
				if (Date.now() < until) requestAnimationFrame(put);
				else scrollTo({ top: y, behavior: 'instant' });
			};
			put();
			for (const ms of [300, 900]) setTimeout(() => Math.abs(scrollY - y) > 4 && document.documentElement.scrollHeight >= y + innerHeight && scrollTo({ top: y, behavior: 'instant' }), ms);
		}
	});
	let loading = $state(true);
	let error = $state('');
	let missing = $state(false);
	// A Kid account asked for a title above its age-rating limit (403).
	let blocked = $state(false);

	const own = createOwnedTitle();
	const manage = $derived(own.manage);
	const owned = $derived(own.owned);
	const ownState = $derived(own.ownState);
	const ownBadges = $derived(own.ownBadges);
	const ownTab = $derived(own.ownTab);
	const ownTabs = $derived(own.ownTabs);
	const ownedFailed = $derived(own.ownedFailed);
	const moreMenu = $derived(own.moreMenu);
	const mayReport = $derived(own.mayReport);
	// Hidden from browsing: owners and admins hide a title or its genre, and see the hides that cover it.
	const mayHide = $derived(session.can('users'));
	let hideOpen = $state(false);
	let people = $state<ReadonlyMap<number, string>>(new Map());
	let peopleAsked = false;
	function loadPeople() {
		if (peopleAsked || !mayHide) return;
		peopleAsked = true;
		api.get<{ users: { id: number; name: string }[] }>('/users').then(
			(r) => (people = new Map(r.users.map((u) => [u.id, u.name]))),
			() => (peopleAsked = false)
		);
	}
	// Fetched with the page so the Hide sheet opens with its For choice already there.
	$effect(() => {
		if (mayHide) loadPeople();
	});
	const hideItem = $derived<MenuItem>({ label: t('discover.hidden.menu'), icon: 'eyeOff', onselect: () => { loadPeople(); hideOpen = true; } });
	const titleMenu = $derived<MenuItem[]>(mayHide ? [...moreMenu, ...(moreMenu.length ? [{ kind: 'separator' as const }] : []), hideItem] : moreMenu);
	const hiddenShown = (d: TitleDetails) => mayHide && !!(d.hides?.length || d.hidden);
	function hidesChanged(next: Hide[]) {
		if (!details) return;
		details = { ...details, hides: next, hidden: next.some((h) => !h.user_id || h.user_id === session.s.id) };
	}
	// The short facts about the copy you have: quality, size, kept up to date.
	let rules = $state<PickRulesView[]>([]);
	let folders = $state<LibraryFolder[]>([]);
	// Who asked, for the hero byline.
	let askedBy = $state<AskedByAnswer>({ titles: {} });
	$effect(() => {
		const id = owned?.id;
		if (!id) return;
		loadAskedBy(api.get, [id]).then((x) => (askedBy = x), () => {});
	});
	const askers = $derived(owned ? (askedBy.titles[String(owned.id)]?.askers ?? []) : []);
	const mayAsk = $derived(session.can('ask') && !manage);
	// A show in the Library can still be asked for the seasons it does not keep up to date.
	const kept = $derived(owned ? keptSeasons(owned) : []);
	const complete = $derived(owned ? completeSeasons(owned) : []);
	const askable = $derived(owned ? seasonsToAsk(details?.show?.seasons, [...kept, ...complete]) : []);
	// The 4K request button shows only when 4K quality is set up for movies.
	let fourKMovies = $state(false);
	$effect(() => {
		if (mayAsk) void fourKReady().then((r) => (fourKMovies = r.movie));
	});
	// Phones share one row between the 4K, Follow and Trailer buttons: the 4K caption shortens to "4K" so it is never cut to "Req…".
	// Choose a release does the same: a pill reading "Releases" on phones, where a round icon has no hover label.
	let phone = $state(false);
	$effect(() => {
		const mq = matchMedia('(max-width: 767px)');
		const set = () => (phone = mq.matches);
		set();
		mq.addEventListener('change', set);
		return () => mq.removeEventListener('change', set);
	});
	let gateDefault = $state('released');
	let showDefaults = $state<ShowSettings | null>(null);
	let addMovie: ReturnType<typeof AddMovieDialog> | undefined = $state();
	let addShow: ReturnType<typeof AddShowDialog> | undefined = $state();
	let askShow: ReturnType<typeof AskShowDialog> | undefined = $state();
	// Cast and Similar are one row each; See all opens the whole list.
	let allCast = $state(false);
	let watchLatest = $state<WatchEntry | null>(null);
	let watchEpisodes = $state<WatchEntry[]>([]);
	const showExt = $derived((kind === 'movie' ? details?.movie?.ids : details?.show?.ids) as Record<string, string | undefined> | undefined);
	const watchEpisodesByKey = $derived(toEpisodeProgressMap(watchEpisodes));
	const watchMeta = $derived(details?.name ? { tmdb, title: details.name, year: details.year, imdb: details.imdb ?? showExt?.imdb, poster: details.poster, backdrop: details.backdrop } : null);
	function watchHref(resume: boolean, s?: number, e?: number, drawer?: 'sources'): string {
		const back = detailPath(kind === 'show' ? 'show' : 'movie', tmdb);
		const extra = drawer ? `&drawer=${drawer}` : '';
		if (kind === 'show') {
			const ss = s ?? resumeTarget?.season ?? seasons.find((x) => x.number > 0)?.number ?? 1;
			const ee = e ?? resumeTarget?.episode ?? 1;
			return to(`/watch/show/${encodeURIComponent(tmdb)}?s=${ss}&e=${ee}&resume=${resume ? '1' : '0'}&from=${encodeURIComponent(back)}${extra}`);
		}
		return to(`/watch/movie/${encodeURIComponent(tmdb)}?resume=${resume ? '1' : '0'}&from=${encodeURIComponent(back)}${extra}`);
	}
	const applyHistory = (eps: WatchEntry[], lat: WatchEntry | null) => { watchEpisodes = eps; watchLatest = lat; };
	const toggleEpisodeWatched = (s: number, e: number, epName: string, watched: boolean) => watchMeta && kind === 'show' ? syncEpisodeWatchedState(watchMeta, watchEpisodes, s, e, epName, watched, applyHistory) : undefined;
	const markSeasonWatched = (s: number, eps: { number: number; name?: string }[], watched: boolean) => watchMeta && kind === 'show' ? syncSeasonWatchedState(watchMeta, watchEpisodes, s, eps, watched, applyHistory) : undefined;
	$effect(() => {
		const k = kind;
		const id = tmdb;
		watchLatest = null;
		watchEpisodes = [];
		if (k !== 'movie' && k !== 'show') return;
		if (!id || session.kid) return;
		let alive = true;
		void getTitleWatchHistory(k, id).then(
			(res) => {
				if (!alive) return;
				watchLatest = res.latest;
				watchEpisodes = res.episodes ?? [];
			},
			() => {}
		);
		return () => {
			alive = false;
		};
	});
	let askMovieDialog: ReturnType<typeof AskMovieDialog> | undefined = $state();
	let loadedSettings = false;
	async function load(k: string, id: string) {
		if (!isDetailKind(k) || !id) {
			missing = true;
			loading = false;
			return;
		}
		const first = remembered(k, id);
		// The hero backdrop starts loading before the details answer arrives.
		preloadHero(first?.backdrop);
		details = first ? { kind: k, tmdb: id, name: '', genres: [], cast: [], similar: [], ...first } : null;
		loading = true;
		error = '';
		missing = false;
		blocked = false;
		// Opened from the Library: its record loads beside the details, not after them.
		if (libraryId && own.ownedFor !== libraryId) void own.loadOwned(libraryId);
		try {
			// The metadata answer draws the hero at once; the Library and ask facts follow.
			const d = await loadDetails(api.get, k, id, undefined, (m) => {
				if (k !== kind || id !== tmdb) return;
				preloadHero(m.backdrop);
				details = { ...m, titleId: m.titleId ?? details?.titleId };
			});
			if (k !== kind || id !== tmdb) return;
			preloadHero(d?.backdrop);
			if (!d) {
				missing = !details;
				return;
			}
			details = d;
			const ownId = d.titleId ?? libraryId;
			if (!ownId) {
				own.owned = null;
				own.ownedFor = 0;
			} else if (own.ownedFor !== ownId) {
				own.owned = null;
				void own.loadOwned(ownId);
			}
		} catch (e) {
			if (e instanceof ApiError && e.status === 403) {
				blocked = true;
				details = null;
			} else error = message(e);
		} finally {
			loading = false;
		}
	}
	// Load only when the title itself changes. A live update hands over the same kind and TMDB id, and reloading then would drop open sheets and scroll.
	let loadedKey = '';
	$effect(() => {
		const k = kind, id = tmdb;
		if (`${k}/${id}` === loadedKey) return;
		loadedKey = `${k}/${id}`;
		untrack(() => {
			void load(k, id);
			allCast = false;
		});
	});
	let openedKey = '';
	$effect(() => {
		if (!details?.name || (kind !== 'movie' && kind !== 'show')) return;
		const k = `${kind}:${tmdb}`;
		if (openedKey === k) return;
		openedKey = k;
		void api.post('/explore/opened', { kind, tmdb_id: tmdb, name: details.name, year: details.year ?? 0 }).catch(() => {});
	});
	$effect(() => {
		if (session.kid || !details?.name || (kind !== 'movie' && kind !== 'show')) return;
		const ss = kind === 'show' ? (resumeTarget?.season ?? seasons.find((s) => s.number > 0)?.number ?? 1) : 0;
		const ee = kind === 'show' ? (resumeTarget?.episode ?? 1) : 0;
		const req = { query: details.name, kind: kind as 'movie' | 'show', year: details.year || 0, season: ss, episode: ee, imdb: details.imdb ?? showExt?.imdb ?? '', tmdb: Number(tmdb) || 0, original_title: details.original_title, genres: details.genres, original_language: details.original_language, origin_country: details.origin_country, runtime_min: details.runtime || 0 };
		const timer = setTimeout(() => prefetchStreamSources(req), 500);
		return () => clearTimeout(timer);
	});
	// The Library folders and Pick rules the add sheets offer; a failed load shows in the sheet with Try again.
	let choicesLoaded = $state(false);
	let choicesFailed = $state(false);
	function loadAddChoices() {
		choicesFailed = false;
		Promise.all([
			api.get<{ library_folders: LibraryFolder[] }>('/library-folders').then((r) => (folders = r.library_folders)),
			api.get<{ pick_rules: PickRulesView[] }>('/pick-rules').then((r) => (rules = r.pick_rules))
		]).then(
			() => (choicesLoaded = true),
			() => ((choicesFailed = true), (choicesLoaded = true))
		);
	}
	$effect(() => {
		if (!manage || loadedSettings) return;
		loadedSettings = true;
		loadAddChoices();
		api.get<GateDefaults>('/settings/release-gate').then((r) => (gateDefault = r.release_gate)).catch(() => {});
		api.get<ShowSettings>('/settings/shows').then((r) => (showDefaults = r)).catch(() => {});
		// The add dialogs wait for the saved defaults, fetched once per visit.
		savedDefaults.forget();
		void savedDefaults.load();
	});
	// Check releases: the release picked before the movie was added; it is sent once the add sheet has added it.
	let checkOpen = $state(false);
	let checked: { checkId: number; release: ReleaseView } | null = null;
	function checkPicked(r: ReleaseView, rulesId: number, checkId: number) {
		checked = { checkId, release: r };
		addMovie?.open(details?.movie as LookupMovie, { choose: true, rulesId });
	}
	async function added(title: TitleView, choose = false) {
		toasts.push(t('discover.titlePage.added2', { name: title.name }), 'ok', { href: `/library/${title.id}` });
		const pick = choose ? checked : null;
		checked = null;
		if (pick) {
			// Send the checked release; if that fails, the release list on the Library page offers the rest.
			try {
				chosenToast(await api.post<Choice>(`/releases/check/${pick.checkId}/pick`, { release_id: pick.release.id, title_id: title.id, force: pick.release.needs_force }));
				return void goto(to(`/library/${title.id}`));
			} catch (e) {
				toasts.push(t('mediafiles.releaseSheet.sendFailed'), 'fail', { body: message(e) });
				await api.post(`/titles/${title.id}/search`, { choose: false }).catch(() => {});
				return void goto(to(`/library/${title.id}?pick`));
			}
		}
		// Choose a release: start the pick-it-yourself search, then open the release list on the Library page.
		if (choose) await api.post(`/titles/${title.id}/search`, { choose: false }).catch((e) => toasts.push(t('mediafiles.title.startFailed'), 'fail', { body: message(e) }));
		void goto(to(`/library/${title.id}${choose ? '?pick' : ''}`));
	}
	/** Add all put this page's own movie in the Library: the page shows it as owned, without leaving. */
	function ownAdded(id: number) {
		if (details) details = { ...details, titleId: id };
		if (own.ownedFor !== id) void own.loadOwned(id);
	}
	// One click, Netflix style: saved defaults, no sheet, no page change. The sheet opens only when a folder must be picked.
	let instantBusy = $state(false);
	async function instantAdd(d: TitleDetails) {
		const item = d.kind === 'movie' ? d.movie : d.show;
		if (!item || instantBusy) return;
		instantBusy = true;
		try {
			for (let i = 0; i < 50 && !choicesLoaded; i++) await new Promise((r) => setTimeout(r, 100));
			const title = await addInstantly(d.kind, item, { folders, rules, defaults: await savedDefaults.load(), showDefaults, gateDefault, handsOff: session.s.handsOff });
			if (!title) return void (d.kind === 'movie' ? addMovie?.open(item as LookupMovie) : addShow?.open(item as LookupShow));
			ownAdded(title.id);
			toasts.push(t('discover.titlePage.addedToWatchlist', { name: title.name }), 'ok');
		} catch (e) {
			toasts.push(t('requests.cards.couldNotAdd'), 'fail', { body: message(e) });
		} finally {
			instantBusy = false;
		}
	}
	function movieAsked(r: { ask: Ask; joined: boolean; already: boolean }) {
		const brief = { id: r.ask.id, state: r.ask.state, state_label: r.ask.state_label, mine: true, people: r.ask.people.length };
		if (details) details = r.ask.four_k ? { ...details, ask4k: brief } : { ...details, ask: brief };
		if (r.ask.four_k) toasts.push(t('discover.titlePage.requested4k', { name: r.ask.name }), 'ok', { body: r.joined ? t('discover.titlePage.onRequestToo') : t('discover.titlePage.waitingForApproval') });
		else if (r.ask.state === 'approved') toasts.push(t('discover.titlePage.approved', { name: r.ask.name }), 'ok', { body: t('discover.titlePage.searchingForRelease') });
		else if (r.joined) toasts.push(t('discover.titlePage.alreadyRequested', { name: r.ask.name }), 'ok', { body: t('discover.titlePage.onRequestToo') });
		else toasts.push(t('discover.titlePage.requested', { name: r.ask.name }), 'ok', { body: t('discover.titlePage.waitingForApproval') });
		void refreshInbox();
	}
	function showAsked(s: LookupShow, r: { ask: Ask; joined: boolean; already: boolean }) {
		if (details) details = { ...details, ask: { id: r.ask.id, state: r.ask.state, state_label: r.ask.state_label, mine: true, people: r.ask.people.length, seasons: r.ask.seasons } };
		const what = `${r.ask.name} (${seasonWords(r.ask.seasons)})`;
		if (r.ask.state === 'approved') toasts.push(t('discover.titlePage.approved', { name: what }), 'ok', { body: t('discover.titlePage.searchingForRelease') });
		else if (r.joined) toasts.push(t('discover.titlePage.alreadyRequested', { name: what }), 'ok', { body: t('discover.titlePage.onRequestToo') });
		else toasts.push(t('discover.titlePage.requested', { name: what }), 'ok', { body: t('discover.titlePage.waitingForApproval') });
		void refreshInbox();
	}
	const kindWord = $derived(kind === 'show' ? t('discover.titlePage.tvShows') : kind === 'movie' ? t('discover.titlePage.movies') : t('discover.titlePage.notFound'));
	// Other apps and old links say "tv" or "series" for a show and "film" for a movie; go to the one address this page uses.
	const KIND_ALIASES: Record<string, string> = { tv: 'show', series: 'show', shows: 'show', film: 'movie', movies: 'movie' };
	$effect(() => {
		const alias = KIND_ALIASES[kind.toLowerCase()];
		if (alias && typeof location !== 'undefined') void goto(location.pathname.replace(`/title/${kind}/`, `/title/${alias}/`) + location.search, { replaceState: true });
	});
	// The logo and age rating, when the metadata endpoint has them (cached, shared with loadDetails).
	let rich = $state<RichMeta | null>(null);
	$effect(() => {
		const k = kind, id = tmdb;
		rich = null;
		if (!isDetailKind(k) || !id) return;
		let live = true;
		void richMetadata(api.get, k, id).then((m) => live && (rich = m));
		return () => (live = false);
	});
	// One status vocabulary (title-status.ts) for the ask on this title.
	const heroPill = (d: TitleDetails) => titleStatus({ ask: d.ask?.state, mine: d.ask?.mine, neverAdd: d.neverAdd, titleKind: d.kind }, audienceOf(session.can));
	const scoreOf = (r?: string) => (r && /\/\s*10$/.test(r) ? Number.parseFloat(r) : undefined);
	const certOf = (r?: string) => (r && !/\/\s*10$/.test(r) ? r : undefined);
	// The details answer read by the rows (seasons for a show).
	let info = $state<InfoDetails | null>(null);
	// The fact column needs these details on first view, before the Recommendations tab is opened.
	$effect(() => {
		const k = kind, id = tmdb;
		info = null;
		if (!id || !isDetailKind(k)) return;
		const ctl = new AbortController();
		api.get<Record<string, unknown>>(`/metadata/${k}/${encodeURIComponent(id)}?region=${encodeURIComponent(serviceRegion(defaultRegion()))}`, ctl.signal)
			.then((raw) => (info = readInfo(k, id, raw)))
			.catch(() => {});
		return () => ctl.abort();
	});
	const seasons = $derived((info?.tmdb === details?.tmdb ? info?.seasons : undefined) ?? details?.seasons ?? []);
	const resumeTarget = $derived(kind === 'show' ? resolveShowResumeTarget(seasons, watchLatest, watchEpisodes) : null);
	const defaultSeason = $derived(kind === 'show' ? pickDefaultSeason(seasons, watchLatest, watchEpisodes) : null);
	const regular = $derived(seasons.filter((s) => s.number > 0).length);
	const networkOf = (d: TitleDetails) => d.network ?? d.show?.network ?? '';
	// The hero says what it is (year, length or seasons, genres, age rating, scores); the details under the story say only what the hero does not.
	const personHref = (id?: string) => (id ? to(`/people/${id.replace(/^tmdb:/, '')}`) : undefined);
	/** The director (movies) or creators (shows). */
	const makers = $derived((info?.crew ?? []).filter((p) => p.role === (kind === 'show' ? 'creator' : 'director')));
	const madeBy = $derived(kind === 'show' ? t('discover.titlePage.createdBy') : t('discover.titlePage.directedBy'));
	const stars = $derived((details?.cast ?? []).slice(0, 3).map((p) => ({ name: p.name, id: p.id })));
	const heroFacts = (d: TitleDetails) =>
		[d.year ? String(d.year) : '', d.kind === 'show' && regular ? `${regular} ${regular === 1 ? 'season' : 'seasons'}` : d.kind === 'movie' ? runtimeWords(d.runtime) : '', ...d.genres.slice(0, d.kind === 'show' ? 2 : 3)].filter(Boolean);
	const langName = (code?: string) => {
		if (!code) return '';
		try {
			return new Intl.DisplayNames(['en'], { type: 'language' }).of(code) ?? code;
		} catch {
			return code;
		}
	};
	const countryName = (code: string) => {
		try {
			return new Intl.DisplayNames(['en'], { type: 'region' }).of(code) ?? code;
		} catch {
			return code;
		}
	};
	const lang = $derived(langName(info?.originalLanguage));
	const countries = $derived((info?.originCountry ?? []).map(countryName));
	const network = $derived(details ? networkOf(details) : '');
	// The scores sit under the story, not in the hero.
	let outside = $state<OutsideScore[]>([]);
	$effect(() => {
		const k = kind, id = tmdb;
		outside = [];
		if (!isDetailKind(k) || !id) return;
		let live = true;
		void loadExtras(api.get, k, id).then((x) => live && (outside = x.scores));
		return () => (live = false);
	});
	// Only the ratings this browser picked (Settings > Appearance).
	const picked = $derived(shownRatings(outside, rich?.score ?? scoreOf(details?.rating), ratingPicks.list, !ratingPicks.custom));
	const hasScores = $derived(picked.scores.length > 0 || !!picked.tmdb);
	// The second row of facts: dates, where the title stands, studio, money, awards, original title.
	const fmtDay = (d?: string) => (d ? new Date(`${d}T00:00:00Z`).toLocaleDateString(appLocale(), { month: 'short', day: 'numeric', year: 'numeric', timeZone: 'UTC' }) : '');
	const money = (n?: number) => (n ? new Intl.NumberFormat(appLocale('en-US'), { style: 'currency', currency: 'USD', notation: 'compact', maximumFractionDigits: 1 }).format(n) : '');
	const statusWord = (s?: string) => (!s || s === 'Released' ? '' : s.charAt(0) + s.slice(1).toLowerCase());
	// Award badges ("3 Oscars"), in the hero or under it with the facts line.
	let awardBadges = $state<string[]>([]);
	$effect(() => {
		const k = kind, id = tmdb;
		awardBadges = [];
		if (!isDetailKind(k) || !id) return;
		let live = true;
		void loadWikiFacts(api.get, k, id).then((f) => live && (awardBadges = f.badges.slice(0, 2)));
		return () => (live = false);
	});
	const topScores = $derived(picked.scores);
	const topTmdb = $derived(picked.tmdb);
	const tags = $derived((info?.keywords ?? []).slice(0, 10));
	const cert = $derived(details ? (rich?.certification ?? certOf(details.rating) ?? '') : '');
	const atvCols = $derived.by(() => {
		if (!details) return [];
		const d = details;
		const i = info;
		const row = (label: string, value: string | number | undefined | null) => (value ? [{ label, value: String(value) }] : []);
		const ended = i?.status === 'Ended' || i?.status === 'Canceled';
		const eps = seasons.filter((x) => x.number > 0).reduce((n, x) => n + (x.episodes ?? 0), 0);
		const information = [
			...row(network ? t('discover.titlePage.network') : i?.studios.length ? t('discover.titlePage.studio') : '', network || i?.studios.slice(0, 2).join(', ')),
			...row(d.kind === 'show' ? t('discover.titlePage.aired') : t('discover.titlePage.released'), d.kind === 'show' ? (i?.dates.firstAir ? `${i.dates.firstAir.slice(0, 4)}–${ended && i.dates.lastAir ? i.dates.lastAir.slice(0, 4) : t('discover.titlePage.now')}` : d.year) : i?.dates.theaters ? fmtDay(i.dates.theaters) : d.year),
			...row(d.kind === 'show' ? t('discover.titlePage.seasons') : t('discover.titlePage.runTime'), d.kind === 'show' ? (regular ? t('discover.titlePage.seasonsEpisodes', { regular, eps }) : '') : runtimeWords(d.runtime)),
			...row(t('discover.titlePage.rated'), cert),
			...row(t('discover.titlePage.status'), d.kind === 'show' ? statusWord(i?.status) || i?.status : ''),
			...row(countries.length > 1 ? t('discover.titlePage.regionsOfOrigin') : t('discover.titlePage.regionOfOrigin'), countries.join(', '))
		].filter((r) => r.label);
		const languages = [
			...row(t('discover.titlePage.originalLanguage'), lang),
			...row(t('discover.titlePage.spokenLanguages'), (i?.spokenLanguages.length ?? 0) > 1 ? i?.spokenLanguages.join(', ') : ''),
			...row(t('discover.titlePage.originalTitle'), i?.originalTitle),
			// The other names searches and file matching also use.
			...row(t('discover.titlePage.otherNames'), (owned?.other_names ?? []).filter((n) => n !== i?.originalTitle && n !== d.name).slice(0, 5).join(' · '))
		];
		const release = [
			...row(t('discover.titlePage.digital'), i?.dates.digital ? fmtDay(i.dates.digital) : ''),
			...row(t('discover.titlePage.onDisc'), i?.dates.physical ? fmtDay(i.dates.physical) : ''),
			...row(t('discover.titlePage.nextEpisode'), i?.nextEpisode ? `${fmtDay(i.nextEpisode.airDate)} · S${i.nextEpisode.season} E${i.nextEpisode.episode}` : ''),
			...row(t('discover.titlePage.budget'), money(i?.budget)),
			...row(t('discover.titlePage.boxOffice'), money(i?.revenue))
		];
		return [
			{ title: t('discover.titlePage.information'), rows: information },
			{ title: t('discover.titlePage.languages'), rows: languages },
			{ title: d.kind === 'show' ? t('discover.titlePage.schedule') : t('discover.titlePage.releaseBoxOffice'), rows: release }
		].filter((c) => c.rows.length);
	});
	const outLinks = $derived.by(() => {
		if (!details) return [];
		const d = details;
		const ids = (d.kind === 'movie' ? d.movie?.ids : d.show?.ids) as Record<string, string | undefined> | undefined;
		return [
			ids?.imdb ? { name: 'IMDb', href: `https://www.imdb.com/title/${ids.imdb}/` } : null,
			{ name: 'TMDB', href: `https://www.themoviedb.org/${d.kind === 'show' ? 'tv' : 'movie'}/${d.tmdb}` },
			{ name: 'Trakt', href: `https://trakt.tv/search/tmdb/${d.tmdb}?id_type=${d.kind}` },
			d.kind === 'movie' ? { name: 'Letterboxd', href: `https://letterboxd.com/tmdb/${d.tmdb}/` } : null
		].filter((x): x is { name: string; href: string } => !!x);
	});
	const crumbs = $derived(details ? [{ href: to('/explore'), label: t('discover.titlePage.explore') }, { href: to(detailPath(details.kind, details.tmdb)), label: details.year ? `${details.name} (${details.year})` : details.name }] : []);
	// The breadcrumb's parent for a page opened fresh is the title's section (Movies or TV shows). From the Library grid it goes back to the grid with its filters (backHref).
	const sectionCrumb = $derived(details?.kind === 'show' ? { label: t('discover.titlePage.tvShows'), href: backHref ?? to('/library?kind=show') } : { label: t('discover.titlePage.movies'), href: backHref ?? to('/library?kind=movie') });
	// Title page sections from running plugins, for titles in the Library: plain data drawn here.
	let pluginSections = $state<PluginCard[]>([]);
	$effect(() => {
		const id = owned?.id;
		pluginSections = [];
		if (id) void titleSections(id).then((c) => (pluginSections = c));
	});
</script>

<svelte:head><title>{details?.name ? `${details.name} · HolyArrs` : `${kindWord} · HolyArrs`}</title></svelte:head>

{#if blocked}
	<EmptyState icon="shield" title={t('discover.titlePage.thisTitleIsNot')} action={{ label: t('discover.titlePage.backToExplore'), href: to('/explore') }} />
{:else if missing}
	<EmptyState icon="search" title={t('discover.titlePage.weCouldNotFind')} text={t('discover.titlePage.itMayHaveBeen')} action={{ label: t('discover.titlePage.search'), href: to('/search') }} />
{:else if error && !details}
	<LoadError hasData={false} message={t('discover.titlePage.couldNotLoadThis', { error })} onretry={() => void load(kind, tmdb)} />
{:else}
<PageLoader loading={!details || !details.name} label={t('discover.titlePage.loadingTheTitle')} onretry={() => void load(kind, tmdb)}>
{#if details}
	{@const d = details}
	<article class="tp" data-testid="title-detail">
		<TitleShowcase
			kind={d.kind}
			tmdb={d.tmdb}
			name={d.name}
			originalName={info?.originalTitle || d.original_title}
			poster={d.poster}
			backdrop={d.backdrop}
			logo={rich?.logo}
			genres={d.genres}
			overview={d.overview}
			facts={[{ label: 'Type', value: d.kind === 'show' ? 'TV' : 'Movie' }, ...(makers.length ? [{ label: madeBy, value: makers.slice(0, 2).map((p) => p.name).join(', ') }] : []), ...(stars.length ? [{ label: t('discover.titlePage.starring'), value: stars.slice(0, 3).map((p) => p.name).join(', ') }] : []), ...atvCols.flatMap((c) => c.rows).filter((r) => r.label !== t('discover.titlePage.otherNames')), ...(awardBadges.length ? [{ label: 'Awards', value: awardBadges.join(' · ') }] : [])]}
			themes={tags.slice(0, 6)}
			studios={network ? [network] : (info?.studios.slice(0, 3) ?? [])}
			languages={info?.spokenLanguages.length ? info.spokenLanguages.slice(0, 4) : lang ? [lang] : []}
			videos={rich?.videos ?? []}
			nextAir={d.kind === 'show' ? (info?.nextEpisode ?? null) : null}
			pill={hiddenShown(d) ? hiddenPill(d.hides ?? [], people) : d.neverAdd || d.ask ? heroPill(d).label : ''}
			showTabs={!(owned && ownTab !== 'overview')}
			episodes={d.kind === 'show' ? seasonGrid : undefined}
		>
			{#snippet byline()}{#if owned}<AskedByLine {askers} />{#if ownState.kind !== 'have' && ownState.kind !== 'downloading'}<SearchOutcomeLine titleId={owned.id} />{/if}{/if}{/snippet}
			{#snippet actions()}
				{#if !session.kid && d.kind === 'show' && resumeTarget}
					{@const seTag = `S${String(resumeTarget.season).padStart(2, '0')} E${String(resumeTarget.episode).padStart(2, '0')}`}
					{#if resumeTarget.kind === 'resume'}
						<ActionButton icon="play" primary label={`Resume ${d.name} ${seTag}`} caption={`Resume ${seTag} · ${formatDuration(resumeTarget.positionSec)}`} href={watchHref(true, resumeTarget.season, resumeTarget.episode)} data-testid="hero-resume" />
						<ActionButton icon="source" pill label={`Pick stream source for ${d.name}`} caption="Sources" href={watchHref(false, resumeTarget.season, resumeTarget.episode, 'sources')} data-testid="hero-stream" />
					{:else if resumeTarget.kind === 'next'}
						<ActionButton icon="play" primary label={`Next Up ${d.name} ${seTag}`} caption={`Next Up · ${seTag}`} href={watchHref(false, resumeTarget.season, resumeTarget.episode)} data-testid="hero-next-up" />
						<ActionButton icon="source" pill label={`Pick stream source for ${d.name}`} caption="Sources" href={watchHref(false, resumeTarget.season, resumeTarget.episode, 'sources')} data-testid="hero-stream" />
					{:else}
						<ActionButton icon="play" primary label={`Watch ${d.name} ${seTag}`} caption={`Watch ${seTag}`} href={watchHref(false, resumeTarget.season, resumeTarget.episode)} data-testid="hero-stream" />
						<ActionButton icon="source" pill label={`Pick stream source for ${d.name}`} caption="Sources" href={watchHref(false, resumeTarget.season, resumeTarget.episode, 'sources')} data-testid="hero-sources" />
					{/if}
				{:else if !session.kid && d.kind === 'movie'}
					{#if watchLatest && !watchLatest.completed && watchLatest.position_sec > 10}
						<ActionButton icon="play" primary label={`Resume ${d.name}`} caption={`Resume · ${formatDuration(watchLatest.position_sec)}`} href={watchHref(true)} data-testid="hero-resume" />
						<ActionButton icon="source" pill label={`Pick stream source for ${d.name}`} caption="Sources" href={watchHref(false, undefined, undefined, 'sources')} data-testid="hero-stream" />
					{:else}
						<ActionButton icon="play" primary label={`Watch ${d.name}`} caption="Watch" href={watchHref(false)} data-testid="hero-stream" />
						<ActionButton icon="source" pill label={`Pick stream source for ${d.name}`} caption="Sources" href={watchHref(false, undefined, undefined, 'sources')} data-testid="hero-sources" />
					{/if}
				{/if}
				{#if owned}
					<ActionButton icon={ownState.kind === 'have' ? 'check' : 'download'} pill label={manage && ownState.kind === 'missing' ? t('discover.titlePage.downloadName', { name: d.name }) : ownState.label} caption={ownState.kind === 'failed' && manage ? t('discover.titlePage.retryDownload') : ownState.kind === 'missing' && manage ? t('discover.titlePage.download') : ownState.label} onclick={() => (manage && (ownState.kind === 'failed' || ownState.kind === 'missing') ? own.ownRun('search') : own.setTab('library'))} data-testid="hero-library" />
					{#if manage && ownState.kind !== 'missing'}<WatchLinks titleId={owned.id} />{/if}
					{#if manage || mayReport || mayHide}<ActionMenu label={t('discover.titlePage.moreActions')} caption={t('discover.titlePage.more')} icon="more" items={titleMenu} />{/if}
					{#if mayAsk}{@render ask4kButton(d)}{/if}
					{#if mayAsk && d.kind === 'show' && d.show && !d.neverAdd && askable.length > 0}
						<ActionButton icon="wish" pill label={t('discover.titlePage.askFor', { name: d.name })} caption={t('discover.titlePage.request')} onclick={() => askShow?.open(d.show as LookupShow, kept, complete)} data-testid="hero-ask-seasons" />
					{/if}
				{:else if manage && !d.neverAdd}
					{#if d.kind === 'movie' && d.movie}
						<ActionButton icon="plus" pill label={t('discover.titlePage.addToTheLibrary', { name: d.name })} caption={t('discover.titlePage.addToLibrary')} busy={instantBusy} onclick={() => void instantAdd(d)} data-testid="hero-add" />
						<ActionButton icon="grid" pill={phone} label={t('discover.titleOwned.checkReleases')} caption={phone ? t('library.title.releases') : t('discover.titleOwned.checkReleases')} onclick={() => (checkOpen = true)} data-testid="hero-choose-release" />
					{:else if d.kind === 'show' && d.show}
						<ActionButton icon="plus" pill label={t('discover.titlePage.addToTheLibrary', { name: d.name })} caption={t('discover.titlePage.addToLibrary')} busy={instantBusy} onclick={() => void instantAdd(d)} data-testid="hero-add" />
					{/if}
				{:else if mayAsk && !d.neverAdd}
					{#if d.kind === 'movie'}
						<ActionButton icon="wish" label={d.ask ? t('discover.titlePage.askForToo', { name: d.name }) : t('discover.titlePage.askFor', { name: d.name })} caption={d.ask?.mine ? t('discover.titlePage.requestedCaption') : d.ask ? t('discover.titlePage.requestToo') : t('discover.titlePage.request')} primary disabled={!!d.ask?.mine} onclick={() => askMovieDialog?.open({ tmdb: d.tmdb, name: d.name, year: d.year, ask: d.ask, ask4k: d.ask4k, has4k: d.has4k })} />
						{@render ask4kButton(d)}
					{:else if d.show}
						<ActionButton icon="wish" label={d.ask ? t('discover.titlePage.askForToo', { name: d.name }) : t('discover.titlePage.askFor', { name: d.name })} caption={d.ask ? t('discover.titlePage.requestToo') : t('discover.titlePage.request')} primary onclick={() => askShow?.open(d.show as LookupShow)} />
					{/if}
				{/if}
				{#if d.kind === 'show' || d.kind === 'movie'}<FollowButton titleId={owned?.id} tmdb={d.tmdb} kind={d.kind} />{/if}
				{#if mayHide && !owned}<ActionMenu label={t('discover.titlePage.moreActions')} caption={t('discover.titlePage.more')} icon="more" items={[hideItem]} />{/if}
			{/snippet}
			{#snippet scores()}{#if topScores.length || topTmdb}<TitleScores scores={topScores} tmdb={topTmdb} variant="mono" />{/if}{/snippet}
			{#snippet library()}
				{#if !owned && ownedFailed && libraryId}<LoadError hasData={false} message={t('discover.titlePage.couldNotLoadThis2')} onretry={() => libraryId && void own.loadOwned(libraryId)} />{/if}
				{#if owned}{@render ownTabBar()}<NoDownloaderNote kind={owned.no_downloader} {manage} /><TitleLibrary {own} {folders} {rules} /><TitleOwnedSheets {own} />{/if}
			{/snippet}
			{#snippet relations()}{@render linkLine()}{@render castRow()}{@render trailers()}<PluginCards cards={pluginSections} testid="title-plugin-sections" />{/snippet}
			{#snippet recommendations()}
				<TitleRows kind={d.kind} tmdb={d.tmdb} band={false} quickAsk={QuickAsk} ownAsk={d.kind === 'movie' && d.ask?.mine ? d.ask.state : undefined} onasked={(a) => { if (details && !a.four_k) details = { ...details, ask: { id: a.id, state: a.state, state_label: a.state_label, mine: true, people: a.people.length } }; }} onadded={ownAdded} />
			{/snippet}
		</TitleShowcase>

		{#snippet ask4kButton(td: TitleDetails)}
			{@const offer = fourKMovies ? fourKOffer({ ...td, titleId: td.titleId ?? owned?.id }) : null}
			{#if offer}
				<ActionButton icon="wish" pill label={offer === 'asked' ? t('discover.titlePage.askedFor4k', { name: td.name }) : t('discover.titlePage.askFor4k', { name: td.name })} caption={phone ? '4K' : offer === 'asked' ? t('discover.titlePage.requested4kCaption') : t('discover.titlePage.request4k')} disabled={offer === 'asked'} onclick={() => askMovieDialog?.open({ tmdb: td.tmdb, name: td.name, year: td.year, ask: td.ask, ask4k: td.ask4k, fourKOnly: true, inLibrary: !!(td.titleId ?? owned?.id), has4k: td.has4k })} data-testid="hero-ask-4k" />
			{/if}
		{/snippet}
		{#snippet seasonGrid()}
					<SeasonFolds showcase tmdb={d.tmdb} {seasons} fallback={d.backdrop} showMeta={{ name: d.name, year: d.year, imdb: d.imdb ?? showExt?.imdb ?? '', original_title: d.original_title, genres: d.genres, original_language: d.original_language, origin_country: d.origin_country }} {defaultSeason} activeTarget={session.kid ? undefined : resumeTarget} progress={watchEpisodesByKey} onstream={session.kid ? undefined : (s, e) => void goto(watchHref(true, s, e))} onstreamSources={session.kid ? undefined : (s, e) => void goto(watchHref(false, s, e, 'sources'))} onToggleWatched={session.kid ? undefined : toggleEpisodeWatched} onMarkSeasonWatched={session.kid ? undefined : markSeasonWatched} />
		{/snippet}
		{#snippet castRow()}
			{#if d.cast.length}
				<Shelf title={t('discover.titlePage.castAndCrew')} item="112px" anchor="title-cast" whole onseeall={() => (allCast = true)}>
					{#each d.cast.slice(0, 12) as p, i (p.name + i)}
						<PersonCircle name={p.name} role={p.role} image={p.image} href={personHref(p.id)} />
					{/each}
				</Shelf>
			{/if}
		{/snippet}
		{#snippet trailers()}
			<TitleTrailers kind={d.kind} tmdb={d.tmdb} title={d.name} videos={rich?.videos ?? []} art={[d.backdrop, ...(rich?.backdrops ?? [])]} />
		{/snippet}

		{#snippet linkLine()}
			{#if outLinks.length}<ul class="tagchips out-links" data-testid="title-links">{#each outLinks as l (l.name)}<li><a href={l.href} target="_blank" rel="noopener noreferrer">{l.name} ↗</a></li>{/each}</ul>{/if}
		{/snippet}
		{#snippet ownTabBar()}
			<div class="own-tabs" role="tablist" data-testid="own-tabs">
				{#each ownTabs as tab (tab.v)}
					<button type="button" role="tab" aria-selected={ownTab === tab.v} tabindex={ownTab === tab.v ? 0 : -1} class:on={ownTab === tab.v} onclick={() => own.setTab(tab.v)} onkeydown={(e) => { const d = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0; if (!d) return; e.preventDefault(); const el = e.currentTarget as HTMLElement; const i = ownTabs.findIndex((x) => x.v === ownTab); const next = ownTabs[(i + d + ownTabs.length) % ownTabs.length]; own.setTab(next.v); requestAnimationFrame(() => el.parentElement?.querySelector<HTMLElement>('[aria-selected="true"]')?.focus()); }}>
						{tab.label}{#if tab.count}<span class="own-count">{tab.count}</span>{/if}{#if tab.alert}<span class="own-badge" aria-hidden="true">!</span><span class="sr-only">{t('discover.titlePage.needsAttention')}</span>{/if}
					</button>
				{/each}
			</div>
		{/snippet}
		{#if mayHide}<HideSheet bind:open={hideOpen} kind={d.kind} tmdb={d.tmdb} name={d.name} genres={d.genres} hides={d.hides ?? []} {people} onchange={hidesChanged} />{/if}
		<Sheet bind:open={allCast} title={t('discover.titlePage.castAndCrew')} size="lg">
			<ul class="all-grid people">
				{#each d.cast as p, i (p.name + i)}
					<li><PersonCircle name={p.name} role={p.role} image={p.image} href={personHref(p.id)} /></li>
				{/each}
			</ul>
		</Sheet>

	</article>
{/if}
</PageLoader>
{/if}

{#if manage}
	{#if details?.movie}<CheckReleasesSheet bind:open={checkOpen} movie={details.movie} {folders} {rules} onpick={checkPicked} />{/if}
	<AddMovieDialog bind:this={addMovie} {folders} {rules} {gateDefault} loaded={choicesLoaded} failed={choicesFailed} onretry={loadAddChoices} onadded={(_m, t, choose) => void added(t, choose)} />
	<AddShowDialog bind:this={addShow} {folders} {rules} defaults={showDefaults} loaded={choicesLoaded} failed={choicesFailed} onretry={loadAddChoices} onadded={(_s, t) => added(t)} />
{/if}
{#if mayAsk}
	<AskMovieDialog bind:this={askMovieDialog} onasked={(_m, r) => movieAsked(r)} onrefused={(_m, body) => { if (typeof body.title_id === 'number') { toasts.push(t('discover.titlePage.alreadyInLibrary', { name: details?.name ?? kindWord }), 'info'); void goto(to(`/library/${body.title_id}`)); } }} />
	<AskShowDialog bind:this={askShow} onasked={showAsked} onrefused={(_s, body) => { if (typeof body.title_id === 'number') { toasts.push(t('discover.titlePage.alreadyInLibrary', { name: details?.name ?? kindWord }), 'info'); void goto(to(`/library/${body.title_id}`)); } }} />
{/if}

<style>
	.all-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(132px, 1fr)); gap: 18px 14px; margin: 0; padding: 0; list-style: none; }
	.all-grid.people { grid-template-columns: repeat(auto-fill, minmax(104px, 1fr)); }
	.own-tabs { display: flex; gap: 6px; }
	.own-tabs button { position: relative; display: inline-flex; align-items: center; gap: 6px; height: 40px; padding: 0 14px; border: 0; background: none; color: var(--text-muted); font: 600 14px/1 var(--font-ui); cursor: pointer; }
	.own-tabs button.on { color: var(--text); }
	.own-count { color: var(--text-muted); font: 600 12px/1 var(--font-ui); font-variant-numeric: tabular-nums; }
	.own-tabs { border-bottom: 1px solid var(--line); }
	.own-tabs button { border-bottom: 2px solid transparent; transition: color var(--dur-fast) var(--ease-out), border-color var(--dur-fast) var(--ease-out); }
	.own-tabs button:hover { color: var(--text); }
	.own-tabs button.on { border-bottom-color: var(--text); }
	@keyframes own-pulse { 50% { opacity: .35; } }
	
	.own-badge { margin-left: 6px; display: inline-grid; place-items: center; width: 16px; height: 16px; border-radius: 50%; background: var(--fail); color: #fff; font-size: 11px; }
	/* Every separator is one fixed 21px lead before its item, and the row is pulled 21px left and clipped there, so a line that wraps never starts with a lone dot. */
	/* Links out sit under the facts line as small chips; the whole chip is the link, 44px tall on touch phones. */
	.tagchips.out-links li { padding: 0; min-height: 28px; color: var(--text-muted); font-size: 12px; }
	.out-links a { display: inline-flex; align-items: center; min-height: inherit; padding: 0 12px; color: inherit; text-decoration: none; }
	.tagchips.out-links li:hover { color: var(--text); border-color: var(--text-faint); }
	@media (max-width: 767px) and (pointer: coarse) { .tagchips.out-links li { min-height: 44px; } }
	.tagchips { display: flex; flex-wrap: wrap; gap: 6px; margin: 0; padding: 0; list-style: none; }
	.tagchips li { display: inline-flex; align-items: center; min-height: 30px; padding: 0 12px; border: 1px solid var(--line); border-radius: 999px; color: var(--text); font: 500 13px/1 var(--font-ui); }
</style>
