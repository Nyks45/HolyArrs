<script lang="ts">
	import Icon from '#lib/ui/Icon.svelte';
	import { shortcutHelp } from './player-shortcuts';
	import { formatDuration, formatSpeed, type SessionStatus } from './stream-api';

	import type { PlayerUtilityPanel } from './player-prefs';
	let { panel, video, session, position, pipeline, recoveries, error, boost, processing, canPip, canMpv, onOpen, onClose, onSave, onPip, onMpv, onKeep }: {
		panel: 'more' | 'stats' | 'shortcuts'; video?: HTMLVideoElement; session: SessionStatus | null; position: number;
		pipeline: string; recoveries: number; error: string; boost: number; processing: string; canPip: boolean; canMpv: boolean;
		onOpen: (panel: PlayerUtilityPanel) => void; onClose: () => void; onSave: (raw: boolean, captions: boolean) => void;
		onPip: () => void; onMpv: () => void; onKeep: () => void;
	} = $props();
	let totals = $state(0), dropped = $state(0), rate = $state(0), buffered = $state(0);
	let throughput = $state<number[]>([]);
	let errorsTab = $state(false);
	const title = $derived(panel === 'stats' ? 'Video stats' : panel === 'shortcuts' ? 'Keyboard shortcuts' : 'More options');
	const points = $derived.by(() => {
		const max = Math.max(...throughput, 1);
		return throughput.map((value, i) => `${i * 6},${26 - (value / max) * 24}`).join(' ');
	});
	$effect(() => {
		if (panel !== 'stats' || !video) return;
		const target = video;
		let lastFrames = target.getVideoPlaybackQuality()?.totalVideoFrames ?? 0;
		let lastTime = performance.now();
		const sample = () => {
			const quality = target.getVideoPlaybackQuality();
			const now = performance.now();
			totals = quality.totalVideoFrames;
			dropped = quality.droppedVideoFrames;
			rate = Math.max(0, Math.round((totals - lastFrames) / Math.max(.001, (now - lastTime) / 1000)));
			lastFrames = totals; lastTime = now;
			const ranges = target.buffered;
			buffered = ranges.length ? Math.max(0, ranges.end(ranges.length - 1) - target.currentTime) : 0;
			throughput = [...throughput.slice(-19), session?.download_bps ?? 0];
		};
		const timer = setInterval(sample, 500);
		return () => clearInterval(timer);
	});
</script>

