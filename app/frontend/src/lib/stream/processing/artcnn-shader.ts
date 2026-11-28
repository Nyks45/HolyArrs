// ArtCNN v1.6.2 stores its weights in mpv compute shaders. A fragment produces
// one output pixel, so it reads the convolution taps without the shared tile.
export function artCnnFragment(source: string): string {
	const depthToSpace = !source.includes('barrier();');
	if (depthToSpace) {
		return source.slice(source.indexOf('void hook()')).replace('void hook()', 'vec4 hook()')
			.replace('imageStore(out_image, ivec2(gl_GlobalInvocationID), clamp(result, 0.0, 1.0));', 'return clamp(result, 0.0, 1.0);');
	}

	const stride = source.includes('input_base = (base + ivec2(x,y) - offset) * ivec2(2, 2)') ? 2 : 1;
	const taps = [...source.matchAll(/inp\[(\d)\]\[y\]\[x\] = (.+);/g)].map((m) =>
		m[2].replace(/\b\w+_mul \* /g, '').replace(/texelFetch\((\w+)_raw, (input_base \+ ivec2\(\d, \d\)), 0\)/g, '$1_fetch($2)'));
	const results = [...source.matchAll(/imageStore\(out_image, [^;]+?, ((?:max\()?result\d[^;]*?)\);/g)].map((m) => m[1]);
	if (!taps.length || !results.length) throw new Error('Unrecognized ArtCNN convolution');

	const body = source.slice(source.indexOf('barrier();') + 'barrier();'.length, source.indexOf('const ivec2 output_base'))
		.replace(/const (F|V4) (inp_\d_\d_\d) = inp\[(\d)\]\[local_xy.y \+ (\d)\]\[local_xy.x \+ (\d)\];/g,
			(_, type: string, name: string, channel: string, y: string, x: string) =>
				`${type} ${name} = tap${channel}((base + ivec2(${Number(x) - 1}, ${Number(y) - 1})) * ${stride});`);
	const definitions = '#define F float\n#define V4 vec4\n#define M4 mat4\n';
	const readers = taps.map((tap, i) => `${stride === 1 ? 'float' : 'vec4'} tap${i}(ivec2 input_base) {return ${tap};}`).join('\n');
	const select = results.length === 1 ? `return ${results[0]};` :
		`${results.slice(0, -1).map((result, i) => `if (subpixel == ${i}) return ${result};`).join('\n')}\nreturn ${results.at(-1)};`;
	return `${definitions}${readers}
vec4 hook() {
	ivec2 pixel = ivec2(gl_FragCoord.xy);
	ivec2 base = pixel / ${results.length === 4 ? 2 : 1};
	int subpixel = (pixel.x % 2) + 2 * (pixel.y % 2);
	${body}
	${select}
}`;
}
