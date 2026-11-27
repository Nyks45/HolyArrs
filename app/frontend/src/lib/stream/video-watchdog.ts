export type FreezeReason = 'no_video' | 'frozen_frames' | 'codec_error';
export type FreezeRecoveryAction = 'kick_hls' | 'kick_direct' | 'reencode_hls' | 'fallback_source';

export interface VideoWatchdogState {
	baseFrames: number | null;
	baseTime: number;
	baseWallMs: number;
	lastWallMs: number;
}

export interface VideoWatchdogSample {
	nowMs: number;
	visible: boolean;
	paused: boolean;
	ended: boolean;
	seeking: boolean;
	busy: boolean;
	readyState: number;
	playbackRate: number;
	currentTime: number;
	videoWidth: number;
	videoHeight: number;
	totalFrames: number | null;
}

export function readDecodedVideoFrames(video: HTMLVideoElement | null | undefined): number | null {
	if (!video) return null;
	if (typeof video.getVideoPlaybackQuality === 'function') {
		const q = video.getVideoPlaybackQuality();
		if (q && typeof q.totalVideoFrames === 'number' && Number.isFinite(q.totalVideoFrames) && q.totalVideoFrames >= 0) {
			return q.totalVideoFrames;
		}
	}
	const webkitCount = (video as HTMLVideoElement & { webkitDecodedFrameCount?: number }).webkitDecodedFrameCount;
	if (typeof webkitCount === 'number' && Number.isFinite(webkitCount) && webkitCount >= 0) {
		return webkitCount;
	}
	return null;
}

export function createVideoWatchdogState(): VideoWatchdogState {
	return { baseFrames: null, baseTime: 0, baseWallMs: 0, lastWallMs: 0 };
}

export function resetVideoWatchdog(st: VideoWatchdogState): void {
	st.baseFrames = null;
	st.baseTime = 0;
	st.baseWallMs = 0;
	st.lastWallMs = 0;
}

export function tickVideoWatchdog(st: VideoWatchdogState, s: VideoWatchdogSample): 'ok' | 'no_video' | 'frozen_frames' {
	if (
		!s.visible ||
		s.paused ||
		s.ended ||
		s.seeking ||
		s.busy ||
		s.readyState < 2 ||
		s.playbackRate <= 0 ||
		!Number.isFinite(s.currentTime)
	) {
		resetVideoWatchdog(st);
		return 'ok';
	}
	if (st.baseWallMs === 0 || s.currentTime < st.baseTime - 0.5 || s.nowMs - st.lastWallMs > 5000) {
		st.baseFrames = s.totalFrames;
		st.baseTime = s.currentTime;
		st.baseWallMs = s.nowMs;
		st.lastWallMs = s.nowMs;
		return 'ok';
	}
	st.lastWallMs = s.nowMs;
	const elapsedTime = s.currentTime - st.baseTime;
	const elapsedWall = s.nowMs - st.baseWallMs;

	if (s.videoWidth === 0 || s.videoHeight === 0) {
		if (elapsedTime >= 2.0 && elapsedWall >= 1800) {
			resetVideoWatchdog(st);
			return 'no_video';
		}
		return 'ok';
	}
	if (s.totalFrames === null) return 'ok';
	if (st.baseFrames === null || s.totalFrames !== st.baseFrames) {
		st.baseFrames = s.totalFrames;
		st.baseTime = s.currentTime;
		st.baseWallMs = s.nowMs;
		return 'ok';
	}
	if (elapsedTime >= 3.0 && elapsedWall >= 2800) {
		resetVideoWatchdog(st);
		return 'frozen_frames';
	}
	return 'ok';
}

export function nextFreezeRecoveryAction(opts: {
	reason: FreezeReason;
	attempts: number;
	pipelineMode: 'none' | 'direct' | 'hls';
	usingLocalHls: boolean;
	usingForceReencode: boolean;
	ffmpegAvailable: boolean;
}): FreezeRecoveryAction {
	if (opts.reason === 'no_video' || opts.reason === 'codec_error') {
		return opts.ffmpegAvailable && !opts.usingForceReencode ? 'reencode_hls' : 'fallback_source';
	}
	if (opts.attempts <= 0) {
		if (opts.pipelineMode === 'hls' && !opts.usingLocalHls) return 'kick_hls';
		if (opts.pipelineMode === 'direct') return 'kick_direct';
		return opts.ffmpegAvailable && !opts.usingForceReencode ? 'reencode_hls' : 'fallback_source';
	}
	if (opts.attempts === 1) {
		return opts.ffmpegAvailable && !opts.usingForceReencode ? 'reencode_hls' : 'fallback_source';
	}
	return 'fallback_source';
}

export function classifyDurationMismatch(kind: 'movie' | 'show', candSec: number, expectedSec: number): '' | 'warn' | 'severe' {
	if (candSec <= 0 || expectedSec < 300) return '';
	const diffMin = Math.abs(candSec - expectedSec) / 60;
	const ratio = candSec / expectedSec;
	if (kind === 'movie') {
		if ((candSec < 900 && expectedSec >= 3000) || diffMin >= 35 || ratio < 0.55 || ratio > 1.55) {
			return 'severe';
		}
		if (diffMin >= 20 || (diffMin >= 15 && (ratio < 0.78 || ratio > 1.25))) {
			return 'warn';
		}
		return '';
	}
	if ((candSec < 300 && expectedSec >= 900) || (candSec <= 630 && expectedSec >= 1800) || diffMin >= 20 || ratio < 0.50 || ratio > 1.70) {
		return 'severe';
	}
	if (diffMin >= 10 || (expectedSec <= 2100 && diffMin >= 7 && (ratio < 0.70 || ratio > 1.35))) {
		return 'warn';
	}
	return '';
}

export function durationMismatchTier(c: { duration_mismatch?: string }): 0 | 1 | 2 {
	if (c.duration_mismatch === 'severe') return 2;
	if (c.duration_mismatch === 'warn') return 1;
	return 0;
}

export function formatShortDuration(sec: number): string {
	if (!Number.isFinite(sec) || sec <= 0) return '';
	const s = Math.round(sec);
	if (s < 60) return `${s}s`;
	const totalMin = Math.round(s / 60);
	if (totalMin < 60) return `${totalMin}m`;
	const h = Math.floor(totalMin / 60);
	const m = totalMin % 60;
	return m > 0 ? `${h}h ${m}m` : `${h}h`;
}

export function checkMeasuredRuntimeMismatch(opts: {
	kind: 'movie' | 'show';
	measuredSec: number;
	expectedRuntimeMin: number;
	userSelectedCurrent: boolean;
	autoFallbackEnabled: boolean;
	autoFallbacks: number;
}): string | null {
	if (opts.userSelectedCurrent || !opts.autoFallbackEnabled || opts.autoFallbacks >= 5) return null;
	const expectedSec = (opts.expectedRuntimeMin || 0) * 60;
	if (expectedSec < 1200 || opts.measuredSec <= 30) return null;
	if (classifyDurationMismatch(opts.kind, opts.measuredSec, expectedSec) !== 'severe') return null;
	return `Wrong video length (${formatShortDuration(opts.measuredSec)} vs ${formatShortDuration(expectedSec)}). Switching source...`;
}
