import type { TitleDetails } from '#lib/metadata/index.ts';
import { inRange } from '../player-prefs';

export const stages = {
	fsr_easu: 'FSR EASU', fsr_rcas: 'FSR RCAS', lanczos3_2x: 'Lanczos3 ×2', bicubic_2x: 'Bicubic ×2',
	a4k_restore: 'Anime4K Restore', a4k_restore_soft: 'Anime4K Restore Soft', a4k_upscale_2x: 'Anime4K Upscale ×2',
	a4k_upscale_denoise: 'Anime4K Upscale and Denoise', artcnn_2x: 'ArtCNN ×2', artcnn_ds: 'ArtCNN DS',
	denoise: 'Denoise', deblur: 'Deblur', clamp_highlights: 'Clamp highlights', fxaa: 'FXAA', smaa: 'SMAA',
	cas_sharpen: 'Contrast-adaptive sharpening', ssim_superres: 'SSIM-guided restoration', darken_lines: 'Darken lines', thin_lines: 'Thin lines', auto_downscale: 'Fit output'
};
export type Stage = keyof typeof stages;
export type Category = 'live_action' | 'animation';

const recipes = {
	fsr: ['fsr_easu', 'fsr_rcas'], fsr_denoise: ['denoise', 'fsr_easu', 'fsr_rcas'],
	cinema_sharp: ['lanczos3_2x', 'fsr_rcas'], ultra_clean: ['denoise', 'fsr_easu', 'ssim_superres', 'fsr_rcas'],
	natural_2x: ['bicubic_2x', 'cas_sharpen'],
	mode_a: ['clamp_highlights', 'a4k_restore', 'a4k_upscale_2x'], mode_b: ['clamp_highlights', 'a4k_restore_soft', 'a4k_upscale_2x'],
	mode_c: ['clamp_highlights', 'a4k_upscale_denoise'], mode_aa: ['clamp_highlights', 'a4k_restore', 'a4k_upscale_2x', 'a4k_restore'],
	mode_bb: ['clamp_highlights', 'a4k_restore_soft', 'a4k_upscale_2x', 'a4k_restore_soft'], mode_ca: ['clamp_highlights', 'a4k_upscale_denoise', 'a4k_restore']
} satisfies Record<string, Stage[]>;

export type Preset = 'auto' | keyof typeof recipes;
export const presets: Record<Preset, string> = {
	auto: 'Auto (Smart)', fsr: 'FSR 1.0', fsr_denoise: 'FSR + Denoise', cinema_sharp: 'Cinema Sharp', ultra_clean: 'Ultra Clean', natural_2x: 'Natural 2×',
	mode_a: 'Mode A', mode_b: 'Mode B', mode_c: 'Mode C', mode_aa: 'A+A', mode_bb: 'B+B', mode_ca: 'C+A'
};
export const general: Preset[] = ['auto', 'fsr', 'fsr_denoise', 'cinema_sharp', 'ultra_clean', 'natural_2x'];
export const animation: Preset[] = ['mode_a', 'mode_b', 'mode_c', 'mode_aa', 'mode_bb', 'mode_ca'];
export const outputs = { auto_display: 'Display size', native_2x: 'Native ×2', limit_1440p: '1440p limit', limit_4k: '4K limit' };
export type OutputSize = keyof typeof outputs;

export interface Upscaler {
	enabled: boolean;
	mode: 'preset' | 'advanced';
	preset: Preset;
	chain: Stage[];
	output: OutputSize;
	sharpness: number;
	darken: boolean;
	thin: boolean;
	workgroup: '8x8' | '16x16' | '32x8';
}

export function detectContentCategory(details: Pick<TitleDetails, 'genres'> | null): Category {
	return details?.genres.some((genre) => /\b(anime|animation|animated)\b/i.test(genre)) ? 'animation' : 'live_action';
}

export function effectivePreset(preset: Preset, category: Category): keyof typeof recipes {
	return preset === 'auto' ? category === 'animation' ? 'mode_a' : 'fsr' : preset;
}

export function effectiveStages(upscaler: Upscaler, category: Category): Stage[] {
	if (!upscaler.enabled) return [];
	const result: Stage[] = upscaler.mode === 'advanced' ? [...upscaler.chain] : [...recipes[effectivePreset(upscaler.preset, category)]];
	if (upscaler.darken) result.push('darken_lines');
	if (upscaler.thin) result.push('thin_lines');
	return result;
}

