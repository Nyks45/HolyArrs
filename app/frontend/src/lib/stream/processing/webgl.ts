import { vertexShader } from './glsl-effects';
import type { RenderPass, RenderPlan } from './render-plan';
import { smaaTexture } from './smaa-textures';
import type { Size } from './mpv-shaders';

interface Texture extends Size { texture: WebGLTexture; precise: boolean }
interface Program { program: WebGLProgram; uniforms: Map<string, WebGLUniformLocation | null> }

export class WebGLVideoRenderer {
	readonly name = 'WebGL2';
	readonly limit: number;
	private readonly gl: WebGL2RenderingContext;
	private readonly framebuffer: WebGLFramebuffer;
	private readonly vertex: WebGLShader;
	private readonly floats: boolean;
	private readonly pool: Texture[] = [];
	private readonly lookups = new Map<number, Texture>();
	private readonly programs = new Map<string, Program>();
	private source?: Texture;
	private pipeline?: RenderPlan;

	constructor(private readonly canvas: HTMLCanvasElement) {
		const gl = canvas.getContext('webgl2', { alpha: false, antialias: false, preserveDrawingBuffer: true, depth: false, powerPreference: 'high-performance' });
		if (!gl) throw new Error('WebGL2 is unavailable');
		this.gl = gl;
		this.limit = gl.getParameter(gl.MAX_TEXTURE_SIZE) as number;
		this.floats = !!gl.getExtension('EXT_color_buffer_float');
		this.framebuffer = gl.createFramebuffer();
		this.vertex = this.compile(gl.VERTEX_SHADER, vertexShader);
		gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, true);
		gl.pixelStorei(gl.UNPACK_COLORSPACE_CONVERSION_WEBGL, gl.NONE);
	}

	private compile(type: number, source: string): WebGLShader {
		const gl = this.gl;
		const shader = gl.createShader(type);
		if (!shader) throw new Error('Could not allocate a video shader');
		gl.shaderSource(shader, source);
		gl.compileShader(shader);
		if (gl.getShaderParameter(shader, gl.COMPILE_STATUS)) return shader;
		const reason = gl.getShaderInfoLog(shader);
		gl.deleteShader(shader);
		throw new Error(reason?.trim() || 'Shader compilation failed');
	}

	private program(pass: RenderPass): Program {
		const cached = this.programs.get(pass.key);
		if (cached) return cached;
		const gl = this.gl;
		const fragment = this.compile(gl.FRAGMENT_SHADER, pass.fragment);
		const program = gl.createProgram();
		gl.attachShader(program, this.vertex);
		gl.attachShader(program, fragment);
		gl.linkProgram(program);
		gl.deleteShader(fragment);
		if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
			const reason = gl.getProgramInfoLog(program);
			gl.deleteProgram(program);
			throw new Error(reason?.trim() || 'Shader link failed');
		}
		const result = { program, uniforms: new Map<string, WebGLUniformLocation | null>() };
		this.programs.set(pass.key, result);
		return result;
	}

	private createTexture(size: Size, precise = false): Texture {
		const gl = this.gl;
		const texture = gl.createTexture();
		gl.bindTexture(gl.TEXTURE_2D, texture);
		gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
		gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
		gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
		gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
		gl.texStorage2D(gl.TEXTURE_2D, 1, precise ? gl.RGBA16F : gl.RGBA8, size.width, size.height);
		return { width: size.width, height: size.height, texture, precise };
	}

	private getLookup(id: number) {
		const cached = this.lookups.get(id);
		if (cached) return cached;
		const data = smaaTexture(id);
		const gl = this.gl;
		const texture = this.createTexture(data);
		gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false);
		gl.texSubImage2D(gl.TEXTURE_2D, 0, 0, 0, data.width, data.height, gl.RGBA, gl.UNSIGNED_BYTE, data.pixels);
		gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, true);
		this.lookups.set(id, texture);
		return texture;
	}

	private drawPass(pass: RenderPass, frames: Map<number, Texture>, target: Texture | undefined, plan: RenderPlan, time: number) {
		const gl = this.gl;
		const program = this.program(pass);
		const location = (name: string) => {
			if (!program.uniforms.has(name)) program.uniforms.set(name, gl.getUniformLocation(program.program, name));
			return program.uniforms.get(name) ?? null;
		};

		gl.bindFramebuffer(gl.FRAMEBUFFER, target ? this.framebuffer : null);
		if (target) gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, target.texture, 0);
		gl.viewport(0, 0, pass.width, pass.height);
		if (pass.clear) { gl.clearColor(0, 0, 0, 0); gl.clear(gl.COLOR_BUFFER_BIT); }
		gl.useProgram(program.program);
		let index = 0;
		for (const [name, id] of Object.entries(pass.bindings)) {
			gl.activeTexture(gl.TEXTURE0 + index);
			const input = id < 0 ? this.getLookup(id) : frames.get(id)!;
			gl.bindTexture(gl.TEXTURE_2D, input.texture);
			gl.uniform1i(location(name), index++);
			if (name === 'inputFrame') gl.uniform2f(location('sourceSize'), input.width, input.height);
		}
		const p = pass.params ?? [];
		gl.uniform4f(location('params'), p[0] ?? 0, p[1] ?? 0, p[2] ?? 0, p[3] ?? 0);
		gl.uniform2f(location('outputSize'), pass.width, pass.height);
		gl.uniform1f(location('frameTime'), time);
		gl.uniform1f(location('reference'), Number(plan.compare));
		gl.drawArrays(gl.TRIANGLES, 0, 3);
	}

	draw(video: HTMLVideoElement, plan: RenderPlan) {
		const gl = this.gl;
		if (gl.isContextLost()) throw new Error('The WebGL2 context was lost');
		if (plan !== this.pipeline) {
			// Prepare all programs before modifying the displayed canvas.
			for (const pass of plan.passes) this.program(pass);
			if (!this.floats && plan.passes.some((pass) => pass.precise)) throw new Error('Floating-point render targets are unavailable');
			for (const entry of this.pool) gl.deleteTexture(entry.texture);
			this.pool.length = 0;
			this.pipeline = plan;
		}
		gl.activeTexture(gl.TEXTURE0);
		if (this.source?.width !== video.videoWidth || this.source.height !== video.videoHeight) {
			if (this.source) gl.deleteTexture(this.source.texture);
			this.source = this.createTexture({ width: video.videoWidth, height: video.videoHeight });
		}
		gl.bindTexture(gl.TEXTURE_2D, this.source.texture);
		gl.texSubImage2D(gl.TEXTURE_2D, 0, 0, 0, gl.RGBA, gl.UNSIGNED_BYTE, video);
		if (this.canvas.width !== plan.width) this.canvas.width = plan.width;
		if (this.canvas.height !== plan.height) this.canvas.height = plan.height;

		const frames = new Map<number, Texture>([[0, this.source]]);
		for (const [index, pass] of plan.passes.entries()) {
			const last = index === plan.passes.length - 1;
			const precise = !!pass.precise;
			let target: Texture | undefined;
			if (!last) {
				const inUse = new Set(frames.values());
				target = this.pool.find((entry) => entry.width === pass.width && entry.height === pass.height && entry.precise === precise && !inUse.has(entry));
				if (!target) { target = this.createTexture(pass, precise); this.pool.push(target); }
			}
			this.drawPass(pass, frames, target, plan, video.currentTime);
			for (const id of frames.keys()) if (plan.lastUse[id] <= index) frames.delete(id);
			if (target) frames.set(index + 1, target);
		}
	}

	destroy() {
		const gl = this.gl;
		if (this.source) gl.deleteTexture(this.source.texture);
		for (const item of [...this.pool, ...this.lookups.values()]) gl.deleteTexture(item.texture);
		for (const item of this.programs.values()) gl.deleteProgram(item.program);
		gl.deleteShader(this.vertex);
		gl.deleteFramebuffer(this.framebuffer);
	}
}
