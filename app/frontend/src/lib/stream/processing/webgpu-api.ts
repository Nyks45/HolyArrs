// Browser WebGPU interfaces used by the video renderer.
type Descriptor = Record<string, unknown>;
interface Resource { readonly label: string }
interface Pipeline extends Resource { getBindGroupLayout(index: number): Resource }
interface Texture extends Resource { createView(): Resource; destroy(): void }
interface Buffer extends Resource {
	mapAsync(mode: number): Promise<void>;
	getMappedRange(): ArrayBuffer;
	unmap(): void;
	destroy(): void;
}
interface BindGroup { binding: number; resource: Resource | { buffer: Buffer } }
interface ComputePass {
	setPipeline(pipeline: Pipeline): void;
	setBindGroup(index: number, bindings: Resource): void;
	dispatchWorkgroups(x: number, y: number): void;
	end(): void;
}
interface RenderPass {
	setPipeline(pipeline: Pipeline): void;
	setBindGroup(index: number, bindings: Resource): void;
	draw(vertexCount: number): void;
	end(): void;
}
interface Encoder {
	copyTextureToBuffer(source: Descriptor, destination: Descriptor, size: number[]): void;
	beginComputePass(): ComputePass;
	beginRenderPass(options: Descriptor): RenderPass;
	finish(): Resource;
}
interface GPUDevice {
	readonly limits: { maxTextureDimension2D: number; maxComputeInvocationsPerWorkgroup: number };
	readonly lost: Promise<unknown>;
	readonly queue: {
		copyExternalImageToTexture(source: Descriptor, target: Descriptor, size: number[]): void;
		writeTexture(target: Descriptor, data: Uint8Array<ArrayBuffer>, layout: Descriptor, size: number[]): void;
		writeBuffer(buffer: Buffer, offset: number, data: Float32Array<ArrayBuffer>): void;
		submit(commands: Resource[]): void;
		onSubmittedWorkDone(): Promise<void>;
	};
	createBindGroupLayout(descriptor: { entries: Descriptor[] }): Resource;
	createPipelineLayout(descriptor: { bindGroupLayouts: Resource[] }): Resource;
	createBindGroup(descriptor: { layout: Resource; entries: BindGroup[] }): Resource;
	createTexture(options: Descriptor): Texture;
	createBuffer(options: Descriptor): Buffer;
	createSampler(options: Descriptor): Resource;
	createShaderModule(options: { code: string; label?: string }): Resource;
	createComputePipelineAsync(options: Descriptor): Promise<Pipeline>;
	createRenderPipelineAsync(options: Descriptor): Promise<Pipeline>;
	createCommandEncoder(): Encoder;
	destroy(): void;
}
interface GPUAdapter { requestDevice(): Promise<GPUDevice> }
interface GPUDriver {
	requestAdapter(options?: { powerPreference: string }): Promise<GPUAdapter | null>;
	getPreferredCanvasFormat(): string;
}
interface GPUCanvas {
	configure(options: { device: GPUDevice; format: string; alphaMode: 'opaque' | 'premultiplied' }): void;
	getCurrentTexture(): Texture;
	unconfigure(): void;
}

export function browserGPU(): GPUDriver | undefined {
	return (navigator as Navigator & { gpu?: GPUDriver }).gpu;
}
export function gpuCanvas(canvas: HTMLCanvasElement): GPUCanvas | null {
	return (canvas as HTMLCanvasElement & { getContext(type: 'webgpu'): GPUCanvas | null }).getContext('webgpu');
}
export type { BindGroup, Buffer, GPUCanvas, GPUDevice, Pipeline, Resource, Texture };
