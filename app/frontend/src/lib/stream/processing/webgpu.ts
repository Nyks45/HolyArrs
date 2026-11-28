import { browserGPU, gpuCanvas, type BindGroup, type Buffer, type GPUCanvas, type GPUDevice, type Pipeline, type Resource, type Texture } from './webgpu-api';
import { smaaTexture } from './smaa-textures';
import type { Size } from './mpv-shaders';
import type { RenderPass, RenderPlan } from './render-plan';

const shaders = import.meta.glob<string>('./wgsl/*.wgsl', { query: '?raw', import: 'default' });
const compute = 4;
const textureBinding = 4;
const storageBinding = 8;
const copyFromTexture = 1;
const mapRead = 1;
const drawTexture = 16;
const copyToTexture = 2;
const copyToBuffer = 8;
const uniformBuffer = 64;

const display = `
@group(0) @binding(0) var videoSampler: sampler;
@group(0) @binding(1) var frame: texture_2d<f32>;
struct VertexOut {
	@builtin(position) position: vec4<f32>,
	@location(0) uv: vec2<f32>,
}
@vertex fn triangle(@builtin(vertex_index) index: u32) -> VertexOut {
	let pos = vec2f(f32((index << 1) & 2u), f32(index & 2u));
	return VertexOut(vec4f(pos.x*2.0-1.0, 1.0-pos.y*2.0, 0.0, 1.0), pos);
}
@fragment fn show(in: VertexOut) -> @location(0) vec4f {
	return vec4f(textureSample(frame, videoSampler, in.uv).rgb, 1.0);
}`;

interface Frame extends Size { texture: Texture; view: Resource }
interface Compiled { pipeline: Pipeline; layout: Resource; usesSampler: boolean; slots: number[] }

export class WebGPUVideoRenderer {
	readonly name = 'WebGPU';
	readonly limit: number;
	private readonly pool: Frame[] = [];
	private readonly lookups = new Map<number, Frame>();
	private readonly pipelines = new Map<string, Promise<Compiled>>();
	private readonly sampler: Resource;
	private readonly uniforms: Buffer[] = [];
	private source?: Frame;
	private plan?: RenderPlan;
	private compiled: Compiled[] = [];
	private lost = false;
	private pending = false;
	private displayed?: Frame;
	private group = [8, 8];

	private constructor(private readonly canvas: HTMLCanvasElement, private readonly context: GPUCanvas, private readonly device: GPUDevice, private readonly present: Pipeline) {
		this.limit = device.limits.maxTextureDimension2D;
		this.sampler = device.createSampler({ magFilter: 'linear', minFilter: 'linear' });
		void device.lost.then(() => { this.lost = true; });
	}

	static async create(canvas: HTMLCanvasElement): Promise<WebGPUVideoRenderer | undefined> {
		const gpu = browserGPU();
		if (!gpu) return;
		const adapter = await gpu.requestAdapter({ powerPreference: 'high-performance' });
		if (!adapter) return;
		const device = await adapter.requestDevice();
		try {
			const module = device.createShaderModule({ code: display, label: 'Video presentation' });
			const format = gpu.getPreferredCanvasFormat();
			const present = await device.createRenderPipelineAsync({
				layout: 'auto', vertex: { module, entryPoint: 'triangle' }, fragment: { module, entryPoint: 'show', targets: [{ format }] }
			});
			const context = gpuCanvas(canvas);
			if (!context) throw new Error('WebGPU canvas is unavailable');
			context.configure({ device, format, alphaMode: 'opaque' });
			return new WebGPUVideoRenderer(canvas, context, device, present);
		} catch (error) { device.destroy(); throw error; }
	}

	private compile(pass: RenderPass, group: number[]) {
		const key = `${pass.key}:${group.join('x')}`;
		const cached = this.pipelines.get(key);
		if (cached) return cached;
		const source = shaders[`./wgsl/${pass.key.replace(':', '-')}.wgsl`];
		if (!source) throw new Error(`WebGPU shader is unavailable for ${pass.key}`);
		const promise = source().then((text) => this.createPipeline(pass, group, text));
		this.pipelines.set(key, promise);
		void promise.catch(() => this.pipelines.delete(key));
		return promise;
	}

