import { api } from '#lib/core/api.ts';

export interface StreamSourceInfo {
	id: number;
	name: string;
	format: string;
	kinds: string[];
	priority: number;
	state: string;
}

export interface StreamSourcesResponse {
	sources: StreamSourceInfo[];
	mpv_available: boolean;
	ffmpeg_available: boolean;
}

export interface StreamSearchReq {
	query: string;
	kind: 'movie' | 'show';
	year?: number;
	season?: number;
	episode?: number;
	imdb?: string;
	tmdb?: number;
	source_ids?: number[];
}

export interface StreamCandidate {
	source_id: number;
	source_name: string;
	title: string;
	link: string;
	info_hash: string;
	protocol: string;
	size: number;
	seeders: number;
	peers: number;
	resolution: string;
	resolution_num: number;
	source_tag: string;
	codec: string;
	hdr: string;
	audio: string;
	languages: string[];
	season?: number;
	episode?: number;
	is_pack?: boolean;
	published?: string;
}

export interface StreamSourceCheck {
	id: number;
	name: string;
	health: string;
	paused?: boolean;
	answered: boolean;
	count: number;
	ms: number;
	error?: string;
}

export interface StreamSearchResponse {
	candidates: StreamCandidate[];
	sources: StreamSourceCheck[];
}

export interface StreamStartReq {
	source_id: number;
	source_name: string;
	link: string;
	title: string;
	release_title: string;
	kind: 'movie' | 'show';
	season?: number;
	episode?: number;
	keep_file?: boolean;
}

export interface VideoFile {
	index: number;
	name: string;
	path: string;
	size: number;
	selected: boolean;
}

export interface AudioTrack {
	index: number;
	stream_index: number;
	language?: string;
	title?: string;
	codec?: string;
	channels?: number;
}

export interface SubtitleTrack {
	index: number;
	stream_index: number;
	language?: string;
	title?: string;
	codec?: string;
	text: boolean;
}

export interface MediaProbe {
	duration_sec: number;
	format_name?: string;
	video_codec?: string;
	width?: number;
	height?: number;
	pix_fmt?: string;
	direct_playable: boolean;
	audio_tracks: AudioTrack[];
	subtitle_tracks: SubtitleTrack[];
}

export interface SessionStatus {
	id: string;
	token: string;
	title: string;
	release_title: string;
	source_id: number;
	source_name: string;
	kind: 'movie' | 'show';
	season?: number;
	episode?: number;
	state: 'resolving' | 'buffering' | 'ready' | 'error';
	error?: string;
	files: VideoFile[];
	selected_file: number;
	peers: number;
	seeders: number;
	download_bps: number;
	downloaded_bytes: number;
	file_size: number;
	progress_pct: number;
	keep_file: boolean;
	mpv_running: boolean;
	media_url: string;
	probe?: MediaProbe;
}

export interface HLSResponse {
	playlist_url: string;
	start_sec: number;
}

export function listStreamSources(kind: 'movie' | 'show'): Promise<StreamSourcesResponse> {
	return api.get<StreamSourcesResponse>(`/stream/sources?kind=${kind}`);
}

export function searchStreamReleases(req: StreamSearchReq): Promise<StreamSearchResponse> {
	return api.post<StreamSearchResponse>('/stream/search', req);
}

export function startStream(req: StreamStartReq): Promise<SessionStatus> {
	return api.post<SessionStatus>('/stream/start', req);
}

export function getStreamStatus(id: string): Promise<SessionStatus> {
	return api.get<SessionStatus>(`/stream/${encodeURIComponent(id)}`);
}

export function selectStreamFile(id: string, fileIndex: number): Promise<SessionStatus> {
	return api.post<SessionStatus>(`/stream/${encodeURIComponent(id)}/file`, { file_index: fileIndex });
}

export function ensureStreamHLS(id: string, quality: string, audioIndex: number, startSec: number): Promise<HLSResponse> {
	return api.post<HLSResponse>(`/stream/${encodeURIComponent(id)}/hls`, {
		quality,
		audio_index: audioIndex,
		start_sec: startSec
	});
}

export function launchStreamMPV(id: string, audioLang = '', subLang = ''): Promise<SessionStatus> {
	return api.post<SessionStatus>(`/stream/${encodeURIComponent(id)}/mpv`, {
		audio_lang: audioLang,
		sub_lang: subLang
	});
}

export function setStreamKeep(id: string, keepFile: boolean): Promise<SessionStatus> {
	return api.put<SessionStatus>(`/stream/${encodeURIComponent(id)}/keep`, { keep_file: keepFile });
}

export function stopStream(id: string): Promise<void> {
	return api.del(`/stream/${encodeURIComponent(id)}`);
}

export function formatSpeed(bps: number): string {
	if (!bps || bps <= 0) return '0 KB/s';
	const units = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
	let v = bps;
	let i = 0;
	while (v >= 1024 && i < units.length - 1) {
		v /= 1024;
		i++;
	}
	return `${i === 0 ? v.toFixed(0) : v.toFixed(v < 10 ? 1 : 0)} ${units[i]}`;
}

export function formatDuration(sec: number): string {
	if (!sec || !Number.isFinite(sec) || sec < 0) return '0:00';
	const total = Math.floor(sec);
	const h = Math.floor(total / 3600);
	const m = Math.floor((total % 3600) / 60);
	const s = total % 60;
	if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
	return `${m}:${String(s).padStart(2, '0')}`;
}
