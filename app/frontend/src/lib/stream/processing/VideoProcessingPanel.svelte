<script lang="ts">
	import Icon from '#lib/ui/Icon.svelte';
	import { animation, defaultProcessingPrefs, ditherTypes, effectivePreset, effectiveStages, filterControls, filterOrder, general, outputs, presets, stages, type Category, type Filter, type OutputSize, type Preset, type ProcessingPrefs, type Stage, type Upscaler } from './presets';

	type View = 'filters' | 'upscaler';
	let { view, prefs, category, status, webgpu, onChange, onTab, onClose }: {
		view: View; prefs: ProcessingPrefs; category: Category; status: string; webgpu: boolean;
		onChange: (prefs: ProcessingPrefs) => void; onTab: (view: View) => void; onClose: () => void;
	} = $props();
	let filterOpen = $state<Filter | null>(null);
	let nextStage = $state<Stage>('fsr_easu');
	const up = $derived(prefs.upscaler);
	const changeUpscaler = (patch: Partial<Upscaler>) => onChange({ ...prefs, upscaler: { ...up, ...patch } });
	const templates = ['fsr', 'fsr_denoise', 'cinema_sharp', ...animation] as const;
	const choose = (preset: Preset) => changeUpscaler({ enabled: true, mode: 'preset', preset });
	const changeFilter = (filter: Filter, entry: ProcessingPrefs['filters'][Filter]) => onChange({ ...prefs, filters: { ...prefs.filters, [filter]: entry } });
	const nudge = (i: number, dir: number) => {
		const chain = [...up.chain];
		[chain[i], chain[i + dir]] = [chain[i + dir], chain[i]];
		changeUpscaler({ chain });
	};
</script>

