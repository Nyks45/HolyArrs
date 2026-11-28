<script lang="ts">
	import { onMount } from 'svelte';
	import { drawAssTrack, videoRect, type VideoRect } from './ass-renderer';
	import type { AssDocument } from './ass-parser';
	import type { ResolvedSubtitleStyle } from './subtitle-style';
	import type { ScopedSubtitleStyle, SubtitleRenderer, VideoFit } from './player-prefs';
	import type { VTTCue } from './vtt';

	let { canvas = $bindable<HTMLCanvasElement | undefined>(), video, fit, time, primary, secondary, primaryAss, secondaryAss, delay, base, scoped, size, raised, overlap, blendFix, renderer }: {
		canvas?: HTMLCanvasElement; video?: HTMLVideoElement; fit: VideoFit; time: number; primary: VTTCue[]; secondary: VTTCue[];
		primaryAss: AssDocument | null; secondaryAss: AssDocument | null; delay: number; base: ResolvedSubtitleStyle;
		scoped: Required<ScopedSubtitleStyle>; size: 'sm' | 'md' | 'lg' | 'xl'; raised: boolean; overlap: boolean; blendFix: boolean; renderer: SubtitleRenderer;
	} = $props();
	let width = $state(0), height = $state(0), sourceWidth = $state(0), sourceHeight = $state(0);
	let surface = $state<HTMLCanvasElement>(), accelerated = $state(false);
	let composite: ((source: HTMLCanvasElement) => void) | undefined;
	$effect(() => {
		const output = surface, mode = renderer;
		accelerated = false;
		if (!output || mode === 'canvas2d') return;
		let stopped = false;
		let dispose: (() => void) | undefined;
		void import('./subtitle-compositor').then(({ createSubtitleCompositor }) => createSubtitleCompositor(output, mode))
			.then((compositor) => {
				if (stopped) { compositor?.destroy(); return; }
				if (!compositor) return;
				dispose = () => compositor.destroy();
				composite = (source) => {
					try { compositor.draw(source); accelerated = true; }
					catch { accelerated = false; composite = undefined; }
				};
				if (canvas?.width && canvas.height) composite(canvas);
			}).catch(() => { if (!stopped) accelerated = false; });
		return () => { stopped = true; composite = undefined; dispose?.(); };
	});
	const scales = { sm: .8, md: 1, lg: 1.25, xl: 1.55 };

	onMount(() => {
		const node = canvas;
		if (!node) return;
		const resized = () => {
			width = node.clientWidth;
			height = node.clientHeight;
		};
		const mediaChanged = () => { sourceWidth = video?.videoWidth ?? 0; sourceHeight = video?.videoHeight ?? 0; };
		const observer = new ResizeObserver(resized);
		observer.observe(node);
		video?.addEventListener('resize', mediaChanged);
		resized(); mediaChanged();
		return () => { observer.disconnect(); video?.removeEventListener('resize', mediaChanged); };
	});

	function drawVtt(ctx: CanvasRenderingContext2D, cues: VTTCue[], box: VideoRect, top: boolean) {
		if (!cues.length) return;
		const fontSize = Math.max(13, Math.min(32, box.width * .022)) * scales[size] * scoped.scale / 100;
		ctx.font = `600 ${fontSize}px ${base.fontFamilyCss.replace('var(--font-ui), ', '')}`;
		ctx.textBaseline = 'top'; ctx.textAlign = 'center';
		const lines = cues.flatMap((cue) => cue.lines.flatMap((line) => {
			const rows: string[] = [];
			let row = '';
			for (const word of line.split(/\s+/)) {
				const next = row ? `${row} ${word}` : word;
				if (row && ctx.measureText(next).width > box.width * .84) { rows.push(row); row = word; }
				else row = next;
			}
			return [...rows, row];
		}));
		const step = fontSize * 1.35;
		const lifted = raised ? 72 : 0;
		const margin = Math.max(8, box.height * base.bottomPct / 100);
		const within = top ? box.y + margin : box.y + box.height - margin - lines.length * step - lifted;
		const y = Math.max(4, Math.min(height - lines.length * step, within - scoped.offset));
		const x = box.x + box.width / 2;
		for (let i = 0; i < lines.length; i++) {
			const w = Math.min(box.width * .86, ctx.measureText(lines[i]).width + 24);
			ctx.fillStyle = base.background;
			ctx.beginPath(); ctx.roundRect(x - w / 2, y + i * step - 2, w, step, 4); ctx.fill();
			ctx.fillStyle = base.color;
			ctx.fillText(lines[i], x, y + i * step, box.width * .84);
		}
	}

	$effect(() => {
		if (!canvas || !width || !height) return;
		const dpr = Math.max(1, Math.min(2, window.devicePixelRatio || 1));
		const w = Math.round(width * dpr), h = Math.round(height * dpr);
		if (canvas.width !== w || canvas.height !== h) { canvas.width = w; canvas.height = h; }
		const ctx = canvas.getContext('2d');
		if (!ctx) return;
		ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
		ctx.clearRect(0, 0, width, height);
		const box = videoRect(width, height, sourceWidth, sourceHeight, fit);
		ctx.save();
		ctx.beginPath(); ctx.rect(box.x, box.y, box.width, box.height); ctx.clip();
		if (primaryAss) drawAssTrack(ctx, primaryAss, time - delay / 1000, box, scoped, raised ? 64 : 0, overlap);
		else drawVtt(ctx, primary, box, false);
		if (secondaryAss) drawAssTrack(ctx, secondaryAss, time - delay / 1000, box, scoped, 0, overlap, true);
		else drawVtt(ctx, secondary, box, true);
		ctx.restore();
		composite?.(canvas);
	});
</script>

<canvas bind:this={canvas} class:blendFix class:invisible={accelerated} aria-hidden="true"></canvas>
{#if renderer !== 'canvas2d'}
	{#key renderer}
		<canvas bind:this={surface} class:blendFix class:invisible={!accelerated} data-caption-renderer={renderer} aria-hidden="true"></canvas>
	{/key}
{/if}

<style>
	canvas { position: absolute; inset: 0; width: 100%; height: 100%; z-index: 6; pointer-events: none; }
	canvas.invisible { visibility: hidden; }
	canvas.blendFix { mix-blend-mode: screen; }
</style>
