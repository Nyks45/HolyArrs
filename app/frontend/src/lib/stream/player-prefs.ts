import type { H264Quality } from './stream-api';

const localKey = 'holyarrs-player-device-v1';
export type PlayerUtilityPanel = 'none' | 'more' | 'stats' | 'shortcuts' | 'filters' | 'upscaler' | 'style';
export type VideoFit = 'contain' | 'cover' | 'fill';
export type SubtitleScope = 'episode' | 'title' | 'global';
export type SubtitleRenderer = 'canvas2d' | 'webgl1' | 'webgl2' | 'webgpu';
export type SubtitleStyleOverride = { font?: string; scale?: number; hidden?: boolean };
export type ScopedSubtitleStyle = { scale?: number; offset?: number; styles?: Record<string, SubtitleStyleOverride> };
export type SubtitleContext = { kind: 'movie' | 'show'; tmdb: string; season: number; episode: number };

export type PlayerDevicePrefs = {
	fit: VideoFit;
	fullscreen: 'player' | 'document';
	immersive: boolean;
	pauseAway: boolean;
	autoNext: boolean;
	forceAv1: boolean;
	h264Quality: H264Quality;
	boost: number;
	overlap: boolean;
	blendFix: boolean;
	renderer: SubtitleRenderer;
	subtitles: Record<string, ScopedSubtitleStyle>;
};

export const defaultDevicePrefs: PlayerDevicePrefs = {
	fit: 'contain', fullscreen: 'player', immersive: false, pauseAway: false, autoNext: true,
	forceAv1: false, h264Quality: 'balanced', boost: 100, overlap: true, blendFix: false, renderer: 'canvas2d', subtitles: {}
};

function choices<T extends string>(input: unknown, options: readonly T[], fallback: T): T {
	return options.find((option) => option === input) ?? fallback;
}

export function inRange(input: unknown, min: number, max: number, fallback: number): number {
	return typeof input === 'number' && Number.isFinite(input) ? Math.min(max, Math.max(min, input)) : fallback;
}

export function loadPlayerDevicePrefs(): PlayerDevicePrefs {
	try {
		const saved = JSON.parse(localStorage.getItem(localKey) ?? '{}') as Partial<PlayerDevicePrefs> | null;
		if (!saved || typeof saved !== 'object' || Array.isArray(saved)) return { ...defaultDevicePrefs };
		return {
			fit: choices(saved.fit, ['contain', 'cover', 'fill'], 'contain'),
			fullscreen: choices(saved.fullscreen, ['player', 'document'], 'player'),
			immersive: saved.immersive === true, pauseAway: saved.pauseAway === true, autoNext: saved.autoNext !== false,
			forceAv1: saved.forceAv1 === true, h264Quality: choices(saved.h264Quality, ['balanced', 'high'], 'balanced'),
			boost: inRange(saved.boost, 100, 300, 100), overlap: saved.overlap !== false, blendFix: saved.blendFix === true,
			renderer: choices(saved.renderer, ['canvas2d', 'webgl1', 'webgl2', 'webgpu'], 'canvas2d'),
			subtitles: saved.subtitles && typeof saved.subtitles === 'object' && !Array.isArray(saved.subtitles) ? saved.subtitles : {}
		};
	} catch {
		return { ...defaultDevicePrefs };
	}
}

export function savePlayerDevicePrefs(prefs: PlayerDevicePrefs): void {
	try { localStorage.setItem(localKey, JSON.stringify(prefs)); } catch { /* A browser can deny storage. */ }
}

export function subtitleScopeKey(scope: SubtitleScope, ctx: SubtitleContext): string {
	if (scope === 'global') return scope;
	if (scope === 'title' || ctx.kind === 'movie') return `title:${ctx.kind}:${ctx.tmdb}`;
	return `episode:${ctx.tmdb}:${ctx.season}:${ctx.episode}`;
}

export function resolveScopedSubtitleStyle(saved: PlayerDevicePrefs['subtitles'], ctx: SubtitleContext): Required<ScopedSubtitleStyle> {
	let scale = 100;
	let offset = 0;
	const styles: Record<string, SubtitleStyleOverride> = Object.create(null);
	const scopes: SubtitleScope[] = ctx.kind === 'show' ? ['global', 'title', 'episode'] : ['global', 'title'];
	for (const scope of scopes) {
		const choice = saved[subtitleScopeKey(scope, ctx)];
		if (!choice || typeof choice !== 'object') continue;
		scale = inRange(choice.scale, 50, 200, scale);
		offset = inRange(choice.offset, -150, 150, offset);
		for (const [name, style] of Object.entries(choice.styles ?? {})) {
			if (!style || typeof style !== 'object') continue;
			const current = styles[name] ?? {};
			styles[name] = {
				font: typeof style.font === 'string' ? style.font : current.font,
				scale: inRange(style.scale, 50, 200, current.scale ?? 100),
				hidden: typeof style.hidden === 'boolean' ? style.hidden : current.hidden
			};
		}
	}
	return { scale, offset, styles };
}
