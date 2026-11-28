import type { Filter, Stage } from './presets';

export const vertexShader = `#version 300 es
out vec2 uv;
void main() {
	vec2 pos = vec2((gl_VertexID << 1) & 2, gl_VertexID & 2);
	uv = pos;
	gl_Position = vec4(pos * 2.0 - 1.0, 0.0, 1.0);
}`;

const prelude = `#version 300 es
precision highp float;
uniform sampler2D inputFrame;
uniform sampler2D originalFrame;
uniform vec2 sourceSize;
uniform vec2 outputSize;
uniform vec4 params;
uniform float reference;
uniform float frameTime;
in vec2 uv;
out vec4 result;
vec3 sampleAt(vec2 offset) { return texture(inputFrame, uv + offset / sourceSize).rgb; }
float luma(vec3 color) { return dot(color, vec3(0.2126, 0.7152, 0.0722)); }
float hash(vec2 p) { return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453123); }
vec3 sourcePixel(vec2 p) { return texelFetch(inputFrame, clamp(ivec2(p), ivec2(0), ivec2(sourceSize) - 1), 0).rgb; }
`;

// EASU and RCAS adapted from AMD FidelityFX FSR 1.0. See shaders/LICENSE-FSR.
const easu = `
float edgeLuma(vec3 c) { return dot(c, vec3(0.5, 1.0, 0.5)); }
vec3 gradient(float a, float b, float c, float d, float e, float weight) {
	vec2 dir = vec2(d-b,e-a);
	vec2 range = max(abs(vec2(d-c,e-c)),abs(vec2(c-b,c-a)));
	vec2 slope = clamp(abs(dir)/max(range,0.000001),0.0,1.0);
	return vec3(dir,dot(slope,slope))*weight;
}
vec3 effect() {
	vec2 coord = uv*sourceSize - 0.5;
	vec2 base = floor(coord), f = fract(coord), inv = 1.0-f;
	vec3 b = sourcePixel(base + vec2(0,-1)), c = sourcePixel(base + vec2(1,-1));
	vec3 e = sourcePixel(base + vec2(-1,0)), g = sourcePixel(base + vec2(1,0));
	vec3 v = sourcePixel(base), h = sourcePixel(base + vec2(2,0));
	vec3 i = sourcePixel(base + vec2(-1,1)), j = sourcePixel(base + vec2(0,1));
	vec3 k = sourcePixel(base + vec2(1,1)), l = sourcePixel(base + vec2(2,1));
	vec3 n = sourcePixel(base + vec2(0,2)), o = sourcePixel(base + vec2(1,2));
	vec3 edge = gradient(edgeLuma(b),edgeLuma(e),edgeLuma(v),edgeLuma(g),edgeLuma(j),inv.x*inv.y)
		+ gradient(edgeLuma(c),edgeLuma(v),edgeLuma(g),edgeLuma(h),edgeLuma(k),f.x*inv.y)
		+ gradient(edgeLuma(v),edgeLuma(i),edgeLuma(j),edgeLuma(k),edgeLuma(n),inv.x*f.y)
		+ gradient(edgeLuma(g),edgeLuma(j),edgeLuma(k),edgeLuma(l),edgeLuma(o),f.x*f.y);
	vec2 dir = dot(edge.xy,edge.xy)<(1.0/32768.0) ? vec2(1,0) : normalize(edge.xy);
	float len = edge.z*0.5; len *= len;
	float stretch = 1.0/max(abs(dir.x),abs(dir.y));
	vec2 shape = vec2(1.0+(stretch-1.0)*len, 1.0-0.5*len);
	float lobe = 0.5+((0.25-0.04)-0.5)*len;
	const vec2 offsets[12] = vec2[12](vec2(0,-1),vec2(1,-1),vec2(-1,0),vec2(0,0),vec2(1,0),vec2(2,0),vec2(-1,1),vec2(0,1),vec2(1,1),vec2(2,1),vec2(0,2),vec2(1,2));
	vec3 samples[12] = vec3[12](b,c,e,v,g,h,i,j,k,l,n,o);
	vec3 accum = vec3(0); float weight = 0.0;
	for (int tap=0; tap<12; tap++) {
		vec2 d = offsets[tap]-f;
		vec2 r = vec2(dot(d,dir),dot(d,vec2(-dir.y,dir.x)))*shape;
		float squared = min(dot(r,r),1.0/lobe);
		float a = lobe*squared-1.0, z = 0.4*squared-1.0;
		float w = a*a*(1.5625*z*z-0.5625);
		accum+=samples[tap]*w; weight+=w;
	}
	return clamp(accum/max(weight,0.0001),min(min(v,g),min(j,k)),max(max(v,g),max(j,k)));
}`;