<aside class="panel" class:stats={panel === 'stats'} class:keys={panel === 'shortcuts'} aria-label={title}>
	<header><h2>{title}</h2><button type="button" class="close" aria-label="Close panel" onclick={onClose}><Icon name="x" size={18} /></button></header>
	<div class="body">
		{#if panel === 'more'}
			<button type="button" onclick={() => onOpen('stats')}>Video stats <kbd>D</kbd></button>
			<button type="button" onclick={() => onOpen('shortcuts')}>Keyboard shortcuts <kbd>/</kbd></button>
			{#if canPip}<button type="button" onclick={onPip}>Picture in picture <kbd>I</kbd></button>{/if}
			{#if canMpv}<button type="button" onclick={onMpv}>Open in MPV</button>{/if}
			{#if session?.transport === 'torrent'}<button type="button" onclick={onKeep}>{session.keep_file ? 'Stop keeping download' : 'Keep download in Library'}</button>{/if}
			<h3>Screenshots</h3>
			<button type="button" onclick={() => onSave(false, false)}>Screenshot <kbd>S</kbd></button>
			<button type="button" onclick={() => onSave(false, true)}>With subtitles <kbd>Shift + S</kbd></button>
			<button type="button" onclick={() => onSave(true, false)}>Raw frame <kbd>Alt + S</kbd></button>
			<button type="button" onclick={() => onSave(true, true)}>Raw with subtitles <kbd>Alt + Shift + S</kbd></button>
		{:else if panel === 'shortcuts'}
			<dl>{#each shortcutHelp as [key, description] (key)}<dt>{description}</dt><dd><kbd>{key}</kbd></dd>{/each}</dl>
		{:else}
			<div class="tabs"><button type="button" aria-pressed={!errorsTab} onclick={() => { errorsTab = false; }}>Stats</button><button type="button" aria-pressed={errorsTab} onclick={() => { errorsTab = true; }}>Errors {error ? '(1)' : '(0)'}</button></div>
			{#if errorsTab}<p>{error || 'No playback errors in this session.'}</p>
			{:else}
				<dl>
					<dt>Frame size</dt><dd>{video?.videoWidth ?? 0} × {video?.videoHeight ?? 0}</dd>
					<dt>Codec</dt><dd>{session?.probe?.video_codec || '—'}</dd>
					<dt>Frame rate</dt><dd>{rate} fps</dd>
					<dt>Decoded / dropped</dt><dd>{totals} / {dropped}</dd>
					<dt>Playback</dt><dd>{pipeline.toUpperCase()} · {formatDuration(position)}</dd>
					<dt>Buffer / speed</dt><dd>{buffered.toFixed(1)}s / {video?.playbackRate ?? 1}×</dd>
					<dt>Source throughput</dt><dd>{formatSpeed(session?.download_bps ?? 0)}</dd>
					<dt>Peers / Seeders</dt><dd>{session?.peers ?? 0} / {session?.seeders ?? 0}</dd>
					<dt>Recoveries / Boost</dt><dd>{recoveries} / {boost}%</dd>
					{#if processing}<dt>Video processing</dt><dd>{processing}</dd>{/if}
				</dl>
				<svg class="spark" viewBox="0 0 116 28" role="img" aria-label="Recent source throughput"><polyline points={points} fill="none" stroke="#ff8195" stroke-width="2" vector-effect="non-scaling-stroke" /></svg>
			{/if}
		{/if}
	</div>
</aside>

<style>
	.panel { position: absolute; bottom: 122px; right: clamp(8px, 3vw, 36px); z-index: 22; width: min(360px, calc(100% - 16px)); max-height: min(70vh, 650px); display: flex; flex-direction: column; border: 1px solid rgb(255 255 255 / 12%); border-radius: 12px; background: #18181b; box-shadow: 0 16px 55px #000a; }
	.panel.stats { top: 62px; bottom: auto; left: 12px; right: auto; }
	.panel.keys { right: 50%; transform: translateX(50%); width: min(470px, calc(100% - 16px)); }
	header { display: flex; align-items: center; justify-content: space-between; padding: 8px 10px 8px 15px; border-bottom: 1px solid rgb(255 255 255 / 10%); }
	h2 { font-size: 15px; margin: 0; } h3 { font-size: 12px; margin: 14px 10px 4px; color: rgb(255 255 255 / 55%); }
	.body { padding: 6px; overflow: auto; }
	button { display: flex; width: 100%; min-height: 32px; align-items: center; justify-content: space-between; gap: 8px; border-radius: 7px; border: 1px solid transparent; padding: 5px 9px; background: transparent; color: inherit; font-size: 13px; cursor: pointer; }
	button:hover, button[aria-pressed='true'] { background: rgb(255 255 255 / 12%); }
	button:focus-visible { outline: 2px solid #ffffff; outline-offset: 1px; }
	button.close { width: 34px; justify-content: center; padding: 0; }
	kbd { background: #26262b; border: 1px solid rgb(255 255 255 / 18%); border-radius: 4px; padding: 2px 5px; white-space: nowrap; font-size: 11px; color: rgb(255 255 255 / 92%); }
	.tabs { display: flex; gap: 4px; } .tabs button { justify-content: center; }
	dl { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: baseline; gap: 8px 12px; margin: 10px; font-size: 12px; }
	dd { margin: 0; font-variant-numeric: tabular-nums; } p { padding: 4px 10px; font-size: 13px; }
	.spark { margin: 8px 3%; width: 94%; height: 38px; }
</style>
