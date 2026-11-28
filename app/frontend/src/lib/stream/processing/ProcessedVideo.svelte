<script lang="ts">
	import type { VideoFit } from '../player-prefs';
	import type { Category, ProcessingPrefs } from './presets';
	import type { RenderPlan } from './render-plan';

	let { video, fit, prefs, category, readyCanvas = $bindable(), capture = $bindable(), status = $bindable('') }: {
		video: HTMLVideoElement; fit: VideoFit; prefs: ProcessingPrefs; category: Category;
		readyCanvas?: HTMLCanvasElement; capture?: () => Promise<HTMLCanvasElement | undefined>; status?: string;
	} = $props();
	let gpuCanvas = $state<HTMLCanvasElement>(), glCanvas = $state<HTMLCanvasElement>();
	let rendered = $state<HTMLCanvasElement>();
	let invalidate: (() => void) | undefined;

	$effect(() => {
		const gpu = gpuCanvas, gl = glCanvas, source = video;
		if (!gpu || !gl) return;
		let cancelled = false, frame = 0, timer = 0;
		let generation = 0;
		let width = 0, height = 0;
		let plan: RenderPlan | undefined;
		let teardown: (() => void) | undefined;
		status = 'Preparing video processing';
		function schedule(draw: () => void) {
			if (cancelled || source.ended || frame || timer) return;
			if (typeof source.requestVideoFrameCallback === 'function') frame = source.requestVideoFrameCallback(() => { frame = 0; draw(); });
			else if (!source.paused) timer = requestAnimationFrame(() => { timer = 0; draw(); });
		}
		function stop() {
			if (frame) source.cancelVideoFrameCallback(frame);
			cancelAnimationFrame(timer);
			frame = timer = 0;
		}
		function activate(element: HTMLCanvasElement, name: string, width: number, height: number) {
			rendered = readyCanvas = element;
			const label = `${name} · ${width} × ${height}`;
			if (status !== label) status = label;
		}
		const start = async () => {
			const { makeRenderPlan } = await import('./render-plan');
			let previousError = '';
			const fail = (error: unknown) => {
				previousError = error instanceof Error ? error.message.split('\n')[0] : String(error);
			};
			const accelerated = 'gpu' in navigator ? await import('./webgpu').then(({ WebGPUVideoRenderer }) =>
				WebGPUVideoRenderer.create(gpu)).catch(fail) : undefined;
			if (cancelled) { accelerated?.destroy(); return; }
			const software = async () => {
				const { WebGLVideoRenderer } = await import('./webgl');
				if (cancelled) return;
				try {
					const renderer = new WebGLVideoRenderer(gl);
					use(gl, renderer);
				} catch (error) { fail(error); status = previousError; }
			};
			function use(element: HTMLCanvasElement, renderer: { name: string; limit: number; draw: (video: HTMLVideoElement, plan: RenderPlan) => boolean | void;
				prepare?: (plan: RenderPlan, group: string) => Promise<void>; capture?: () => Promise<HTMLCanvasElement | undefined>; destroy: () => void }) {
				teardown = () => renderer.destroy();
				let preparing = false;
				const paint = () => {
					if (cancelled || preparing) return;
					if (source.readyState < 2 || !source.videoWidth || !source.videoHeight) { schedule(paint); return; }
					const render = () => {
						if (cancelled || !plan) return;
						const drawn = renderer.draw(source, plan);
						if (drawn !== false) {
							if (!capture) capture = renderer.capture ? () => renderer.capture!() : async () => element;
							activate(element, renderer.name, plan.width, plan.height);
						}
						if (!source.paused) schedule(paint);
						else if (drawn === false && !timer) timer = requestAnimationFrame(() => { timer = 0; paint(); });
					};
					const onError = (error: unknown) => {
						fail(error);
						stop(); plan = undefined;
						if (renderer.name === 'WebGPU') {
							teardown?.(); teardown = undefined;
							rendered = readyCanvas = capture = undefined;
							void software();
						} else {
							rendered = readyCanvas = capture = undefined;
							status = previousError;
						}
					};
					try {
						if (!plan || width !== source.videoWidth || height !== source.videoHeight) {
							width = source.videoWidth; height = source.videoHeight;
							const scale = Math.min(window.devicePixelRatio || 1, 2);
							const revision = generation;
							const workgroup = prefs.upscaler.workgroup;
							preparing = true;
							void makeRenderPlan({ width, height }, { width: element.clientWidth * scale, height: element.clientHeight * scale }, prefs, category, renderer.limit)
								.then(async (next) => {
									if (!cancelled && revision === generation) await renderer.prepare?.(next, workgroup);
									return next;
								}).then((next) => {
									preparing = false;
									if (cancelled) return;
									if (revision !== generation) { paint(); return; }
									plan = next;
									render();
								}).catch((error) => { preparing = false; if (!cancelled) onError(error); });
							return;
						}
						render();
					} catch (error) { onError(error); }
				};
				invalidate = () => { plan = undefined; generation++; stop(); timer = requestAnimationFrame(() => { timer = 0; paint(); }); };
				paint();
			}
			if (accelerated) use(gpu, accelerated);
			else await software();
		};
		const redraw = () => invalidate?.();
		const reset = () => { rendered = readyCanvas = capture = undefined; plan = undefined; generation++; stop(); };
		const events = ['loadeddata', 'seeked', 'play', 'pause', 'resize'] as const;
		for (const event of events) source.addEventListener(event, redraw);
		source.addEventListener('emptied', reset);
		const observer = new ResizeObserver(redraw);
		observer.observe(gl);
		void start().catch(() => { if (!cancelled) status = 'Video processing could not start'; });
		return () => {
			cancelled = true;
			stop();
			for (const event of events) source.removeEventListener(event, redraw);
			source.removeEventListener('emptied', reset);
			observer.disconnect();
			invalidate = undefined;
			teardown?.();
			rendered = readyCanvas = capture = undefined; status = '';
		};
	});

	$effect(() => {
		$state.snapshot(prefs);
		if (category && invalidate) {
			status = 'Preparing video processing';
			invalidate();
		}
	});
</script>

<canvas bind:this={gpuCanvas} class:ready={!!rendered && rendered === gpuCanvas} data-renderer="webgpu" style:object-fit={fit} aria-hidden="true"></canvas>
<canvas bind:this={glCanvas} class:ready={!!rendered && rendered === glCanvas} data-renderer="webgl" style:object-fit={fit} aria-hidden="true"></canvas>

<style>
	canvas { position: absolute; inset: 0; width: 100%; height: 100%; visibility: hidden; pointer-events: none; background: #000; }
	canvas.ready { visibility: visible; }
</style>
