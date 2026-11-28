import type { StreamCandidate, WatchEntry } from './stream-api';

export interface ResumeState {
	pipelineMode: 'none' | 'direct' | 'hls';
	pendingSeekTarget: number | null;
	currentPos: number;
	pendingStartSec: number;
	landed: boolean;
	/** Last playhead seen after the current source reached its start. */
	lastGood: number;
	saved: WatchEntry | null;
}

/** Position a replacement source should start at, so a switch or fallback keeps the viewer's place. */
export function resumeTarget(s: ResumeState): number {
	let pos: number;
	if (s.pendingSeekTarget !== null) pos = s.pendingSeekTarget;
	else if (s.landed && s.pipelineMode !== 'none') pos = s.currentPos;
	else {
		// The current source never reached its start point, so its playhead is not the viewer's place.
		const savedPos = s.saved && !s.saved.completed ? s.saved.position_sec : 0;
		pos = Math.max(s.lastGood, s.pendingStartSec, s.lastGood > 0 || s.pendingStartSec > 0 ? 0 : savedPos);
	}
	return pos > 10 ? pos : 0;
}

/** True once playback reached the requested start, so saving progress cannot overwrite a good resume point. */
export function hasLandedAt(start: number, actual: number): boolean {
	return start <= 10 || Math.abs(actual - start) <= 15;
}

/** Prefer a live search result for the saved release, which keeps its debrid or library route. */
export function matchSavedCandidate(saved: WatchEntry, candidates: StreamCandidate[]): StreamCandidate | null {
	const hash = (saved.info_hash || '').toLowerCase();
	if (hash) {
		const byHash = candidates.find((c) => (c.info_hash || '').toLowerCase() === hash && !!c.debrid_provider)
			?? candidates.find((c) => (c.info_hash || '').toLowerCase() === hash);
		if (byHash) return byHash;
	}
	const link = saved.release_link || '';
	return link ? candidates.find((c) => c.link === link) ?? null : null;
}