	private createPipeline(pass: RenderPass, group: number[], source: string) {
		const code = source.replace('GROUP_X', String(group[0])).replace('GROUP_Y', String(group[1]));
		const bindings = [...code.matchAll(/@binding\((\d+)\)/g)].map(([, index]) => Number(index));
		const slots = bindings.filter((index) => index > 0 && index < 6);
		const usesSampler = bindings.includes(0);
		const device = this.device;
		const layout = device.createBindGroupLayout({ entries: [
			...(usesSampler ? [{ binding: 0, visibility: compute, sampler: { type: 'filtering' } }] : []),
			...slots.map((binding) => ({ binding, visibility: compute, texture: { sampleType: 'float' } })),
			{ binding: 6, visibility: compute, buffer: { type: 'uniform' } },
			{ binding: 7, visibility: compute, storageTexture: { access: 'write-only', format: 'rgba16float' } }
		] });
		const module = device.createShaderModule({ code, label: pass.key });
		return device.createComputePipelineAsync({ label: pass.key, layout: device.createPipelineLayout({ bindGroupLayouts: [layout] }), compute: { module, entryPoint: 'compute_main' } })
			.then((pipeline) => ({ pipeline, layout, slots, usesSampler }));
	}

	async prepare(plan: RenderPlan, workgroup: string) {
		const selected = workgroup.split('x').map(Number);
		const group = selected[0] * selected[1] <= this.device.limits.maxComputeInvocationsPerWorkgroup ? selected : [8, 8];
		const compiled = await Promise.all(plan.passes.map((pass) => this.compile(pass, group)));
		const required = new Set(plan.passes.map((pass) => `${pass.width}x${pass.height}`));
		for (let i = this.pool.length - 1; i >= 0; i--) {
			const item = this.pool[i];
			if (item !== this.displayed && !required.has(`${item.width}x${item.height}`)) { item.texture.destroy(); this.pool.splice(i, 1); }
		}
		this.compiled = compiled;
		this.group = group;
		this.plan = plan;
	}

	private getLookup(id: number) {
		const cached = this.lookups.get(id);
		if (cached) return cached;
		const data = smaaTexture(id);
		const result = this.allocate(data, true);
		this.device.queue.writeTexture({ texture: result.texture }, data.pixels, { bytesPerRow: 4 * data.width }, [data.width, data.height]);
		this.lookups.set(id, result);
		return result;
	}

	private allocate(size: Size, input: boolean): Frame {
		const texture = this.device.createTexture({
			size: [size.width, size.height],
			format: input ? 'rgba8unorm' : 'rgba16float',
			usage: textureBinding | (input ? copyToTexture | drawTexture : storageBinding | copyFromTexture)
		});
		return { width: size.width, height: size.height, texture, view: texture.createView() };
	}

