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
	node_available?: boolean;
}

export interface StreamSearchReq {
	query: string;
	kind: 'movie' | 'show';
	year?: number;
	season?: number;
	episode?: number;
	imdb?: string;
	tmdb?: number;
	original_title?: string;
	genres?: string[];
	original_language?: string;
	origin_country?: string[];
	anime?: boolean;
	mode?: 'fast' | 'full';
	no_cache?: boolean;
	preferred_quality?: string;
	source_ids?: number[];
	local_only?: boolean;
}

export type CandidateKind = 'local' | 'http' | 'hls' | 'debrid_cached' | 'torrent';

export interface StreamCandidate {
	id?: string;
	kind?: CandidateKind;
	instant?: boolean;
	fast_start?: boolean;
	probe_ms?: number;
	verified?: boolean;
	provider?: string;
	provider_type?: string;
	release_name?: string;
	quality?: string;
	local_file_id?: number;
	debrid_provider?: string;
	filename?: string;
	score?: number;
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
	counts?: {
		local: number;
		instant: number;
		debrid: number;
		torrent: number;
	};
	warnings?: string[];
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
	candidate_id?: string;
	local_file_id?: number;
	stream_url?: string;
	headers?: Record<string, string>;
	debrid_provider?: string;
	tmdb?: number;
	imdb?: string;
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

export interface ChapterMark {
	start: number;
	end: number;
	title?: string;
	kind?: string;
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
	chapters?: ChapterMark[];
}

export interface SessionStatus {
	id: string;
	token: string;
	title: string;
	release_title: string;
	source_id: number;
	source_name: string;
	source_provider?: string;
	transport?: 'torrent' | 'local' | 'direct' | 'hls_remote';
	direct_url?: string;
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
	needs_transcode?: boolean;
	probe?: MediaProbe;
	chapters?: ChapterMark[];
}

export interface HLSResponse {
	playlist_url: string;
	start_sec: number;
}

export interface SubPref {
	mode: 'off' | 'embedded' | 'site' | 'upload';
	language?: string;
	label?: string;
	index?: number;
	site_id?: string;
	site_key?: string;
	delay_ms?: number;
	size?: 'sm' | 'md' | 'lg' | 'xl';
}

export interface WatchEntry {
	key?: string;
	user_id?: number;
	kind: 'movie' | 'show';
	tmdb: string;
	imdb?: string;
	title: string;
	year?: number;
	poster?: string;
	backdrop?: string;
	season?: number;
	episode?: number;
	episode_name?: string;
	position_sec: number;
	duration_sec: number;
	progress_pct?: number;
	completed?: boolean;
	source_id?: number;
	source_name?: string;
	release_title?: string;
	release_link?: string;
	info_hash?: string;
	resolution?: string;
	file_index?: number;
	quality?: string;
	audio_index?: number;
	playback_rate?: number;
	subtitle?: SubPref;
	updated_at?: string;
}

export interface WatchHistoryResponse {
	items: WatchEntry[];
	continue_watching: WatchEntry[];
}

export interface TitleWatchResponse {
	latest: WatchEntry | null;
	episodes: WatchEntry[];
}

export interface StreamSubSearchReq {
	kind: 'movie' | 'show';
	title: string;
	year?: number;
	season?: number;
	episode?: number;
	imdb?: string;
	tmdb?: string;
	release_name?: string;
	languages?: string[];
}

export interface StreamSubCandidate {
	id: string;
	site_id: number;
	site_name: string;
	site_kind: string;
	language: string;
	release_names?: string[];
	hearing_impaired?: boolean;
	forced?: boolean;
	downloads?: number;
	format?: string;
	machine_made?: string;
}

export interface StreamSubSearchResponse {
	candidates: StreamSubCandidate[];
	languages: string[];
}

export interface StreamSubFetchResponse {
	vtt: string;
	language?: string;
	sub_file?: string;
}

export interface MPVLaunchReq {
	audio_lang?: string;
	sub_lang?: string;
	start_sec?: number;
	sub_file?: string;
	watch?: WatchEntry;
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

export function launchStreamMPV(id: string, opts: MPVLaunchReq | string = '', legacySubLang = ''): Promise<SessionStatus> {
	const body: MPVLaunchReq =
		typeof opts === 'string'
			? { audio_lang: opts, sub_lang: legacySubLang }
			: opts;
	return api.post<SessionStatus>(`/stream/${encodeURIComponent(id)}/mpv`, body);
}

export function setStreamKeep(id: string, keepFile: boolean): Promise<SessionStatus> {
	return api.put<SessionStatus>(`/stream/${encodeURIComponent(id)}/keep`, { keep_file: keepFile });
}

export function stopStream(id: string): Promise<void> {
	return api.del(`/stream/${encodeURIComponent(id)}`);
}

export function listWatchHistory(limit = 100): Promise<WatchHistoryResponse> {
	return api.get<WatchHistoryResponse>(`/stream/history?limit=${limit}`);
}

export function getTitleWatchHistory(kind: 'movie' | 'show', tmdb: string): Promise<TitleWatchResponse> {
	return api.get<TitleWatchResponse>(`/stream/history/title/${kind}/${encodeURIComponent(tmdb)}`);
}

export function saveWatchProgress(entry: WatchEntry, keepalive = false): Promise<WatchEntry> {
	return api.request<WatchEntry>('/stream/history', { method: 'PUT', body: entry, keepalive });
}

export function deleteWatchHistory(key: string): Promise<void> {
	return api.del(`/stream/history/${encodeURIComponent(key)}`);
}

export function searchStreamSubtitles(req: StreamSubSearchReq): Promise<StreamSubSearchResponse> {
	return api.post<StreamSubSearchResponse>('/stream/subtitles/search', req);
}

export function fetchStreamSubtitle(id: string, sessionId = ''): Promise<StreamSubFetchResponse> {
	return api.post<StreamSubFetchResponse>('/stream/subtitles/fetch', { id, session_id: sessionId });
}

export function uploadStreamSubtitle(filename: string, content: string, language = '', sessionId = ''): Promise<StreamSubFetchResponse> {
	return api.post<StreamSubFetchResponse>('/stream/subtitles/upload', {
		filename,
		language,
		content,
		session_id: sessionId
	});
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

export function formatTimeLeft(posSec: number, durSec: number): string {
	if (!durSec || !Number.isFinite(durSec) || durSec <= 0) return '';
	const rem = Math.max(0, Math.floor(durSec - Math.max(0, posSec)));
	if (rem < 60) return '< 1m left';
	const totalMin = Math.round(rem / 60);
	const h = Math.floor(totalMin / 60);
	const m = totalMin % 60;
	if (h > 0) return m > 0 ? `${h}h ${m}m left` : `${h}h left`;
	return `${totalMin}m left`;
}

export function formatEndsAt(posSec: number, durSec: number, playbackRate = 1): string {
	if (!durSec || !Number.isFinite(durSec) || durSec <= 0) return '';
	const remSec = Math.max(0, (durSec - Math.max(0, posSec)) / (playbackRate > 0 ? playbackRate : 1));
	if (!Number.isFinite(remSec) || remSec <= 0) return '';
	const endDate = new Date(Date.now() + remSec * 1000);
	const hh = String(endDate.getHours()).padStart(2, '0');
	const mm = String(endDate.getMinutes()).padStart(2, '0');
	return `Ends at ${hh}:${mm}`;
}
