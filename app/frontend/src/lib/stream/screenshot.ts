import { videoRect } from './ass-renderer';
import type { VideoFit } from './player-prefs';

export function screenshotName(title: string, time: number, season?: number, episode?: number): string {
	const safe = title.trim().replace(/[\\/:*?"<>|\u0000-\u001f]/g, '_').replace(/\s+/g, '_').slice(0, 120) || 'HolyArrs';
	const seconds = Math.max(0, Math.floor(Number.isFinite(time) ? time : 0));
	const ep = episode !== undefined && season !== undefined ? `_S${String(season).padStart(2, '0')}E${String(episode).padStart(2, '0')}` : '';
	return `${safe}${ep}_${String(Math.floor(seconds / 60)).padStart(2, '0')}m${String(seconds % 60).padStart(2, '0')}s.png`;
}

export interface ScreenshotOptions {
	video: HTMLVideoElement;
	processed?: () => Promise<HTMLCanvasElement | undefined>;
	captions?: HTMLCanvasElement | null;
	raw: boolean;
	withSubtitles: boolean;
	name: string;
	fit: VideoFit;
}

export async function takePlayerScreenshot(options: ScreenshotOptions): Promise<void> {
	const { video, processed, captions, raw, withSubtitles, name, fit } = options;
	if (video.readyState < 2 || !video.videoWidth || !video.videoHeight) throw new Error('Wait for the video to start before taking a screenshot.');
	const frame = (!raw ? await processed?.() : undefined) ?? video;
	const canvas = document.createElement('canvas');
	canvas.width = frame instanceof HTMLVideoElement ? frame.videoWidth : frame.width;
	canvas.height = frame instanceof HTMLVideoElement ? frame.videoHeight : frame.height;
	const ctx = canvas.getContext('2d');
	if (!ctx) throw new Error('Screenshots are unavailable in this browser.');
	ctx.drawImage(frame, 0, 0, canvas.width, canvas.height);
	if (withSubtitles && captions?.width && captions.height) {
		const rect = videoRect(captions.width, captions.height, video.videoWidth, video.videoHeight, fit);
		ctx.drawImage(captions, rect.x, rect.y, rect.width, rect.height, 0, 0, canvas.width, canvas.height);
	}
	const blob = await new Promise<Blob>((resolve, reject) => canvas.toBlob((blob) => blob ? resolve(blob) : reject(new Error('The video frame could not be captured.')), 'image/png'));
	const handle = URL.createObjectURL(blob);
	const link = document.createElement('a');
	link.href = handle;
	link.download = name;
	link.click();
	setTimeout(() => URL.revokeObjectURL(handle), 30_000);
}
