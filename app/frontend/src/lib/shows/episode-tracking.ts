import {
	batchWatchHistory,
	classifyAirDate,
	deleteWatchHistory,
	getTitleWatchHistory,
	saveWatchProgress,
	type EpisodeReleaseInfo,
	type EpisodeReleaseStatus,
	type WatchEntry
} from '#lib/stream/index.ts';
import { newestSeason } from './labels.ts';

export { classifyAirDate, type EpisodeReleaseInfo, type EpisodeReleaseStatus };

export interface EpProgress {
	progress_pct: number;
	completed: boolean;
	position_sec: number;
}

export interface ShowResumeTarget {
	kind: 'resume' | 'next' | 'caught_up' | 'start';
	season: number;
	episode: number;
	positionSec: number;
}

export interface SeasonEpisodeSummary {
	total: number;
	airedCount: number;
	upcomingCount: number;
	tbaCount: number;
	watchedCount: number;
	unwatchedAiredCount: number;
	allAiredWatched: boolean;
	nextUpcoming: { episode: number; badge: string; airDay?: string } | null;
}

/** Resolves the smart Watch / Resume / Next Up target for a TV show. */
export function resolveShowResumeTarget(
	seasons: { number: number; episodes: number }[],
	watchLatest: WatchEntry | null,
	watchEpisodes: WatchEntry[]
): ShowResumeTarget | null {
	const regular = [...seasons].filter((s) => s.number > 0).sort((a, b) => a.number - b.number);
	const ordered = regular.length ? regular : [...seasons].sort((a, b) => a.number - b.number);
	if (ordered.length === 0) return null;

	if (watchLatest && !watchLatest.completed && watchLatest.position_sec > 10 && watchLatest.season) {
		return {
			kind: 'resume',
			season: watchLatest.season,
			episode: watchLatest.episode || 1,
			positionSec: watchLatest.position_sec
		};
	}

	const completedList = watchEpisodes
		.filter((ep) => ep.completed && (ep.season ?? 0) > 0)
		.sort((a, b) => (b.season ?? 0) - (a.season ?? 0) || (b.episode ?? 0) - (a.episode ?? 0));

	const refCompleted =
		watchLatest?.completed && (watchLatest.season ?? 0) > 0
			? watchLatest
			: completedList[0] ?? null;

	if (refCompleted && refCompleted.season) {
		const idx = ordered.findIndex((s) => s.number === refCompleted.season);
		if (idx >= 0) {
			const curSeason = ordered[idx];
			const epNum = refCompleted.episode || 1;
			if (curSeason.episodes === 0 || epNum < curSeason.episodes) {
				return { kind: 'next', season: curSeason.number, episode: epNum + 1, positionSec: 0 };
			}
			const nextSeason = ordered.slice(idx + 1).find((s) => s.episodes > 0) ?? ordered[idx + 1];
			if (nextSeason) {
				return { kind: 'next', season: nextSeason.number, episode: 1, positionSec: 0 };
			}
			return { kind: 'caught_up', season: curSeason.number, episode: epNum, positionSec: 0 };
		}
	}

	const inProgress = [...watchEpisodes]
		.filter((ep) => !ep.completed && ep.position_sec > 10 && (ep.season ?? 0) > 0)
		.sort((a, b) => (b.season ?? 0) - (a.season ?? 0) || (b.episode ?? 0) - (a.episode ?? 0))[0];
	if (inProgress?.season) {
		return {
			kind: 'resume',
			season: inProgress.season,
			episode: inProgress.episode || 1,
			positionSec: inProgress.position_sec
		};
	}

	const firstWithEps = ordered.find((s) => s.episodes > 0) ?? ordered[0];
	return { kind: 'start', season: firstWithEps.number, episode: 1, positionSec: 0 };
}

/** Picks the season to expand first on the show page: active watched season when history exists, else newest. */
export function pickDefaultSeason(
	seasons: { number: number; episodes: number }[],
	watchLatest: WatchEntry | null,
	watchEpisodes: WatchEntry[]
): number | null {
	if (watchLatest || watchEpisodes.length > 0) {
		const target = resolveShowResumeTarget(seasons, watchLatest, watchEpisodes);
		if (target && seasons.some((s) => s.number === target.season)) {
			return target.season;
		}
	}
	return newestSeason(seasons) ?? null;
}

/** Summarizes aired vs upcoming counts and watch completion for one season's episode list. */
export function summarizeSeasonEpisodes(
	episodes: { number: number; air_day?: string }[],
	seasonNumber: number,
	progress: Record<string, EpProgress>,
	now = new Date()
): SeasonEpisodeSummary {
	const sorted = [...episodes].sort((a, b) => a.number - b.number);
	let airedCount = 0;
	let upcomingCount = 0;
	let tbaCount = 0;
	let watchedCount = 0;
	let nextUpcomingDated: { episode: number; badge: string; airDay?: string } | null = null;
	let firstTba: { episode: number; badge: string; airDay?: string } | null = null;

	for (const ep of sorted) {
		const rel = classifyAirDate(ep.air_day, now);
		if (rel.isReleased) {
			airedCount++;
			if (progress[`${seasonNumber}:${ep.number}`]?.completed) {
				watchedCount++;
			}
		} else if (rel.status === 'upcoming') {
			upcomingCount++;
			if (!nextUpcomingDated) {
				nextUpcomingDated = { episode: ep.number, badge: rel.badge, airDay: ep.air_day };
			}
		} else {
			tbaCount++;
			if (!firstTba) {
				firstTba = { episode: ep.number, badge: rel.badge, airDay: ep.air_day };
			}
		}
	}

	const unwatchedAiredCount = Math.max(0, airedCount - watchedCount);
	return {
		total: episodes.length,
		airedCount,
		upcomingCount,
		tbaCount,
		watchedCount,
		unwatchedAiredCount,
		allAiredWatched: airedCount > 0 && watchedCount === airedCount,
		nextUpcoming: nextUpcomingDated ?? firstTba
	};
}

