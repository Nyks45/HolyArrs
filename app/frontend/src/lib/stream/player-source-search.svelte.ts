import { message } from '#lib/core/api.ts';
import { countCandidateKinds, mergeCandidateLists } from './player-media';
import { searchStreamReleases, type StreamCandidate, type StreamSearchReq } from './stream-api';

export function createPlayerSourceSearch(onFull: (candidates: StreamCandidate[]) => void) {
	let request = 0;
	const state = $state({
		candidates: [] as StreamCandidate[], counts: countCandidateKinds([]), searching: false, loadingMore: false, error: ''
	});

	async function run(query: StreamSearchReq, noCache: boolean) {
		const attempt = ++request;
		state.searching = true; state.loadingMore = false; state.error = '';
		try {
			const fast = await searchStreamReleases({ ...query, mode: 'fast', no_cache: noCache });
			if (attempt !== request) return;
			state.candidates = fast.candidates ?? [];
			state.counts = fast.counts ?? countCandidateKinds(state.candidates);
		} catch (error) {
			if (attempt === request) { state.error = message(error); state.candidates = []; state.counts = countCandidateKinds([]); }
		} finally { if (attempt === request) state.searching = false; }

		if (attempt !== request) return;
		state.loadingMore = true;
		void searchStreamReleases({ ...query, mode: 'full', no_cache: noCache }).then((full) => {
			if (attempt !== request) return;
			state.candidates = mergeCandidateLists(state.candidates, full.candidates ?? []);
			state.counts = countCandidateKinds(state.candidates);
			onFull(state.candidates);
		}).catch((error) => {
			if (attempt === request && !state.candidates.length) state.error = message(error);
		}).finally(() => { if (attempt === request) state.loadingMore = false; });
	}
	return { state, run, cancel: () => { request++; } };
}