<aside class="processing" aria-label="Video processing">
	<header>
		<h2>Video processing</h2>
		<button class="icon" type="button" aria-label="Close video processing" onclick={onClose}><Icon name="x" size={18} /></button>
	</header>
	<nav aria-label="Processing options">
		<button type="button" aria-pressed={view === 'filters'} onclick={() => onTab('filters')}>Filters</button>
		<button type="button" aria-pressed={view === 'upscaler'} onclick={() => onTab('upscaler')}>Upscaler</button>
	</nav>
	<div class="body">
		{#if view === 'filters'}
			<label class="row">Enable filters <input type="checkbox" checked={prefs.filtersOn} onchange={(e) => onChange({ ...prefs, filtersOn: e.currentTarget.checked })} /></label>
			{#each filterOrder as filter (filter)}
				<div class="filter">
					<div class="row">
						<button type="button" aria-expanded={filterOpen === filter} onclick={() => { filterOpen = filterOpen === filter ? null : filter; }}>{filterControls[filter].name} <Icon name="chevronRight" size={14} /></button>
						<input type="checkbox" aria-label={`Enable ${filterControls[filter].name}`} checked={prefs.filters[filter].enabled} onchange={(e) => changeFilter(filter, { ...prefs.filters[filter], enabled: e.currentTarget.checked })} />
					</div>
					{#if filterOpen === filter}
						{#each filterControls[filter].controls as [name, min, max, step], index (name)}
							<label class="slider"><span>{name} <output>{prefs.filters[filter].values[index]}</output></span><input type="range" {min} {max} {step} value={prefs.filters[filter].values[index]} oninput={(e) => {
								const values = [...prefs.filters[filter].values]; values[index] = e.currentTarget.valueAsNumber; changeFilter(filter, { ...prefs.filters[filter], values });
							}} /></label>
						{/each}
						{#if filter === 'color'}<button type="button" class="reset" onclick={() => changeFilter('color', { ...prefs.filters.color, values: defaultProcessingPrefs().filters.color.values })}>Reset color</button>{/if}
						{#if filter === 'dithering'}<label class="row">Pattern <select value={prefs.dither} onchange={(e) => onChange({ ...prefs, dither: Number(e.currentTarget.value) })}>{#each ditherTypes as name, i (name)}<option value={i}>{name}</option>{/each}</select></label>{/if}
						{#if filter === 'grain'}<label class="row">Colored grain <input type="checkbox" checked={prefs.coloredGrain} onchange={(e) => onChange({ ...prefs, coloredGrain: e.currentTarget.checked })} /></label>{/if}
					{/if}
				</div>
			{/each}
			<button type="button" class="reset" onclick={() => { const defaults = defaultProcessingPrefs(); onChange({ ...prefs, filters: defaults.filters, filtersOn: false, coloredGrain: defaults.coloredGrain, dither: defaults.dither }); }}>Reset filters</button>
		{:else}
			<label class="row">Enable upscaler <input type="checkbox" checked={up.enabled} onchange={(e) => changeUpscaler({ enabled: e.currentTarget.checked })} /></label>
			<nav aria-label="Upscaler mode">
				<button type="button" aria-pressed={up.mode === 'preset'} onclick={() => changeUpscaler({ mode: 'preset' })}>Presets</button>
				<button type="button" aria-pressed={up.mode === 'advanced'} onclick={() => changeUpscaler({ mode: 'advanced', chain: up.chain.length ? up.chain : ['fsr_easu', 'fsr_rcas'] })}>Advanced</button>
			</nav>
			{#if up.mode === 'preset'}
				<h3>General</h3>
				<div class="grid">{#each general as preset (preset)}<button type="button" aria-pressed={up.preset === preset} onclick={() => choose(preset)}>{presets[preset]}</button>{/each}</div>
				{#if up.preset === 'auto'}<p>Auto selects {presets[effectivePreset('auto', category)]} for this title.</p>{/if}
				<h3>Animation</h3>
				<div class="grid">{#each animation as preset (preset)}<button type="button" aria-pressed={up.preset === preset} onclick={() => choose(preset)}>{presets[preset]}</button>{/each}</div>
			{:else}
				<h3>Templates</h3>
				<div class="grid">{#each templates as preset (preset)}<button type="button" onclick={() => changeUpscaler({ enabled: true, chain: effectiveStages({ ...up, enabled: true, mode: 'preset', preset, darken: false, thin: false }, category) })}>{presets[preset]}</button>{/each}<button type="button" onclick={() => changeUpscaler({ chain: [] })}>Clear chain</button></div>
				<h3>Processing chain</h3>
				<ol>
					{#each up.chain as stage, i (i)}
						<li><span>{stages[stage]}</span>
							<button type="button" disabled={i === 0} aria-label={`Move ${stages[stage]} up`} onclick={() => nudge(i, -1)}>↑</button>
							<button type="button" disabled={i === up.chain.length - 1} aria-label={`Move ${stages[stage]} down`} onclick={() => nudge(i, 1)}>↓</button>
							<button type="button" aria-label={`Remove ${stages[stage]}`} onclick={() => changeUpscaler({ chain: up.chain.filter((_, index) => index !== i) })}><Icon name="x" size={14} /></button>
						</li>
					{/each}
				</ol>
				<div class="row"><select aria-label="Processing stage" bind:value={nextStage}>{#each Object.entries(stages) as [key, title] (key)}<option value={key}>{title}</option>{/each}</select><button type="button" disabled={up.chain.length >= 16} onclick={() => changeUpscaler({ enabled: true, chain: [...up.chain, nextStage] })}>Add stage</button></div>
				{#if webgpu}
					<label class="row">Workgroup size <select value={up.workgroup} onchange={(e) => changeUpscaler({ workgroup: e.currentTarget.value as Upscaler['workgroup'] })}><option value="8x8">8 × 8</option><option value="16x16">16 × 16</option><option value="32x8">32 × 8</option></select></label>
				{/if}
			{/if}
			<label class="row">Output resolution <select value={up.output} onchange={(e) => changeUpscaler({ output: e.currentTarget.value as OutputSize })}>{#each Object.entries(outputs) as [key, title] (key)}<option value={key}>{title}</option>{/each}</select></label>
			<label class="slider"><span>Sharpness <output>{Math.round(up.sharpness * 100)}%</output></span><input type="range" min="0" max="1" step=".01" value={up.sharpness} oninput={(e) => changeUpscaler({ sharpness: e.currentTarget.valueAsNumber })} /></label>
			<label class="row">Darken lines <input type="checkbox" checked={up.darken} onchange={(e) => changeUpscaler({ darken: e.currentTarget.checked })} /></label>
			<label class="row">Thin lines <input type="checkbox" checked={up.thin} onchange={(e) => changeUpscaler({ thin: e.currentTarget.checked })} /></label>
			<button type="button" class="reset" onclick={() => changeUpscaler(defaultProcessingPrefs().upscaler)}>Reset upscaler</button>
		{/if}
	</div>
	<footer>
		<label class="row">Compare original and processed <input type="checkbox" checked={prefs.compare} onchange={(e) => onChange({ ...prefs, compare: e.currentTarget.checked })} /></label>
		{#if status}<p aria-live="polite">{status}</p>{/if}
	</footer>
</aside>

<style>
	.processing { position: absolute; z-index: 21; right: clamp(8px, 3vw, 36px); bottom: 90px; width: min(400px, calc(100% - 16px)); max-height: min(70vh, 660px); display: flex; flex-direction: column; border: 1px solid #42424c; border-radius: 16px; background: #191b23; box-shadow: 0 14px 45px #0009; color: #fff; font-size: 13px; }
	header { display: flex; justify-content: space-between; align-items: center; padding: 9px 12px; border-bottom: 1px solid #3d3e49; }
	h2 { font-size: 15px; margin: 0; } h3 { margin: 12px 8px 5px; color: #d1d3de; font-size: 12px; }
	nav { display: flex; gap: 5px; padding: 6px 9px; }
	nav button { flex: 1; }
	.body { overflow: auto; padding: 3px 7px; }
	footer { border-top: 1px solid #3d3e49; padding: 3px 7px; }
	.row { display: flex; align-items: center; justify-content: space-between; gap: 8px; min-height: 39px; padding: 4px 8px; }
	button, select { color: inherit; border: 1px solid #454653; border-radius: 7px; background: #262832; min-height: 33px; padding: 4px 7px; cursor: pointer; }
	button:hover { background: #363746; }
	button[aria-pressed='true'] { background: #3d2631; border-color: #b85265; }
	button:disabled { opacity: .45; cursor: default; }
	button:focus-visible, input:focus-visible, select:focus-visible { outline: 2px solid #f292a1; outline-offset: 2px; }
	.icon { display: grid; place-items: center; min-width: 34px; }
	.filter { border-top: 1px solid #353641; }
	.filter button { min-height: 34px; display: flex; justify-content: space-between; align-items: center; flex: 1; gap: 8px; background: transparent; border-color: transparent; padding-left: 0; }
	input { accent-color: #eb6379; }
	.slider { display: block; padding: 5px 8px; } .slider span { display: flex; justify-content: space-between; } .slider input { width: 100%; margin: 5px 0 0; }
	.grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 4px; padding: 0 7px; }
	ol { margin: 0; padding: 0 7px; list-style: none; }
	li { display: flex; align-items: center; gap: 4px; margin: 4px 0; } li span { flex: 1; } li button { display: grid; place-items: center; min-width: 28px; }
	.reset { margin: 5px 8px; }
	p { margin: 3px 8px 8px; color: #caced9; overflow-wrap: anywhere; }
</style>
