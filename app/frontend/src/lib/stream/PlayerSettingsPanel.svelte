<script lang="ts">
	import Icon from '#lib/ui/Icon.svelte';
	import type { PlayerDevicePrefs, VideoFit } from './player-prefs';
	import { message } from '#lib/core/api.ts';
	import type { StreamSourceCheck } from './stream-api';
	import type { StreamPrefs } from './streaming-config-api';
	import type { HlsQualityLevel } from './player-media';

	type View = 'home' | 'playback' | 'quality' | 'speed' | 'servers';
	type Quality = 'original' | '1080p' | '720p' | '480p';
	let { device, prefs, speed, remote, canTranscode, levels, level, quality, source, sourceContext, onTestSources, onDevice, onPrefs, onSpeed, onLevel, onQuality, onOpen, onClose }: {
		device: PlayerDevicePrefs; prefs: StreamPrefs | null; speed: number; remote: boolean; canTranscode: boolean;
		levels: HlsQualityLevel[]; level: number; quality: Quality; source: string; sourceContext: string; onTestSources: () => Promise<StreamSourceCheck[]>;
		onDevice: (patch: Partial<PlayerDevicePrefs>) => void; onPrefs: (patch: Partial<StreamPrefs>) => void;
		onSpeed: (value: number) => void; onLevel: (value: number) => void; onQuality: (value: Quality) => void;
		onOpen: (panel: 'sources' | 'audio' | 'subtitles' | 'filters' | 'upscaler') => void; onClose: () => void;
	} = $props();
	let view = $state<View>('home');
	const titles = { home: 'Settings', playback: 'Playback preferences', quality: 'Quality', speed: 'Speed', servers: 'Source / Server' };
	const speeds = [.25, .5, .75, 1, 1.25, 1.5, 1.75, 2];
	const modes: { value: VideoFit; label: string }[] = [{ value: 'contain', label: 'Fit' }, { value: 'cover', label: 'Crop' }, { value: 'fill', label: 'Stretch' }];
	const qualities: Quality[] = ['original', '1080p', '720p', '480p'];
	let results = $state<StreamSourceCheck[] | undefined>();
	let testing = $state(false), testError = $state('');
	let revision = 0;
	$effect(() => {
		if (sourceContext) { revision++; results = undefined; testing = false; testError = ''; }
	});
	async function testSources() {
		const attempt = ++revision;
		testing = true; testError = '';
		try {
			const checks = await onTestSources();
			if (attempt === revision) results = checks;
		} catch (error) {
			if (attempt === revision) testError = message(error);
		} finally { if (attempt === revision) testing = false; }
	}

</script>

