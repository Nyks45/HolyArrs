import { expect, test } from 'vitest';
import { makeRenderPlan } from './render-plan';
import { defaultProcessingPrefs, detectContentCategory, effectivePreset, outputSize, type Stage } from './presets';

const source = { width: 640, height: 360 };
const screen = { width: 1920, height: 1080 };

test('auto selects FSR for live action and a neural pipeline for animation', async () => {
	const settings = defaultProcessingPrefs();
	settings.upscaler.enabled = true;
	const category = detectContentCategory({ genres: ['Action', 'Animation'] });
	expect(effectivePreset('auto', category)).toBe('mode_a');
	const general = await makeRenderPlan(source, screen, settings, 'live_action', 4096);
	expect(general.passes.map((pass) => pass.key)).toEqual(['fsr_easu', 'fsr_rcas', 'present']);
	const anime = await makeRenderPlan(source, screen, settings, category, 4096);
	expect(anime.passes.some((pass) => pass.key.startsWith('a4k_upscale_2x'))).toBe(true);
	expect(anime.passes.at(-2)?.key).toBe('clamp_highlights:2');
	expect(anime.passes.at(-1)).toMatchObject({ key: 'present', width: 1920, height: 1080 });
});

test('custom stages use declared live textures and keep the original for comparison', async () => {
	const settings = defaultProcessingPrefs();
	const chain: Stage[] = ['artcnn_2x', 'ssim_superres', 'smaa', 'darken_lines', 'thin_lines'];
	settings.upscaler = { ...settings.upscaler, enabled: true, mode: 'advanced', chain, output: 'native_2x' };
	settings.compare = true;
	const plan = await makeRenderPlan(source, screen, settings, 'live_action', 4096);
	for (const item of chain) expect(plan.passes.some((pass) => pass.key.startsWith(item))).toBe(true);
	expect(plan.passes.at(-1)?.bindings.originalFrame).toBe(0);
	expect(plan.lastUse[0]).toBe(plan.passes.length - 1);
	for (const [index, pass] of plan.passes.entries()) {
		for (const input of Object.values(pass.bindings)) expect(input).toBeLessThanOrEqual(index);
	}
	expect(outputSize(1920, 1080, 640, 360, 'limit_1440p', 4096)).toEqual({ width: 2560, height: 1440 });
	expect(outputSize(3840, 2160, 1920, 1080, 'native_2x', 2048)).toEqual({ width: 2048, height: 1152 });
});
