import edgeSource from './smaa/edges.glsl?raw';
import weightsSource from './smaa/weights.glsl?raw';
import blendSource from './smaa/blend.glsl?raw';

export const smaaArea = -1;
export const smaaSearch = -2;
const edges = `vec4 vOffset[3];
void setup() {
	vec4 stepSize = (1.0 / outputSize).xyxy;
	vOffset[0] = uv.xyxy + stepSize * vec4(-1.0, 0.0, 0.0, -1.0);
	vOffset[1] = uv.xyxy + stepSize * vec4( 1.0, 0.0, 0.0,  1.0);
	vOffset[2] = uv.xyxy + stepSize * vec4(-2.0, 0.0, 0.0, -2.0);
}`;
const weights = `vec4 vOffset[3];
vec2 vPixCoord;
void setup() {
	vec2 stepSize = 1.0 / outputSize;
	vPixCoord = uv * outputSize;
	vOffset[0] = uv.xyxy + stepSize.xyxy * vec4(-0.25, -0.125, 1.25, -0.125);
	vOffset[1] = uv.xyxy + stepSize.xyxy * vec4(-0.125, -0.25, -0.125, 1.25);
	vOffset[2] = vec4(vOffset[0].xz, vOffset[1].yw) + stepSize.xxyy * vec4(-2.0, 2.0, -2.0, 2.0) * 16.0;
}`;
const blend = `vec4 vOffset;
void setup() {
	vOffset = uv.xyxy + (1.0 / outputSize).xyxy * vec4(1.0, 0.0, 0.0, 1.0);
}`;

const weightFunctions = 'SMAA(?:Search(?:Diag[12]|Length|XLeft|XRight|YUp|YDown)|Area(?:Diag)?|CalculateDiagWeights|Detect(?:Horizontal|Vertical)CornerPattern)';
function specialize(source: string, names: Record<string, string>) {
	for (const [local, texture] of Object.entries(names)) {
		source = source.replace(new RegExp(`(?:,\\s*)?sampler2D ${local}\\b(?:,\\s*)?`, 'g'), (match) =>
			match.trim().startsWith(',') && match.trim().endsWith(',') ? ', ' : '');
		source = source.replace(new RegExp(`(${weightFunctions}\\()(?:(?:edgesTex|areaTex|searchTex)\\s*,\\s*)+`, 'g'), '$1');
		source = source.replace(new RegExp(`\\b${local}\\b`, 'g'), texture);
	}
	return source;
}
function shader(source: string, setup: string, inputs: string[], names: Record<string, string> = {}) {
	let body = source
		.replace(/^\s*uniform (sampler2D|vec[24]) [^;]+;\s*$/gm, '')
		.replace(/^\s*varying .*;\s*$/gm, '')
		.replace('vec4 color;', 'vec4 color = vec4(0.0);')
		.replace('void main() {', 'void main() { setup();')
		.replaceAll('discard;', 'return vec2(0.0);')
		.replaceAll('gl_FragColor', 'result')
		.replaceAll('texture2D(', 'texture(')
		.replace('texture(tex, coord + offset * uTexelSize)', 'texture(uEdgesTexture, coord + offset * uTexelSize)');
	for (const input of inputs) {
		if (input === 'uColorTexture') body = body.replace(/SMAASampleColor\((?:colorTex|uColorTexture),\s*/g, 'SMAASampleColor(');
		if (input === 'uColorTexture') body = body.replace(/vOffset, uColorTexture/g, 'vOffset').replace(/offset\[3\], sampler2D colorTex/g, 'offset[3]');
		if (input === 'uEdgesTexture') body = body.replace(new RegExp(`(${weightFunctions}\\()(?:(?:uEdgesTexture|uAreaTexture|uSearchTexture)\\s*,\\s*)+`, 'g'), '$1');
	}
	body = specialize(body, names);
	return `#version 300 es
precision highp float;
precision highp int;
#define SMAA_EDGES_LUMA
#define vTexCoord0 uv
#define uViewportSize outputSize
#define uTexelSize (1.0/outputSize)
#define uSubsampleIndices vec4(0.0)
uniform vec2 outputSize;
${inputs.map((input) => `uniform sampler2D ${input};`).join('\n')}
in vec2 uv;
out vec4 result;
${setup}
${body}`;
}

export const smaaFragments = [
	shader(edgeSource, edges, ['uColorTexture'], { tex: 'uColorTexture', colorTex: 'uColorTexture' }),
	shader(weightsSource, weights, ['uEdgesTexture', 'uAreaTexture', 'uSearchTexture'], { edgesTex: 'uEdgesTexture', areaTex: 'uAreaTexture', searchTex: 'uSearchTexture' }),
	shader(blendSource, blend, ['uColorTexture', 'uBlendTexture'])
];
