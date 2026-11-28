import { to } from '#lib/core/paths.ts';
import { attachHlsMedia } from './player-media';
import { startDirectPlayback } from './player-runtime';
import { ensureStreamHLS, type H264Quality, type SessionStatus } from './stream-api';

type HlsOptions = Parameters<typeof attachHlsMedia>[0];
type HlsHandlers = Omit<HlsOptions, 'srcUrl' | 'startSec'>;
export interface PlaybackTransport {
	mode: 'direct' | 'hls'; offset: number; time: number; local: boolean; reencoded: boolean;
}

export async function connectPlayerTransport(options: {
	session: SessionStatus; start: number; hls: boolean; reencode: boolean; keepFrame: boolean; canTranscode: boolean;
	quality: string; audio: number; h264: H264Quality; media: HlsHandlers;
	onPipeline: (transport: PlaybackTransport) => void; onReseek: (loading: boolean) => void; onDirect: () => void;
}) {
	const { session, start, hls, reencode, quality, audio, media, onPipeline, onReseek, onDirect } = options;
	const transport = session.transport ?? 'torrent';
	const url = to(session.direct_url || session.media_url);
	const remote = !hls && !reencode && transport === 'hls_remote';
	const direct = !hls && !reencode && quality === 'original' && audio === 0 && !session.needs_transcode
		&& (transport === 'local' || transport === 'direct' ? session.probe?.direct_playable !== false : !!session.probe?.direct_playable);
	if (remote) {
		onPipeline({ mode: 'hls', offset: 0, time: start > .5 ? start : 0, local: false, reencoded: false });
		return attachHlsMedia({ ...media, srcUrl: url, startSec: start });
	}
	if (direct) {
		onPipeline({ mode: 'direct', offset: 0, time: start > .5 ? start : 0, local: false, reencoded: false });
		await startDirectPlayback(media.video, url, start, media.playbackRate, media.volume, media.muted, media.isCancelled);
		if (!media.isCancelled()) onDirect();
		return null;
	}
	if (!options.canTranscode) throw new Error('ffmpeg is not installed for transcoding');
	if (options.keepFrame) onReseek(true);
	const result = await ensureStreamHLS(session.id, quality, audio, start, reencode, options.h264);
	if (media.isCancelled()) return null;
	onReseek(false);
	onPipeline({ mode: 'hls', offset: result.start_sec, time: 0, local: true, reencoded: reencode || quality !== 'original' });
	return attachHlsMedia({ ...media, srcUrl: to(result.playlist_url), startSec: 0 });
}
