<script lang="ts">
	import type { TitleDetails } from '#lib/metadata/index.ts';
	import type { PlayerUtilityPanel, VideoFit } from '../player-prefs';
	import ProcessedVideo from './ProcessedVideo.svelte';
	import VideoProcessingPanel from './VideoProcessingPanel.svelte';
	import { detectContentCategory, filterOrder, loadProcessingPrefs, saveProcessingPrefs } from './presets';

	let { video, details, fit, panel, onTab, onClose, canvas = $bindable(), capture = $bindable(), status = $bindable('') }: {
		video?: HTMLVideoElement; details: TitleDetails | null; fit: VideoFit; panel: PlayerUtilityPanel;
		onTab: (next: 'filters' | 'upscaler') => void; onClose: () => void;
		canvas?: HTMLCanvasElement; capture?: () => Promise<HTMLCanvasElement | undefined>; status?: string;
	} = $props();
	let prefs = $state(loadProcessingPrefs());
	const active = $derived(prefs.upscaler.enabled || (prefs.filtersOn && filterOrder.some((key) => prefs.filters[key].enabled)));
	const category = $derived(detectContentCategory(details));
</script>

{#if video && active}
	<ProcessedVideo {video} {fit} {prefs} {category} bind:readyCanvas={canvas} bind:capture bind:status />
{/if}
{#if panel === 'filters' || panel === 'upscaler'}
	<VideoProcessingPanel view={panel} {prefs} {category} {status} webgpu={canvas?.dataset.renderer === 'webgpu'} onChange={(next) => { prefs = next; saveProcessingPrefs(next); }} {onTab} {onClose} />
{/if}
