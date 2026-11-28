import { effectFragment } from './glsl-effects';
import { evaluateSize, shaderPasses, type Size } from './mpv-shaders';
import { smaaArea, smaaFragments, smaaSearch } from './smaa-shaders';
import { effectiveStages, filterOrder, outputSize, type Category, type ProcessingPrefs, type Stage } from './presets';

export type Bindings = Record<string, number>;
export interface RenderPass extends Size {
	key: string;
	fragment: string;
	bindings: Bindings;
	params?: readonly number[];
	precise?: boolean;
	clear?: boolean;
}
export interface RenderPlan extends Size { passes: RenderPass[]; lastUse: number[]; compare: boolean }

const resizes = new Set<Stage>(['fsr_easu', 'lanczos3_2x', 'bicubic_2x', 'auto_downscale']);
export const hasProcessing = (prefs: ProcessingPrefs) => prefs.upscaler.enabled ||
	(prefs.filtersOn && filterOrder.some((key) => prefs.filters[key].enabled));

export async function makeRenderPlan(source: Size, screen: Size, prefs: ProcessingPrefs, category: Category, limit: number): Promise<RenderPlan> {
	const up = prefs.upscaler;
	const target = up.enabled ? outputSize(source.width, source.height, screen.width, screen.height, up.output, limit) : source;
	const passes: RenderPass[] = [];
	let current = 0;
	let highlightStats: number | undefined;
	let highlightClamp: RenderPass | undefined;
	const sizes: Size[] = [source];
	const add = (pass: RenderPass) => { passes.push(pass); sizes.push({ width: pass.width, height: pass.height }); return passes.length; };
	const apply = (key: Parameters<typeof effectFragment>[0], size: Size, params?: readonly number[], reference = 0) =>
		add({ ...size, key, fragment: effectFragment(key), bindings: { inputFrame: current, originalFrame: reference }, params });

	const stages = effectiveStages(up, category);
	const bundled = await Promise.all(stages.map(shaderPasses));
	for (const [index, stage] of stages.entries()) {
		if (stage === 'smaa') {
			const size = sizes[current];
			const detected = add({ ...size, key: 'smaa:0', fragment: smaaFragments[0], bindings: { uColorTexture: current }, clear: true });
			const weights = add({ ...size, key: 'smaa:1', fragment: smaaFragments[1], bindings: { uEdgesTexture: detected, uAreaTexture: smaaArea, uSearchTexture: smaaSearch } });
			current = add({ ...size, key: 'smaa:2', fragment: smaaFragments[2], bindings: { uColorTexture: current, uBlendTexture: weights } });
			continue;
		}
		if (stage === 'ssim_superres' && current === 0) current = apply('bicubic_2x', target);
		const vendor = bundled[index];
		if (!vendor) {
			current = apply(stage, resizes.has(stage) ? target : sizes[current], [up.sharpness]);
			continue;
		}
		const art = stage === 'artcnn_2x' || stage === 'artcnn_ds';
		const before = current;
		if (art) current = apply('luma_input', sizes[current]);
		const starting = current;
		const named = new Map<string, number>([['MAIN', current], ['LUMA', current], ['POSTKERNEL', current], ['PREKERNEL', 0]]);
		for (const [i, pass] of vendor.entries()) {
			if (pass.hook === 'PREKERNEL') {
				highlightStats = named.get('STATSMAX');
				highlightClamp = { ...target, key: `${stage}:${i}`, fragment: pass.fragment, bindings: {} };
				continue;
			}
			named.set('HOOKED', named.get(pass.hook) ?? current);
			const dimensions = new Map([...named].map(([k, index]) => [k, sizes[index]]));
			dimensions.set('OUTPUT', target);
			dimensions.set('NATIVE_CROPPED', source);
			if (pass.when && !evaluateSize(pass.when, dimensions)) continue;
			const width = Math.max(1, Math.floor(evaluateSize(pass.width, dimensions)));
			const height = Math.max(1, Math.floor(evaluateSize(pass.height, dimensions)));
			if (width > limit || height > limit) continue;
			const input = pass.binds.map((name) => named.get(name));
			if (input.some((index) => index === undefined)) continue;
			const id = add({ width, height, key: `${stage}:${i}`, fragment: pass.fragment, precise: true, params: stage === 'ssim_superres' ? [source.width, source.height] : undefined,
				bindings: Object.fromEntries(input.map((index, n) => [`frame${n}`, index!])) });
			named.set(pass.save, id);
		}
		current = named.get(art ? 'LUMA' : stage === 'ssim_superres' ? 'POSTKERNEL' : 'MAIN') ?? current;
		if (art) current = current === starting ? before : apply('luma_output', sizes[current], undefined, before);
	}
	if (highlightClamp && highlightStats !== undefined) {
		current = add({ ...highlightClamp, ...sizes[current], bindings: { frame0: current, frame1: highlightStats } });
	}
	if (prefs.filtersOn) {
		for (const filter of filterOrder) {
			const entry = prefs.filters[filter];
			if (!entry.enabled) continue;
			const params = [...entry.values];
			if (filter === 'dithering') params[1] = prefs.dither;
			if (filter === 'grain') params[2] = Number(prefs.coloredGrain);
			current = apply(filter, sizes[current], params);
		}
	}
	apply('present', target);
	const lastUse = Array.from({ length: passes.length + 1 }, () => 0);
	passes.forEach((pass, i) => {
		for (const input of Object.values(pass.bindings)) if (input >= 0) lastUse[input] = i;
	});
	return { ...target, passes, lastUse, compare: prefs.compare };
}