/** Builds the canonical backend WatchKey for one show episode. */
export function episodeWatchKey(tmdb: string, season: number, episode: number): string {
	return `show:${tmdb.trim()}:s${season}e${episode}`;
}

export interface ShowWatchMeta {
	tmdb: string;
	title: string;
	year?: number;
	imdb?: string;
	poster?: string | null;
	backdrop?: string | null;
}

/** Builds a completed WatchEntry payload for manual episode/season watched toggles. */
export function buildWatchedEntry(
	meta: ShowWatchMeta,
	season: number,
	episode: number,
	epName?: string
): WatchEntry {
	return {
		key: episodeWatchKey(meta.tmdb, season, episode),
		kind: 'show',
		tmdb: meta.tmdb,
		imdb: meta.imdb,
		title: meta.title,
		year: meta.year,
		poster: meta.poster ?? undefined,
		backdrop: meta.backdrop ?? undefined,
		season,
		episode,
		episode_name: epName,
		position_sec: 0,
		duration_sec: 0,
		progress_pct: 100,
		completed: true,
		updated_at: new Date().toISOString()
	};
}

/** Converts backend WatchEntry list into a season:episode lookup map. */
export function toEpisodeProgressMap(episodes: WatchEntry[]): Record<string, EpProgress> {
	const map: Record<string, EpProgress> = {};
	for (const ep of episodes) {
		map[`${ep.season ?? 0}:${ep.episode ?? 0}`] = {
			progress_pct: ep.progress_pct ?? 0,
			completed: !!ep.completed,
			position_sec: ep.position_sec
		};
	}
	return map;
}

/** Applies optimistic episode watched/unwatched updates and recomputes watchLatest. */
export function applyWatchTrackingChange(
	episodes: WatchEntry[],
	upserts: WatchEntry[],
	removeTargets: { season: number; episode: number }[]
): { episodes: WatchEntry[]; latest: WatchEntry | null } {
	const drop = new Set(removeTargets.map((r) => `${r.season}:${r.episode}`));
	for (const u of upserts) {
		drop.add(`${u.season ?? 0}:${u.episode ?? 0}`);
	}
	const kept = episodes.filter((ep) => !drop.has(`${ep.season ?? 0}:${ep.episode ?? 0}`));
	const next = [...kept, ...upserts].sort(
		(a, b) => (a.season ?? 0) - (b.season ?? 0) || (a.episode ?? 0) - (b.episode ?? 0)
	);
	let latest: WatchEntry | null = null;
	if (upserts.length > 0) {
		latest = upserts[upserts.length - 1];
	} else {
		for (const ep of next) {
			if (!latest || (ep.updated_at ?? '') >= (latest.updated_at ?? '')) {
				latest = ep;
			}
		}
	}
	return { episodes: next, latest };
}

/** Optimistically toggles one episode watched state and syncs with backend (rolling back on error). */
export async function syncEpisodeWatchedState(
	meta: ShowWatchMeta,
	currentEpisodes: WatchEntry[],
	season: number,
	episode: number,
	epName: string,
	watched: boolean,
	onUpdate: (episodes: WatchEntry[], latest: WatchEntry | null) => void
): Promise<void> {
	const entry = buildWatchedEntry(meta, season, episode, epName);
	const next = watched
		? applyWatchTrackingChange(currentEpisodes, [entry], [])
		: applyWatchTrackingChange(currentEpisodes, [], [{ season, episode }]);
	onUpdate(next.episodes, next.latest);
	try {
		if (watched) await saveWatchProgress(entry);
		else await deleteWatchHistory(episodeWatchKey(meta.tmdb, season, episode));
	} catch {
		const res = await getTitleWatchHistory('show', meta.tmdb).catch(() => null);
		if (res) onUpdate(res.episodes ?? [], res.latest);
	}
}

/** Optimistically toggles an entire released season's watched state in one batch request. */
export async function syncSeasonWatchedState(
	meta: ShowWatchMeta,
	currentEpisodes: WatchEntry[],
	season: number,
	seasonEpisodes: { number: number; name?: string }[],
	watched: boolean,
	onUpdate: (episodes: WatchEntry[], latest: WatchEntry | null) => void
): Promise<void> {
	if (!seasonEpisodes.length) return;
	const upserts = watched ? seasonEpisodes.map((ep) => buildWatchedEntry(meta, season, ep.number, ep.name)) : [];
	const delKeys = watched ? [] : seasonEpisodes.map((ep) => episodeWatchKey(meta.tmdb, season, ep.number));
	const next = applyWatchTrackingChange(
		currentEpisodes,
		upserts,
		watched ? [] : seasonEpisodes.map((ep) => ({ season, episode: ep.number }))
	);
	onUpdate(next.episodes, next.latest);
	try {
		await batchWatchHistory({ upserts, delete_keys: delKeys });
	} catch {
		const res = await getTitleWatchHistory('show', meta.tmdb).catch(() => null);
		if (res) onUpdate(res.episodes ?? [], res.latest);
	}
}
