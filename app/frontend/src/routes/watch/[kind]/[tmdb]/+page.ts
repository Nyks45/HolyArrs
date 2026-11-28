import { redirect, type Load } from '@sveltejs/kit';
import { api } from '#lib/core/api.ts';
import { to } from '#lib/core/paths.ts';
import { session } from '#lib/core/session.svelte.ts';
import { detailPath, loadDetails } from '#lib/metadata/index.ts';
import { classifyAirDate, extractRegularSeasons, findNextEpisodeTarget, type SeasonEp } from '#lib/stream/index.ts';

// Resolve a Continue Watching link before any player frame is shown.
export const load: Load = async ({ params, url }) => {
	const tmdb = params.tmdb;
	if (!tmdb || params.kind !== 'show' || session.kid || url.searchParams.get('next') !== '1') return;
	const season = Number(url.searchParams.get('s'));
	const episode = Number(url.searchParams.get('e'));
	const next = await nextAiredEpisode(tmdb, season, episode).catch(() => null);
	if (!next) redirect(307, to(detailPath('show', tmdb)));
	const target = new URL(url);
	target.searchParams.delete('next');
	target.searchParams.set('s', String(next.season));
	target.searchParams.set('e', String(next.episode));
	target.searchParams.set('resume', '1');
	redirect(307, target.pathname + target.search + target.hash);
};

async function nextAiredEpisode(tmdb: string, season: number, episode: number) {
	if (!Number.isSafeInteger(season) || season < 0 || !Number.isSafeInteger(episode) || episode < 1) return null;
	const episodes = async (n: number) => {
		const r = await api.get<{ episodes?: SeasonEp[] }>(`/metadata/show/${encodeURIComponent(tmdb)}/seasons/${n}`);
		return r.episodes ?? [];
	};
	const current = await episodes(season);
	let next = findNextEpisodeTarget('show', season, episode, current, []);
	if (!next) {
		const details = await loadDetails(api.get, 'show', tmdb);
		next = findNextEpisodeTarget('show', season, episode, current, extractRegularSeasons(details));
	}
	if (!next) return null;
	const available = next.season === season ? current : await episodes(next.season);
	const nextEp = available.find((ep) => ep.number === next.episode);
	return nextEp && classifyAirDate(nextEp.air_day).isReleased ? next : null;
}
