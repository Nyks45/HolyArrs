import { api } from '#lib/core/api.ts';

export interface SubStylePrefs {
	color: string;
	bg_color: string;
	bg_opacity: number;
	size: number;
	offset_pct: number;
	hdr_dimmed: boolean;
	hdr_brightness: number;
	strip_sdh: boolean;
	font_family: 'sans' | 'serif' | 'mono' | string;
}

export interface StreamPrefs {
	exclude_terms: string[];
	exclude_3d: boolean;
	preferred_quality: string;
	max_size_gb: number;
	auto_play_fastest: boolean;
	auto_fallback: boolean;
	auto_resolve_debrid: boolean;
	auto_skip_intro: boolean;
	auto_skip_recap: boolean;
	auto_skip_outro: boolean;
	anime_skip_client_id?: string;
	scrobble_trakt: boolean;
	scrobble_simkl: boolean;
	tmdb_api_key?: string;
	sub_style: SubStylePrefs;
}

export interface AddonCatalogDef {
	type: string;
	id: string;
	name: string;
	genres?: string[];
}

export interface StremioAddon {
	id: string;
	name: string;
	version?: string;
	description?: string;
	logo?: string;
	manifest_url: string;
	base_url: string;
	enabled: boolean;
	order: number;
	resources: string[];
	types: string[];
	id_prefixes?: string[];
	catalogs?: AddonCatalogDef[];
	updated_at: string;
}

export interface AddonCatalogItem {
	id: string;
	tmdb_id?: number;
	imdb_id?: string;
	kind: 'movie' | 'show';
	name: string;
	poster?: string;
	background?: string;
	year?: string;
	description?: string;
	genres?: string[];
}

export interface AddonCatalogShelf {
	addon_id: string;
	addon_name: string;
	catalog_id: string;
	type: string;
	name: string;
	items: AddonCatalogItem[];
}

export interface NuvioScraper {
	id: string;
	name: string;
	version?: string;
	description?: string;
	filename: string;
	url: string;
	supported_types?: string[];
	enabled: boolean;
	settings?: Record<string, unknown>;
}

export interface NuvioRepo {
	id: string;
	name: string;
	manifest_url: string;
	enabled: boolean;
	scrapers: NuvioScraper[];
	updated_at: string;
}

export interface DebridProviderStatus {
	provider: 'torbox' | 'premiumize' | 'realdebrid' | string;
	name: string;
	configured: boolean;
	masked_key?: string;
	username?: string;
}

export interface SkipSegment {
	kind: 'intro' | 'recap' | 'outro' | 'credits' | string;
	start: number;
	end: number;
	source: string;
	label: string;
}

export interface SyncStatus {
	trakt_connected: boolean;
	simkl_connected: boolean;
	simkl_username?: string;
	simkl_client_id?: string;
	scrobble_trakt: boolean;
	scrobble_simkl: boolean;
}

export interface SimklPINChallenge {
	user_code: string;
	verification_url: string;
	expires_in: number;
	interval: number;
}

export interface PresetAddon {
	id: string;
	name: string;
	category: string;
	description: string;
	manifest_url: string;
	default: boolean;
	installed: boolean;
}

export interface PresetPluginRepo {
	id: string;
	name: string;
	category: string;
	description: string;
	manifest_url: string;
	default: boolean;
	default_scrapers?: string[];
	installed: boolean;
}

export interface PresetCatalog {
	addons: PresetAddon[];
	plugins: PresetPluginRepo[];
}

export interface PresetSetupResult {
	installed: string[];
	skipped: string[];
	errors?: string[];
	catalog: PresetCatalog;
}

export function getStreamingPresets(): Promise<PresetCatalog> {
	return api.get<PresetCatalog>('/streaming/presets');
}

export function applyStreamingPresets(req: { kind?: string; id?: string; recommended?: boolean } = { recommended: true }): Promise<PresetSetupResult> {
	return api.post<PresetSetupResult>('/streaming/presets/setup', req);
}

export function getStreamingPrefs(): Promise<StreamPrefs> {
	return api.get<StreamPrefs>('/streaming/prefs');
}

export function saveStreamingPrefs(prefs: StreamPrefs): Promise<StreamPrefs> {
	return api.put<StreamPrefs>('/streaming/prefs', prefs);
}

export function listStremioAddons(): Promise<{ addons: StremioAddon[] }> {
	return api.get<{ addons: StremioAddon[] }>('/streaming/addons');
}

export function addStremioAddon(manifestUrl: string): Promise<StremioAddon> {
	return api.post<StremioAddon>('/streaming/addons', { manifest_url: manifestUrl });
}