	draw(video: HTMLVideoElement, plan: RenderPlan): boolean {
		if (this.lost) throw new Error('The WebGPU device was lost');
		if (this.pending || plan !== this.plan) return false;
		const device = this.device;
		if (this.source?.width !== video.videoWidth || this.source.height !== video.videoHeight) {
			this.source?.texture.destroy();
			this.source = this.allocate({ width: video.videoWidth, height: video.videoHeight }, true);
		}
		const input = this.source;
		device.queue.copyExternalImageToTexture({ source: video }, { texture: input.texture, colorSpace: 'srgb' }, [input.width, input.height]);
		if (this.canvas.width !== plan.width) this.canvas.width = plan.width;
		if (this.canvas.height !== plan.height) this.canvas.height = plan.height;

		const encoder = device.createCommandEncoder();
		const frames = new Map<number, Frame>([[0, input]]);
		let final = input;
		for (const [index, pass] of plan.passes.entries()) {
			const { pipeline, layout, slots, usesSampler } = this.compiled[index];
			const references = Object.values(pass.bindings).map((id) => id < 0 ? this.getLookup(id) : frames.get(id)!);
			const source = references[0];
			const inUse = new Set(frames.values());
			let target = this.pool.find((entry) => entry.width === pass.width && entry.height === pass.height && !inUse.has(entry));
			if (!target) { target = this.allocate(pass, false); this.pool.push(target); }
			const uniform = this.uniforms[index] ??= device.createBuffer({ size: 48, usage: copyToBuffer | uniformBuffer });
			const p = pass.params ?? [];
			device.queue.writeBuffer(uniform, 0, new Float32Array([p[0] ?? 0, p[1] ?? 0, p[2] ?? 0, p[3] ?? 0,
				source?.width ?? 1, source?.height ?? 1, pass.width, pass.height, Number(plan.compare), video.currentTime, 0, 0]));
			const entries: BindGroup[] = [
				...slots.map((binding) => ({ binding, resource: references[binding - 1].view })),
				{ binding: 6, resource: { buffer: uniform } }, { binding: 7, resource: target.view }
			];
			if (usesSampler) entries.push({ binding: 0, resource: this.sampler });
			const stage = encoder.beginComputePass();
			stage.setPipeline(pipeline);
			stage.setBindGroup(0, device.createBindGroup({ layout, entries }));
			stage.dispatchWorkgroups(Math.ceil(pass.width / this.group[0]), Math.ceil(pass.height / this.group[1]));
			stage.end();
			for (const id of frames.keys()) if (plan.lastUse[id] <= index) frames.delete(id);
			frames.set(index + 1, target);
			final = target;
		}
		const target = this.context.getCurrentTexture().createView();
		const stage = encoder.beginRenderPass({ colorAttachments: [{ view: target, clearValue: [0, 0, 0, 1], loadOp: 'clear', storeOp: 'store' }] });
		stage.setPipeline(this.present);
		stage.setBindGroup(0, device.createBindGroup({ layout: this.present.getBindGroupLayout(0), entries: [
			{ binding: 0, resource: this.sampler }, { binding: 1, resource: final.view }
		] }));
		stage.draw(3);
		stage.end();
		device.queue.submit([encoder.finish()]);
		this.displayed = final;
		this.pending = true;
		void device.queue.onSubmittedWorkDone().then(() => { this.pending = false; }, () => { this.lost = true; this.pending = false; });
		return true;
	}

	async capture(): Promise<HTMLCanvasElement | undefined> {
		const frame = this.displayed;
		if (!frame || this.lost) return;
		const { width, height, texture } = frame;
		const bytesPerRow = Math.ceil(width * 8 / 256) * 256;
		const readback = this.device.createBuffer({ size: bytesPerRow * height, usage: mapRead | copyToBuffer });
		try {
			const encoder = this.device.createCommandEncoder();
			encoder.copyTextureToBuffer({ texture }, { buffer: readback, bytesPerRow }, [width, height]);
			this.device.queue.submit([encoder.finish()]);
			await readback.mapAsync(mapRead);
			const data = new Uint16Array(readback.getMappedRange());
			const output = document.createElement('canvas');
			output.width = width; output.height = height;
			const context = output.getContext('2d');
			if (!context) return;
			const image = context.createImageData(width, height);
			for (let y = 0; y < height; y++) {
				let input = y * bytesPerRow / 2, output = y * width * 4;
				for (let x = 0; x < width; x++) {
					image.data[output++] = halfToColor(data[input++]);
					image.data[output++] = halfToColor(data[input++]);
					image.data[output++] = halfToColor(data[input++]);
					image.data[output++] = 255; input++;
				}
			}
			context.putImageData(image, 0, 0);
			return output;
		} finally { readback.destroy(); }
	}

	destroy() {
		this.context.unconfigure();
		this.device.destroy();
	}
}

function halfToColor(bits: number) {
	const exponent = (bits >> 10) & 31;
	if (bits & 0x8000) return 0;
	if (exponent >= 15) return 255;
	const mantissa = bits & 1023;
	return 255 * (exponent === 0 ? mantissa * 2 ** -24 : 2 ** (exponent - 15) * (1 + mantissa / 1024));
}
