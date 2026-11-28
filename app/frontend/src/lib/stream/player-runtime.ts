import { releaseAudioBoost } from './audio-boost';
import { closePlayerPiP, playerInPictureInPicture } from './player-window';
import { readDecodedVideoFrames, tickVideoWatchdog, type FreezeReason, type VideoWatchdogState } from './video-watchdog';

export function attachPlayerWindowEvents(video: HTMLVideoElement | undefined, options: {
	pauseAway: () => boolean; onFullscreen: (fullscreen: boolean) => void; onPiP: (pip: boolean) => void; onUnload: () => void;
}) {
	const pip = () => options.onPiP(playerInPictureInPicture(video));
	const visibility = () => { if (options.pauseAway() && document.hidden && !playerInPictureInPicture(video)) video?.pause(); };
	const fullscreen = () => options.onFullscreen(!!document.fullscreenElement);
	video?.addEventListener('enterpictureinpicture', pip);
	video?.addEventListener('leavepictureinpicture', pip);
	document.addEventListener('visibilitychange', visibility);
	document.addEventListener('fullscreenchange', fullscreen);
	window.addEventListener('beforeunload', options.onUnload);
	return () => {
		if (video) { closePlayerPiP(video); releaseAudioBoost(video); }
		video?.removeEventListener('enterpictureinpicture', pip);
		video?.removeEventListener('leavepictureinpicture', pip);
		document.removeEventListener('visibilitychange', visibility);
		document.removeEventListener('fullscreenchange', fullscreen);
		window.removeEventListener('beforeunload', options.onUnload);
	};
}

export function monitorVideo(watchdog: VideoWatchdogState, video: HTMLVideoElement, busy: boolean, pip: boolean, onFreeze: (reason: FreezeReason) => void) {
	const result = tickVideoWatchdog(watchdog, {
		nowMs: Date.now(), visible: document.visibilityState === 'visible' || pip,
		paused: video.paused, ended: video.ended, seeking: video.seeking, busy,
		readyState: video.readyState, playbackRate: video.playbackRate, currentTime: video.currentTime,
		videoWidth: video.videoWidth, videoHeight: video.videoHeight, totalFrames: readDecodedVideoFrames(video)
	});
	if (result !== 'ok') onFreeze(result);
}

export async function startDirectPlayback(video: HTMLVideoElement, url: string, start: number, rate: number, volume: number, muted: boolean, cancelled: () => boolean) {
	if (start > 0.5) video.addEventListener('loadedmetadata', () => { if (!cancelled()) video.currentTime = start; }, { once: true });
	video.src = url; video.playbackRate = rate; video.volume = volume; video.muted = muted;
	await video.play().catch(() => {});
}
