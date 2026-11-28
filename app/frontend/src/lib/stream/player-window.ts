import { videoRect } from './ass-renderer';

interface DocumentPiPWindow {
	window: Window | null;
	requestWindow(options?: { width: number; height: number }): Promise<Window>;
}
type Layer = () => HTMLCanvasElement | undefined;
interface PlayerWindowProps { processed: Layer; captions: Layer; togglePlay: () => void; changed: () => void }

const windows = new WeakMap<HTMLVideoElement, Window>();

function documentPiP(): DocumentPiPWindow | undefined {
	return (window as Window & { documentPictureInPicture?: DocumentPiPWindow }).documentPictureInPicture;
}

export function pictureInPictureAvailable(): boolean {
	return typeof document !== 'undefined' && (!!document.pictureInPictureEnabled || !!documentPiP());
}

export function playerInPictureInPicture(video?: HTMLVideoElement): boolean {
	return !!video && (document.pictureInPictureElement === video || !!windows.get(video));
}

export function closePlayerPiP(video: HTMLVideoElement) {
	windows.get(video)?.close();
}

function mirrorPlayer(child: Window, video: HTMLVideoElement, props: PlayerWindowProps) {
	const doc = child.document;
	doc.title = 'HolyArrs · Player';
	const style = doc.createElement('style');
	style.textContent = 'body{margin:0;background:#000;color:#fff;font:14px system-ui} canvas{width:100vw;height:100vh;display:block} button{position:absolute;left:8px;bottom:8px;min-width:60px;padding:8px;border:1px solid #ccc;border-radius:8px;background:#191b23;color:#fff;opacity:0} body:hover button,button:focus-visible{opacity:1}';
	doc.head.append(style);
	const frame = doc.createElement('canvas');
	const action = doc.createElement('button');
	action.type = 'button';
	const update = () => { action.textContent = video.paused ? 'Play' : 'Pause'; };
	action.addEventListener('click', props.togglePlay);
	update();
	video.addEventListener('play', update);
	video.addEventListener('pause', update);
	doc.body.append(frame, action);
	const ctx = frame.getContext('2d', { alpha: false });
	if (!ctx) throw new Error('Picture-in-picture canvas is unavailable');
	const paint = () => {
		if (child.closed) return;
		const width = child.innerWidth, height = child.innerHeight;
		if (frame.width !== width) frame.width = width;
		if (frame.height !== height) frame.height = height;
		if (video.readyState >= 2 && video.videoWidth) {
			const rect = videoRect(width, height, video.videoWidth, video.videoHeight, 'contain');
			ctx.fillStyle = '#000'; ctx.fillRect(0, 0, width, height);
			const processed = props.processed();
			ctx.drawImage(processed?.dataset.renderer === 'webgl' ? processed : video, rect.x, rect.y, rect.width, rect.height);
			const captions = props.captions();
			if (captions) {
				const src = videoRect(captions.width, captions.height, video.videoWidth, video.videoHeight, 'contain');
				ctx.drawImage(captions, src.x, src.y, src.width, src.height, rect.x, rect.y, rect.width, rect.height);
			}
		}
		child.requestAnimationFrame(paint);
	};
	paint();
	child.addEventListener('pagehide', () => {
		windows.delete(video);
		video.removeEventListener('play', update);
		video.removeEventListener('pause', update);
		props.changed();
	}, { once: true });
}

export async function togglePlayerPiP(video: HTMLVideoElement, props: PlayerWindowProps): Promise<void> {
	if (windows.has(video)) { closePlayerPiP(video); return; }
	if (document.pictureInPictureElement) { await document.exitPictureInPicture(); return; }
	if (video.readyState < 2) throw new Error('Start playback to open picture-in-picture.');
	const docPip = documentPiP();
	if (docPip) {
		let child: Window | undefined;
		try {
			child = await docPip.requestWindow({ width: 576, height: 324 });
			mirrorPlayer(child, video, props);
			windows.set(video, child);
			props.changed();
			return;
		} catch { child?.close(); }
	}
	await video.requestPictureInPicture();
}