export function outputSize(width: number, height: number, screenWidth: number, screenHeight: number, mode: OutputSize, deviceLimit = 4096) {
	if (!(width > 0 && height > 0)) return { width: 0, height: 0 };
	const limit = mode === 'limit_1440p' ? 1440 : 2160;
	const displayScale = screenWidth > 0 && screenHeight > 0 ? Math.min(screenWidth / width, screenHeight / height) : 1;
	const factor = Math.min(mode === 'auto_display' ? Math.max(1, displayScale) : 2, deviceLimit / width, deviceLimit / height, limit / height);
	return { width: Math.max(1, Math.round(width * factor)), height: Math.max(1, Math.round(height * factor)) };
}

export const filterControls = {
	debanding: { name: 'Debanding', controls: [ ['Threshold', 0, 255, 1, 88], ['Range', 1, 32, 1, 17], ['Iterations', 1, 6, 1, 4], ['Grain', 0, 30, 1, 12] ] },
	sharpening: { name: 'Sharpening (CAS)', controls: [ ['Strength', 0, 1, .01, .5] ] },
	dithering: { name: 'Dithering', controls: [ ['Strength', 0, 1, .01, .5] ] },
	color: { name: 'Color adjustments', controls: [ ['Brightness', -100, 100, 1, 0], ['Contrast', 0, 200, 1, 100], ['Saturation', 0, 200, 1, 100], ['Gamma', .1, 3, .01, 1] ] },
	grain: { name: 'Film grain', controls: [ ['Intensity', 0, 100, 1, 15], ['Grain size', .5, 4, .1, 1.5] ] },
	localContrast: { name: 'Local contrast', controls: [ ['Strength', 0, 100, 1, 30], ['Radius', 1, 16, 1, 4] ] },
	aberration: { name: 'Chromatic aberration', controls: [ ['Intensity', 0, 100, 1, 30] ] }
} satisfies Record<string, { name: string; controls: [string, number, number, number, number][] }>;

export type Filter = keyof typeof filterControls;
export const filterOrder = Object.keys(filterControls) as Filter[];
export const ditherTypes = ['White noise', 'Ordered', 'Blue noise', 'Triangular', 'Interleaved gradient', 'Fruit'];
export interface ProcessingPrefs {
	upscaler: Upscaler;
	filtersOn: boolean;
	filters: Record<Filter, { enabled: boolean; values: number[] }>;
	dither: number;
	coloredGrain: boolean;
	compare: boolean;
}

export function defaultProcessingPrefs(): ProcessingPrefs {
	return {
		upscaler: { enabled: false, mode: 'preset', preset: 'auto', chain: [], output: 'auto_display', sharpness: .45, darken: false, thin: false, workgroup: '8x8' },
		filtersOn: false, filters: Object.fromEntries(filterOrder.map((id) => [id, { enabled: false, values: filterControls[id].controls.map((control) => control[4]) }])) as ProcessingPrefs['filters'],
		dither: 4, coloredGrain: false, compare: false
	};
}

const storageKey = 'holyarrs-processing-v1';
export function loadProcessingPrefs(): ProcessingPrefs {
	const prefs = defaultProcessingPrefs();
	try {
		const saved = JSON.parse(localStorage.getItem(storageKey) ?? '{}') as Partial<ProcessingPrefs>;
		const up = saved.upscaler;
		if (up && typeof up === 'object') {
			prefs.upscaler = {
				enabled: up.enabled === true, mode: up.mode === 'advanced' ? 'advanced' : 'preset',
				preset: Object.hasOwn(presets, up.preset) ? up.preset : 'auto',
				chain: Array.isArray(up.chain) ? up.chain.filter((item) => Object.hasOwn(stages, item)).slice(0, 16) : [],
				output: Object.hasOwn(outputs, up.output) ? up.output : 'auto_display', sharpness: inRange(up.sharpness, 0, 1, .45),
				darken: up.darken === true, thin: up.thin === true, workgroup: up.workgroup === '16x16' || up.workgroup === '32x8' ? up.workgroup : '8x8'
			};
		}
		prefs.filtersOn = saved.filtersOn === true; prefs.coloredGrain = saved.coloredGrain === true; prefs.compare = saved.compare === true;
		prefs.dither = Math.round(inRange(saved.dither, 0, ditherTypes.length - 1, 4));
		for (const id of filterOrder) {
			const entry = saved.filters?.[id];
			prefs.filters[id] = { enabled: entry?.enabled === true, values: filterControls[id].controls.map(([, min, max, , fallback], i) => inRange(entry?.values?.[i], min, max, fallback)) };
		}
	} catch { /* Use defaults if browser storage is inaccessible or invalid. */ }
	return prefs;
}

export function saveProcessingPrefs(prefs: ProcessingPrefs) {
	try { localStorage.setItem(storageKey, JSON.stringify(prefs)); } catch { /* Storage may be disabled. */ }
}