const rcas = `
vec3 effect() {
	vec3 c = sampleAt(vec2(0)), a = sampleAt(vec2(0,-1)), b = sampleAt(vec2(-1,0));
	vec3 d = sampleAt(vec2(1,0)), e = sampleAt(vec2(0,1));
	vec3 lo = min(min(a,b),min(d,e)), hi = max(max(a,b),max(d,e));
	vec3 hitMin = min(lo,c)/max(4.0*hi,vec3(0.0001));
	vec3 hitMax = -(1.0-max(hi,c))/max(4.0*(1.0-lo),vec3(0.0001));
	vec3 l = max(-hitMin,hitMax);
	float lobe = max(-0.1875,min(max(max(l.r,l.g),l.b),0.0))*params.x;
	return (c+lobe*(a+b+d+e))/(1.0+4.0*lobe);
}`;

const lanczos = `
float adSinc(float x) { float p=3.1415926536*x; return abs(x)<0.0001 ? 1.0 : sin(p)/p; }
float kernel(float x) { return abs(x)<3.0 ? adSinc(x)*adSinc(x/3.0) : 0.0; }
`;
const bicubic = `
float kernel(float offset) {
	float t=abs(offset);
	if (t<=1.0) return (1.5*t-2.5)*t*t+1.0;
	if (t<2.0) return ((-0.5*t+2.5)*t-4.0)*t+2.0;
	return 0.0;
}`;
const resample = (radius: number) => `
vec3 effect() {
	vec2 pos=uv*sourceSize-0.5; vec2 base=floor(pos), q=fract(pos);
	vec3 sum=vec3(0); float w=0.0;
	for (int y=${1-radius}; y<=${radius}; y++) for (int x=${1-radius}; x<=${radius}; x++) {
		vec2 d=vec2(x,y); float s=kernel(d.x-q.x)*kernel(d.y-q.y);
		sum+=sourcePixel(base+d)*s; w+=s;
	}
	return sum/max(w,0.001);
}`;

const inline = (body: string) => `vec3 effect(){vec3 c=sampleAt(vec2(0)); ${body}}`;
const neighbours = 'vec3 cross=(sampleAt(vec2(-1,0))+sampleAt(vec2(1,0))+sampleAt(vec2(0,-1))+sampleAt(vec2(0,1)))*0.25;';
const denoise = inline(`vec3 sum=c; float weight=1.0;
for (int y=-2;y<=2;y++) for(int x=-2;x<=2;x++) { if(x==0&&y==0)continue; vec2 delta=vec2(x,y); vec3 v=sampleAt(delta);
float w=exp(-dot(delta,delta)*0.35 - pow(abs(luma(v)-luma(c))*10.0,2.0)); sum+=v*w; weight+=w; } return sum/weight;`);
const cas = inline(`
vec3 a=sampleAt(vec2(-1,0)), b=sampleAt(vec2(0,-1)), d=sampleAt(vec2(1,0)), e=sampleAt(vec2(0,1));
vec3 lo=min(min(min(a,b),min(d,e)),c), hi=max(max(max(a,b),max(d,e)),c);
vec3 weight=-sqrt(clamp(min(lo,1.0-hi)/max(hi,vec3(0.0001)),0.0,1.0))*mix(0.125,0.20,params.x);
return (c+(a+b+d+e)*weight)/(1.0+4.0*weight);`);
const antiAlias = inline(`
float a=luma(sampleAt(vec2(-1,-1))), b=luma(sampleAt(vec2(1,-1))), d=luma(sampleAt(vec2(-1,1))), e=luma(sampleAt(vec2(1,1)));
float center=luma(c), lo=min(center,min(min(a,b),min(d,e))), hi=max(center,max(max(a,b),max(d,e)));
if(hi-lo<max(0.0312,hi*0.125)) return c;
vec2 dir=vec2(-((a+b)-(d+e)),(a+d)-(b+e));
float reduction=max((a+b+d+e)*0.03125,0.0078125);
dir=clamp(dir/(min(abs(dir.x),abs(dir.y))+reduction),-8.0,8.0);
vec3 inner=(sampleAt(dir*(-1.0/6.0))+sampleAt(dir*(1.0/6.0)))*0.5;
vec3 wide=inner*0.5+(sampleAt(-dir*0.5)+sampleAt(dir*0.5))*0.25;
return luma(wide)<lo||luma(wide)>hi ? inner : wide;`);

