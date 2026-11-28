import { canSeekInLocalHlsBuffer, isTimeBuffered, shouldAcceptTimeUpdate, shouldFallbackDirectSeekToTranscode } from './player-media';

interface TimelineContext {
	video?: HTMLVideoElement;
	mode: 'none' | 'direct' | 'hls';
	active: boolean;
	paused: boolean;
	switching: boolean;
	reseeking: boolean;
	canTranscode: boolean;
	localHls: boolean;
	offset: number;
	pos: number;
	duration: number;
	videoDuration: number;
}

export function createPlayerTimeline(read: () => TimelineContext, options: {
	restart: (at: number, status: string) => void; feedback: (text: string) => void; landed: () => void; userSeek?: () => void;
}) {
	const state = $state({ videoTime: 0, bufferedEnd: 0, pendingSeekTarget: null as number | null, stalled: false, seamlessStatus: '', isScrubbing: false, scrubValue: 0 });
	let wait: ReturnType<typeof setTimeout> | undefined;

	function clearStall(keepSeamless = false) {
		clearTimeout(wait);
		wait = undefined;
		state.stalled = false;
		if (!keepSeamless) { state.seamlessStatus = ''; state.pendingSeekTarget = null; }
	}
	function onVideoWaiting() {
		const { mode, switching } = read();
		if (mode === 'none' || switching) return;
		clearTimeout(wait);
		wait = setTimeout(() => {
			const next = read();
			if (!next.paused && next.mode !== 'none' && !next.switching) state.stalled = true;
		}, 1200);
	}
	function onVideoTimeUpdate() {
		const { video, mode, offset, switching, reseeking } = read();
		if (!video) return;
		if (video.buffered.length) state.bufferedEnd = video.buffered.end(video.buffered.length - 1);
		const actualPos = mode === 'hls' ? offset + video.currentTime : video.currentTime;
		const result = shouldAcceptTimeUpdate({ switchingSource: switching, reseekingHls: reseeking, videoSeeking: video.seeking, pendingSeekTarget: state.pendingSeekTarget, actualPos });
		if (result === 'ignore') return;
		state.videoTime = video.currentTime;
		if (result === 'landed') { clearStall(); options.landed(); } else clearStall(true);
	}
	function onVideoSeeked() {
		const { video, switching, reseeking, mode, canTranscode, videoDuration } = read();
		if (switching || reseeking || !video || video.seeking) return;
		const seekableEnd = video.seekable.length ? video.seekable.end(video.seekable.length - 1) : 0;
		const target = state.pendingSeekTarget;
		if (target !== null && shouldFallbackDirectSeekToTranscode({ pipelineMode: mode, ffmpegAvailable: canTranscode, pendingSeekTarget: target, videoTime: video.currentTime, videoDuration, seekableEnd })) {
			options.restart(videoDuration > 0 ? Math.min(target, videoDuration) : target, 'Seeking...');
			return;
		}
		state.videoTime = video.currentTime;
		clearStall();
	}
	function commitSeek(target: number) {
		state.isScrubbing = false;
		const { video, active, mode, offset, duration, localHls } = read();
		if (!active || !video) return;
		options.userSeek?.();
		const at = Math.max(0, duration > 0 ? Math.min(duration, target) : target);
		const actual = mode === 'hls' ? offset + state.videoTime : state.videoTime;
		if (Math.abs(at - actual) < 0.25 && !video.seeking) { clearStall(); return; }
		state.pendingSeekTarget = at;
		if (mode === 'direct' || (mode === 'hls' && !localHls)) {
			if (!isTimeBuffered(video.buffered, at)) state.seamlessStatus = 'Seeking...';
			video.currentTime = state.videoTime = at;
		} else if (mode === 'hls' && canSeekInLocalHlsBuffer(video, offset, at, state.bufferedEnd)) {
			video.currentTime = state.videoTime = Math.max(0, at - offset);
		} else options.restart(at, 'Jumping...');
	}
	function seekBy(delta: number) {
		const { active, mode, duration, pos } = read();
		if (!active || mode === 'none') return;
		const anchor = state.pendingSeekTarget ?? pos;
		const target = Math.max(0, Math.min(duration || anchor + delta, anchor + delta));
		options.feedback(`${delta > 0 ? '+' : ''}${delta}s`);
		commitSeek(target);
	}

	return { state, clearStall, onVideoWaiting, onVideoTimeUpdate, onVideoSeeked, commitSeek, seekBy,
		scrub: (seconds: number) => { state.isScrubbing = true; state.scrubValue = seconds; } };
}
