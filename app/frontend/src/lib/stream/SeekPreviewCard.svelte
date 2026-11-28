<script lang="ts">
	import { onDestroy } from 'svelte';
	import { formatDuration } from './stream-api';

	let { source, seconds, chapter, percent }: { source: string; seconds: number; chapter: string; percent: number } = $props();
	let preview = $state<HTMLVideoElement>(), canvas = $state<HTMLCanvasElement>(), ready = $state(false);
	let lastTime = -Infinity;
	let currentSource = '';
	let request: ReturnType<typeof setTimeout> | undefined;

	$effect(() => {
		if (source !== currentSource) { ready = false; currentSource = source; lastTime = -Infinity; }
		const element = preview, at = seconds;
		if (!source || !element) return;
		clearTimeout(request);
		const jump = () => {
			if (!element.videoWidth || element.seeking || typeof element.duration !== 'number') return;
			if (Math.abs(element.currentTime - at) < .1) { paint(); return; }
			element.currentTime = Math.max(0, Math.min(at, Number.isFinite(element.duration) ? element.duration : at));
			lastTime = performance.now();
		};
		request = setTimeout(jump, Math.max(0, 135 - (performance.now() - lastTime)));
		return () => clearTimeout(request);
	});

	function paint() {
		if (!preview || !canvas || preview.readyState < 2 || Math.abs(preview.currentTime - seconds) > .8) return;
		const ctx = canvas.getContext('2d');
		if (!ctx) return;
		try { ctx.drawImage(preview, 0, 0, canvas.width, canvas.height); ready = true; }
		catch { ready = false; }
	}

	function loadLatest() {
		if (preview && Number.isFinite(preview.duration)) preview.currentTime = Math.min(seconds, preview.duration);
	}

	onDestroy(() => { clearTimeout(request); if (preview) { preview.removeAttribute('src'); preview.load(); } });
</script>

<div class="preview" class:ready style:left="clamp(84px, {percent}%, calc(100% - 84px))">
	{#if source}
		<video bind:this={preview} src={source} preload="metadata" muted playsinline crossorigin="anonymous" aria-hidden="true" onloadedmetadata={loadLatest} onseeked={() => { paint(); if (preview && Math.abs(preview.currentTime - seconds) > .8) loadLatest(); }} onerror={() => { ready = false; }}></video>
		<canvas bind:this={canvas} width="320" height="180" aria-hidden="true"></canvas>
	{/if}
	<span>{formatDuration(seconds)}</span>
	{#if chapter}<small>{chapter}</small>{/if}
</div>

<style>
	.preview { position: absolute; bottom: calc(100% + 5px); transform: translateX(-50%); z-index: 2; padding: 4px 9px; display: grid; text-align: center; pointer-events: none; max-width: 160px; width: max-content; overflow: hidden; color: #fff; background: #20222c; border: 1px solid #555660; border-radius: 9px; font-size: 12px; font-weight: 700; font-variant-numeric: tabular-nums; }
	.preview.ready { padding: 4px; } video, canvas { display: none; }
	.ready canvas { display: block; width: 150px; height: auto; margin-bottom: 3px; border-radius: 5px; }
	small { font-size: 10px; color: #d2d2df; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
