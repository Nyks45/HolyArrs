import type { SkipSegment, StreamPrefs } from './streaming-config-api';

function autoSkip(segment: SkipSegment, prefs: StreamPrefs | null) {
	if (segment.kind === 'intro') return prefs?.auto_skip_intro;
	if (segment.kind === 'recap') return prefs?.auto_skip_recap;
	if (segment.kind === 'outro') return prefs?.auto_skip_outro;
	return false;
}

export function createPlayerCountdowns(options: {
	activeSkip: () => SkipSegment | null; prefs: () => StreamPrefs | null; episodeKey: () => string;
	paused: () => boolean; nearEpisodeEnd: () => boolean; next: () => void; skip: (segment: SkipSegment) => void;
}) {
	let skipSeconds = $state(0);
	let nextSeconds = $state(0);
	let dismissed = $state('');
	const identity = (seg: SkipSegment) => `${options.episodeKey()}:${seg.kind}:${seg.start}`;
	let consumedNext = '';
	$effect(() => {
		if (!options.nearEpisodeEnd() || options.paused()) { nextSeconds = 0; return; }
		const episode = options.episodeKey();
		if (consumedNext === episode) return;
		nextSeconds = 10;
		const tick = setInterval(() => {
			nextSeconds--;
			if (nextSeconds > 0) return;
			clearInterval(tick);
			consumedNext = episode;
			options.next();
		}, 1000);
		return () => clearInterval(tick);
	});
	$effect(() => {
		const segment = options.activeSkip();
		if (!segment || options.paused() || !autoSkip(segment, options.prefs()) || dismissed === identity(segment)) {
			skipSeconds = 0; return;
		}
		skipSeconds = 3;
		const tick = setInterval(() => {
			skipSeconds--;
			if (skipSeconds > 0) return;
			clearInterval(tick);
			options.skip(segment);
		}, 1000);
		return () => clearInterval(tick);
	});
	return {
		get skipSeconds() { return skipSeconds; },
		get nextSeconds() { return nextSeconds; },
		cancel(segment: SkipSegment | null) { if (segment) dismissed = identity(segment); }
	};
}