const effects: Partial<Record<Stage | Filter | 'present' | 'luma_input' | 'luma_output', string>> = {
	fsr_easu: easu, fsr_rcas: rcas, lanczos3_2x: lanczos + resample(3), bicubic_2x: bicubic + resample(2),
	denoise, deblur: inline(`${neighbours} return c+(c-cross)*0.25;`), cas_sharpen: cas, sharpening: cas, fxaa: antiAlias,
	auto_downscale: inline('return (sampleAt(vec2(-0.5,-0.5))+sampleAt(vec2(0.5,-0.5))+sampleAt(vec2(-0.5,0.5))+sampleAt(vec2(0.5,0.5)))*0.25;'),
	color: inline('c=(c-0.5)*(params.y/100.0)+0.5+params.x/100.0; c=mix(vec3(luma(c)),c,params.z/100.0); return pow(max(c,vec3(0)),vec3(1.0/max(0.1,params.w)));'),
	localContrast: inline('float r=params.y; vec3 s=(sampleAt(vec2(r,0))+sampleAt(vec2(-r,0))+sampleAt(vec2(0,r))+sampleAt(vec2(0,-r)))*0.25; return c+vec3((luma(c)-luma(s))*params.x/100.0);'),
	aberration: inline('vec2 d=(uv-0.5)*dot(uv-0.5,uv-0.5)*params.x*0.00035; return vec3(texture(inputFrame,uv+d).r,c.g,texture(inputFrame,uv-d).b);'),
	grain: inline('vec2 p=floor(uv*outputSize/max(params.y,0.5))+floor(frameTime*24.0); float n=hash(p)-0.5; vec3 g=params.z>0.5 ? vec3(n,hash(p+19.1)-0.5,hash(p+53.2)-0.5) : vec3(n); return c+g*params.x*0.004*(1.0-abs(luma(c)-0.5));'),
	debanding: inline(`vec3 sum=c; float count=1.0;
for(int i=0;i<6;i++){if(float(i)>=params.z) break; float angle=6.283185*hash(floor(uv*sourceSize)+float(i)*19.35); vec2 d=vec2(cos(angle),sin(angle))*params.y; vec3 a=(sampleAt(d)+sampleAt(-d))*0.5;
if(abs(luma(a)-luma(c))<params.x/255.0*0.25){sum+=a;count+=1.0;}} return sum/count+(hash(gl_FragCoord.xy)-0.5)*params.w*0.0007;`),
	dithering: inline(`vec2 p=floor(gl_FragCoord.xy); float n=hash(p);
if(params.y==1.0){n=0.0;for(int i=0;i<3;i++){vec2 bit=mod(floor(p/exp2(float(i))),2.0);n+=mod(2.0*bit.x+3.0*bit.y,4.0)*pow(0.25,float(i+1));}}
if(params.y==2.0) n=0.5+0.5*(n-hash(p+vec2(1,0)));
if(params.y==3.0) n=0.5*(n+hash(p+31.1));
if(params.y==4.0) n=fract(52.9829189*fract(dot(p,vec2(0.06711056,0.00583715))));
if(params.y==5.0) n=fract(0.754877666*p.x+0.569840296*p.y);
return c+vec3((n-0.5)*params.x/255.0);`),
	luma_input: inline('return vec3(luma(c));'),
	luma_output: inline('vec3 rgb=texture(originalFrame,uv).rgb; return rgb+c.r-luma(rgb);'),
	present: 'vec3 effect(){ if(reference>0.5 && abs(uv.x-0.5)<0.0008) return vec3(1.0); return reference>0.5&&uv.x<0.5 ? texture(originalFrame,uv).rgb : texture(inputFrame,uv).rgb; }'
};

export function effectFragment(stage: Stage | Filter | 'present' | 'luma_input' | 'luma_output'): string {
	const effect = effects[stage];
	if (!effect) throw new Error(`The selected processing stage is unavailable: ${stage}`);
	return `${prelude}\n${effect}\nvoid main(){result=vec4(clamp(effect(),0.0,1.0),1.0);}`;
}
