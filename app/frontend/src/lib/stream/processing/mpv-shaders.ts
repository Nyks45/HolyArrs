import { artCnnFragment } from './artcnn-shader';
import type { Stage } from './presets';

const bundled = import.meta.glob<string>('./shaders/*.glsl', { query: '?raw', import: 'default' });

const files: Partial<Record<Stage, string>> = {
	a4k_restore: 'Anime4K_Restore_CNN_S', a4k_restore_soft: 'Anime4K_Restore_CNN_Soft_S',
	a4k_upscale_2x: 'Anime4K_Upscale_CNN_x2_S', a4k_upscale_denoise: 'Anime4K_Upscale_Denoise_CNN_x2_S',
	clamp_highlights: 'Anime4K_Clamp_Highlights', darken_lines: 'Anime4K_Darken_VeryFast', thin_lines: 'Anime4K_Thin_VeryFast',
	artcnn_2x: 'ArtCNN_C4F16', artcnn_ds: 'ArtCNN_C4F16_DS', ssim_superres: 'SSimSuperRes'
};

export interface ShaderPass {
	hook: string;
	binds: string[];
	save: string;
	width: string;
	height: string;
	when: string;
	fragment: string;
}

export function parseShaderPasses(source: string): ShaderPass[] {
	return (source.includes('//!HOOK POSTKERNEL') ? source.split(/(?=^\/\/!HOOK POSTKERNEL\b)/m).filter((part) => part.startsWith('//!HOOK POSTKERNEL')) :
		source.split(/(?=^\/\/!DESC\b)/m).filter((part) => part.startsWith('//!DESC'))).map((part) => {
		const read = (key: string) => [...part.matchAll(new RegExp(`^//!${key}\\s+(.+)$`, 'mg'))].map((m) => m[1].trim());
		const hook = read('HOOK')[0] ?? 'MAIN';
		const binds = read('BIND');
		const body = read('COMPUTE').length ? artCnnFragment(part) : part.replace(/^\/\/!.*$/mg, '').replace(/textureLod\((\w+)_raw,\s*(\w+),\s*0.0\)/g, '$1_tex($2)');
		const bindings = binds.map((name, i) => `uniform sampler2D frame${i};
#define ${name}_raw frame${i}
#define ${name}_mul 1.0
#define ${name}_pos uv
#define ${name}_size vec2(textureSize(frame${i},0))
#define ${name}_pt (1.0/${name}_size)
#define ${name}_tex(p) texture(frame${i},p)
#define ${name}_fetch(p) texelFetch(frame${i},clamp(p,ivec2(0),textureSize(frame${i},0)-1),0)
#define ${name}_texOff(p) texture(frame${i},uv+vec2(p)*${name}_pt)`).join('\n');
		return {
			hook, binds, save: read('SAVE')[0] ?? hook, width: read('WIDTH')[0] ?? 'HOOKED.w', height: read('HEIGHT')[0] ?? 'HOOKED.h',
			when: read('WHEN')[0] ?? '',
			fragment: `#version 300 es\nprecision highp float;\nprecision highp int;\nin vec2 uv;\nout vec4 result;\n${hook === 'POSTKERNEL' ? 'uniform vec4 params;\n#define input_size params.xy\n#define tex_offset vec2(0.0)' : ''}\n${bindings}\n${hook === 'MAIN' && binds.includes('HOOKED') && !binds.includes('MAIN') ? '#define MAIN_texOff(p) HOOKED_texOff(p)' : ''}\n${body}\nvoid main(){result=hook();}`
		};
	});
}

const stages = new Map<Stage, Promise<ShaderPass[]>>();
export function shaderPasses(stage: Stage): Promise<ShaderPass[]> | undefined {
	const name = files[stage];
	if (!name) return undefined;
	const cached = stages.get(stage);
	if (cached) return cached;
	const source = bundled[`./shaders/${name}.glsl`];
	if (!source) throw new Error(`Shader asset ${name} is missing`);
	const loaded = source().then(parseShaderPasses);
	stages.set(stage, loaded);
	void loaded.catch(() => stages.delete(stage));
	return loaded;
}

export interface Size { width: number; height: number }
export function evaluateSize(formula: string, sizes: ReadonlyMap<string, Size>): number {
	const stack: number[] = [];
	for (const part of formula.split(/\s+/)) {
		if (!part) continue;
		const variable = /^([\w]+)\.([wh])$/.exec(part);
		if (variable) {
			const size = sizes.get(variable[1]);
			if (!size) throw new Error(`Shader refers to unavailable frame ${variable[1]}`);
			stack.push(variable[2] === 'w' ? size.width : size.height);
			continue;
		}
		if (!Number.isNaN(Number(part))) { stack.push(Number(part)); continue; }
		const right = stack.pop() ?? 0, left = stack.pop() ?? 0;
		switch (part) {
			case '/': stack.push(left / right); break;
			case '*': stack.push(left * right); break;
			case '+': stack.push(left + right); break;
			case '-': stack.push(left - right); break;
			case '>': stack.push(left > right ? 1 : 0); break;
			case '<': stack.push(left < right ? 1 : 0); break;
			default: throw new Error(`Unsupported shader size operator ${part}`);
		}
	}
	return stack.pop() ?? 0;
}