export function toggleStremioAddon(id: string, patch: { enabled?: boolean; order?: number }): Promise<StremioAddon> {
	return api.put<StremioAddon>(`/streaming/addons/${encodeURIComponent(id)}`, patch);
}

export function refreshStremioAddon(id: string): Promise<StremioAddon> {
	return api.post<StremioAddon>(`/streaming/addons/${encodeURIComponent(id)}/refresh`, {});
}

export function deleteStremioAddon(id: string): Promise<void> {
	return api.del(`/streaming/addons/${encodeURIComponent(id)}`);
}

export function listAddonCatalogs(kind = ''): Promise<{ shelves: AddonCatalogShelf[] }> {
	const q = kind ? `?kind=${encodeURIComponent(kind)}` : '';
	return api.get<{ shelves: AddonCatalogShelf[] }>(`/streaming/addons/catalogs${q}`);
}

export function listPluginRepos(): Promise<{ repos: NuvioRepo[]; node_available: boolean }> {
	return api.get<{ repos: NuvioRepo[]; node_available: boolean }>('/streaming/plugins');
}

export function addPluginRepo(manifestUrl: string): Promise<NuvioRepo> {
	return api.post<NuvioRepo>('/streaming/plugins', { manifest_url: manifestUrl });
}

export function updatePluginRepo(
	id: string,
	patch: { enabled?: boolean; scraper_id?: string; scraper_enabled?: boolean; settings?: Record<string, unknown> }
): Promise<NuvioRepo> {
	return api.put<NuvioRepo>(`/streaming/plugins/${encodeURIComponent(id)}`, patch);
}

export function refreshPluginRepo(id: string): Promise<NuvioRepo> {
	return api.post<NuvioRepo>(`/streaming/plugins/${encodeURIComponent(id)}/refresh`, {});
}

export function deletePluginRepo(id: string): Promise<void> {
	return api.del(`/streaming/plugins/${encodeURIComponent(id)}`);
}

export function listDebridProviders(): Promise<{ providers: DebridProviderStatus[] }> {
	return api.get<{ providers: DebridProviderStatus[] }>('/streaming/debrid');
}

export function saveDebridKey(provider: string, apiKey: string): Promise<DebridProviderStatus> {
	return api.put<DebridProviderStatus>(`/streaming/debrid/${encodeURIComponent(provider)}`, { api_key: apiKey });
}

export function deleteDebridKey(provider: string): Promise<void> {
	return api.del(`/streaming/debrid/${encodeURIComponent(provider)}`);
}

export function getSkipSegments(params: {
	kind: 'movie' | 'show';
	imdb?: string;
	tmdb?: string;
	season?: number;
	episode?: number;
	title?: string;
	anime?: boolean;
	session_id?: string;
}): Promise<{ segments: SkipSegment[] }> {
	const sp = new URLSearchParams();
	sp.set('kind', params.kind);
	if (params.imdb) sp.set('imdb', params.imdb);
	if (params.tmdb) sp.set('tmdb', params.tmdb);
	if (params.season) sp.set('season', String(params.season));
	if (params.episode) sp.set('episode', String(params.episode));
	if (params.title) sp.set('title', params.title);
	if (params.anime) sp.set('anime', '1');
	if (params.session_id) sp.set('session_id', params.session_id);
	return api.get<{ segments: SkipSegment[] }>(`/streaming/skip?${sp.toString()}`);
}

export function getSyncStatus(): Promise<SyncStatus> {
	return api.get<SyncStatus>('/streaming/sync');
}

export function postScrobble(params: {
	action: 'start' | 'pause' | 'stop';
	kind: 'movie' | 'show';
	tmdb: number;
	imdb?: string;
	season?: number;
	episode?: number;
	progress: number;
}): Promise<{ sent: string[] }> {
	return api.post<{ sent: string[] }>('/streaming/sync/scrobble', params);
}

export function pullSyncProgress(): Promise<{ merged: number }> {
	return api.post<{ merged: number }>('/streaming/sync/pull', {});
}

export function startSimklPIN(clientId = ''): Promise<SimklPINChallenge> {
	return api.post<SimklPINChallenge>('/streaming/sync/simkl/pin', { client_id: clientId });
}

export function pollSimklPIN(userCode: string): Promise<{ connected: boolean; username: string }> {
	return api.post<{ connected: boolean; username: string }>('/streaming/sync/simkl/poll', { user_code: userCode });
}

export function disconnectSimkl(): Promise<void> {
	return api.del('/streaming/sync/simkl');
}
