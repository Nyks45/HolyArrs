import { browserGPU, gpuCanvas } from './processing/webgpu-api';
import type { SubtitleRenderer } from './player-prefs';

interface Compositor { draw(source: HTMLCanvasElement): void; destroy(): void }
type GL = WebGLRenderingContext | WebGL2RenderingContext;

function glCompositor(output: HTMLCanvasElement, gl: GL, version: SubtitleRenderer): Compositor {
	const gl2 = version === 'webgl2';
	const vertex = gl.createShader(gl.VERTEX_SHADER)!;
	const fragment = gl.createShader(gl.FRAGMENT_SHADER)!;
	const program = gl.createProgram()!;
	const positions = gl.createBuffer();
	const texture = gl.createTexture();
	gl.shaderSource(vertex, gl2 ? `#version 300 es
in vec2 position; out vec2 uv; void main(){gl_Position=vec4(position,0,1);uv=position*0.5+0.5;}` :
		'attribute vec2 position; varying vec2 uv; void main(){gl_Position=vec4(position,0,1);uv=position*0.5+0.5;}');
	gl.shaderSource(fragment, gl2 ? `#version 300 es
precision mediump float; in vec2 uv; uniform sampler2D source; out vec4 color; void main(){color=texture(source,uv);}` :
		'precision mediump float; varying vec2 uv; uniform sampler2D source; void main(){gl_FragColor=texture2D(source,uv);}');
	gl.compileShader(vertex); gl.compileShader(fragment);
	gl.attachShader(program, vertex); gl.attachShader(program, fragment); gl.linkProgram(program);
	gl.deleteShader(vertex); gl.deleteShader(fragment);
	if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
		gl.deleteProgram(program); gl.deleteBuffer(positions); gl.deleteTexture(texture);
		throw new Error('Caption compositor could not start');
	}
	gl.useProgram(program);
	gl.bindBuffer(gl.ARRAY_BUFFER, positions);
	gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 3, -1, -1, 3]), gl.STATIC_DRAW);
	const attribute = gl.getAttribLocation(program, 'position');
	gl.enableVertexAttribArray(attribute); gl.vertexAttribPointer(attribute, 2, gl.FLOAT, false, 0, 0);
	gl.bindTexture(gl.TEXTURE_2D, texture);
	gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
	gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
	gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
	gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
	gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, true);
	gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, true);
	return {
		draw(source) {
			if (gl.isContextLost()) throw new Error('Caption compositor context was lost');
			if (output.width !== source.width) output.width = source.width;
			if (output.height !== source.height) output.height = source.height;
			gl.viewport(0, 0, source.width, source.height);
			gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, source);
			gl.drawArrays(gl.TRIANGLES, 0, 3);
		},
		destroy() { gl.deleteProgram(program); gl.deleteBuffer(positions); gl.deleteTexture(texture); }
	};
}

const overlay = `
struct VertexOut { @builtin(position) position: vec4f, @location(0) uv: vec2f }
@group(0) @binding(0) var frame: texture_2d<f32>;
@group(0) @binding(1) var samplerLinear: sampler;
@vertex fn vertex(@builtin(vertex_index) i: u32) -> VertexOut {
	let pos = vec2f(f32((i << 1) & 2u), f32(i & 2u));
	return VertexOut(vec4f(pos.x * 2.0 - 1.0, 1.0 - pos.y * 2.0, 0.0, 1.0), pos);
}
@fragment fn fragment(in: VertexOut) -> @location(0) vec4f {
	return textureSample(frame, samplerLinear, in.uv);
}`;

async function gpuCompositor(output: HTMLCanvasElement): Promise<Compositor | undefined> {
	const gpu = browserGPU();
	const adapter = await gpu?.requestAdapter();
	if (!gpu || !adapter) return;
	const device = await adapter.requestDevice();
	const context = gpuCanvas(output);
	if (!context) { device.destroy(); return; }
	try {
		const format = gpu.getPreferredCanvasFormat();
		const module = device.createShaderModule({ code: overlay, label: 'Subtitle compositor' });
		const pipeline = await device.createRenderPipelineAsync({ layout: 'auto', vertex: { module, entryPoint: 'vertex' },
			fragment: { module, entryPoint: 'fragment', targets: [{ format }] } });
		context.configure({ device, format, alphaMode: 'premultiplied' });
		const sampler = device.createSampler({ minFilter: 'linear', magFilter: 'linear' });
		let texture: ReturnType<typeof device.createTexture> | undefined;
		let width = 0, height = 0, lost = false;
		void device.lost.then(() => { lost = true; });
		return {
			draw(source) {
				if (lost) throw new Error('Caption compositor device was lost');
				if (!texture || width !== source.width || height !== source.height) {
					texture?.destroy();
					width = output.width = source.width; height = output.height = source.height;
					texture = device.createTexture({ size: [width, height], format: 'rgba8unorm', usage: 2 | 4 | 16 });
				}
				device.queue.copyExternalImageToTexture({ source }, { texture, premultipliedAlpha: true }, [width, height]);
				const commands = device.createCommandEncoder();
				const pass = commands.beginRenderPass({ colorAttachments: [{
					view: context.getCurrentTexture().createView(), loadOp: 'clear', storeOp: 'store', clearValue: [0, 0, 0, 0]
				}] });
				const bind = device.createBindGroup({ layout: pipeline.getBindGroupLayout(0), entries: [{ binding: 0, resource: texture.createView() }, { binding: 1, resource: sampler }] });
				pass.setPipeline(pipeline); pass.setBindGroup(0, bind); pass.draw(3); pass.end();
				device.queue.submit([commands.finish()]);
			},
			destroy() { texture?.destroy(); context.unconfigure(); device.destroy(); }
		};
	} catch (error) { device.destroy(); throw error; }
}

export async function createSubtitleCompositor(output: HTMLCanvasElement, renderer: SubtitleRenderer): Promise<Compositor | undefined> {
	if (renderer === 'webgpu') return gpuCompositor(output);
	if (renderer === 'canvas2d') return;
	const options = { alpha: true, premultipliedAlpha: true, antialias: false };
	const gl = output.getContext(renderer === 'webgl2' ? 'webgl2' : 'webgl', options) as GL | null;
	if (gl) return glCompositor(output, gl, renderer);
}