<aside class="panel" aria-label={titles[view]}>
	<header>
		{#if view !== 'home'}<button class="icon" type="button" aria-label="Back to settings" onclick={() => { view = 'home'; }}><Icon name="back" size={17} /></button>{/if}
		<h2>{titles[view]}</h2>
		<button class="icon close" type="button" aria-label="Close settings" onclick={onClose}><Icon name="x" size={17} /></button>
	</header>
	<div class="body">
		{#if view === 'home'}
			<button class="row" type="button" onclick={() => { view = 'playback'; }}>Playback preferences <Icon name="chevronRight" size={15} /></button>
			<div class="pair"><span>Video fit</span><div class="choices">{#each modes as mode (mode.value)}<button type="button" aria-pressed={device.fit === mode.value} onclick={() => onDevice({ fit: mode.value })}>{mode.label}</button>{/each}</div></div>
			<button class="row" type="button" onclick={() => { view = 'servers'; }}>Source / Server <Icon name="chevronRight" size={15} /></button>
			<button class="row" type="button" onclick={() => { view = 'quality'; }}>Quality <Icon name="chevronRight" size={15} /></button>
			<button class="row" type="button" onclick={() => { view = 'speed'; }}>Speed <span>{speed}× <Icon name="chevronRight" size={15} /></span></button>
			<button class="row" type="button" onclick={() => onOpen('audio')}>Audio <Icon name="chevronRight" size={15} /></button>
			<button class="row" type="button" onclick={() => onOpen('subtitles')}>Subtitles <Icon name="chevronRight" size={15} /></button>
			<button class="row" type="button" onclick={() => onOpen('filters')}>Video filters <Icon name="chevronRight" size={15} /></button>
			<button class="row" type="button" onclick={() => onOpen('upscaler')}>Upscaler <Icon name="chevronRight" size={15} /></button>
		{:else if view === 'playback'}
			<label><span>Auto start</span><input type="checkbox" checked={prefs?.auto_play_fastest ?? true} onchange={(e) => onPrefs({ auto_play_fastest: e.currentTarget.checked })} /></label>
			<label><span>Pause when away</span><input type="checkbox" checked={device.pauseAway} onchange={(e) => onDevice({ pauseAway: e.currentTarget.checked })} /></label>
			<label><span>Auto next episode</span><input type="checkbox" checked={device.autoNext} onchange={(e) => onDevice({ autoNext: e.currentTarget.checked })} /></label>
			<label><span>Auto skip intro</span><input type="checkbox" checked={prefs?.auto_skip_intro} onchange={(e) => onPrefs({ auto_skip_intro: e.currentTarget.checked })} /></label>
			<label><span>Auto skip recap</span><input type="checkbox" checked={prefs?.auto_skip_recap} onchange={(e) => onPrefs({ auto_skip_recap: e.currentTarget.checked })} /></label>
			<label><span>Auto skip outro</span><input type="checkbox" checked={prefs?.auto_skip_outro} onchange={(e) => onPrefs({ auto_skip_outro: e.currentTarget.checked })} /></label>
			<h3>AV1 compatibility</h3>
			<label><span>Force AV1 transcoding</span><input type="checkbox" checked={device.forceAv1} onchange={(e) => onDevice({ forceAv1: e.currentTarget.checked })} /></label>
			<label><span>H.264 quality</span><select value={device.h264Quality} onchange={(e) => onDevice({ h264Quality: e.currentTarget.value === 'high' ? 'high' : 'balanced' })}><option value="balanced">Balanced</option><option value="high">High</option></select></label>
			<label><span>Fullscreen type</span><select value={device.fullscreen} onchange={(e) => onDevice({ fullscreen: e.currentTarget.value === 'document' ? 'document' : 'player' })}><option value="player">Player</option><option value="document">Document</option></select></label>
		{:else if view === 'quality'}
			{#if levels.length > 1}
				<button class="row" type="button" aria-pressed={level === -1} onclick={() => onLevel(-1)}>Auto</button>
				{#each levels as item (item.index)}<button class="row" type="button" aria-pressed={level === item.index} onclick={() => onLevel(item.index)}>{item.label}</button>{/each}
			{:else if !remote && canTranscode}
				{#each qualities as item (item)}<button class="row" type="button" aria-pressed={quality === item} onclick={() => onQuality(item)}>{item === 'original' ? 'Auto / Original' : item}</button>{/each}
			{:else}
				<p>Original quality is supplied by the source.</p>
			{/if}
		{:else if view === 'speed'}
			{#each speeds as item (item)}<button class="row" type="button" aria-pressed={speed === item} onclick={() => onSpeed(item)}>{item === 1 ? 'Normal' : `${item}×`}</button>{/each}
		{:else}
			<p>{source || 'No active source'}</p>
			<button class="row" type="button" disabled={testing} onclick={() => void testSources()}>{testing ? 'Testing sources…' : 'Test all'}</button>
			{#if testError}<p role="alert">{testError}</p>{/if}
			{#if results}
				<div aria-live="polite">
					{#each results as result (result.id)}
						<div class="pair"><span>{result.name}</span><span>{result.paused ? 'Paused' : result.answered ? `${result.ms} ms · ${result.count} results` : result.health || 'Unavailable'}</span></div>
					{:else}<p>No configured servers reported a result.</p>{/each}
				</div>
			{/if}
			<button class="row" type="button" onclick={() => onOpen('sources')}>All sources and torrents <Icon name="chevronRight" size={15} /></button>
		{/if}
	</div>
</aside>

<style>
	.panel { position: absolute; z-index: 20; right: clamp(8px, 3vw, 36px); bottom: 122px; width: min(360px, calc(100% - 16px)); max-height: min(68vh, 570px); display: flex; flex-direction: column; border: 1px solid rgb(255 255 255 / 10%); border-radius: 12px; background: #18181b; box-shadow: 0 14px 45px #0009; color: #fff; }
	header { display: flex; align-items: center; gap: 8px; padding: 9px 12px; border-bottom: 1px solid rgb(255 255 255 / 10%); }
	h2 { margin: 0; font-size: 15px; font-weight: 700; } h3 { font-size: 12px; color: rgb(255 255 255 / 50%); margin: 14px 8px 2px; }
	.body { overflow: auto; padding: 6px; }
	.row, label, .pair { display: flex; align-items: center; justify-content: space-between; gap: 10px; min-height: 38px; width: 100%; padding: 6px 9px; font-size: 13px; }
	.row > span { display: flex; align-items: center; gap: 8px; color: rgb(255 255 255 / 70%); }
	button, select { background: transparent; color: inherit; border-radius: 7px; border: 1px solid transparent; cursor: pointer; }
	.row:hover, .icon:hover, .choices button:hover { background: #26262b; }
	button[aria-pressed='true'] { background: rgb(255 255 255 / 12%); border-color: rgb(255 255 255 / 55%); }
	button:focus-visible, input:focus-visible, select:focus-visible { outline: 2px solid #ffffff; outline-offset: 2px; }
	.choices { display: flex; gap: 3px; } .choices button { padding: 5px 7px; white-space: nowrap; }
	.icon { display: grid; place-items: center; min-width: 33px; min-height: 33px; } .close { margin-left: auto; }
	input { accent-color: #ffffff; } select { background: #26262b; padding: 5px; }
	p { margin: 12px 8px; color: rgb(255 255 255 / 70%); font-size: 13px; }
</style>
