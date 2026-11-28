<script lang="ts">
	import { onMount } from 'svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import { addPlayerFont, loadPlayerFonts, removePlayerFont, type PlayerFont } from './player-fonts';
	import { resolveScopedSubtitleStyle, subtitleScopeKey, type PlayerDevicePrefs, type ScopedSubtitleStyle, type SubtitleContext, type SubtitleScope, type SubtitleRenderer, type SubtitleStyleOverride } from './player-prefs';

	let { context, device, styles, onChange, onClose }: {
		context: SubtitleContext; device: PlayerDevicePrefs; styles: string[];
		onChange: (patch: Partial<PlayerDevicePrefs>) => void; onClose: () => void;
	} = $props();
	let scope = $state<SubtitleScope>('title');
	let fonts = $state<PlayerFont[]>([]);
	let error = $state('');
	const key = $derived(subtitleScopeKey(scope, context));
	const applied = $derived(resolveScopedSubtitleStyle(device.subtitles, context));
	const selected = $derived(device.subtitles[key] ?? {});
	const titleLabel = $derived(context.kind === 'movie' ? 'Movie' : 'Show');

	onMount(() => { void loadPlayerFonts().then((choices) => { fonts = choices; }).catch(() => {}); });

	function patch(value: ScopedSubtitleStyle) { onChange({ subtitles: { ...device.subtitles, [key]: { ...selected, ...value } } }); }
	function patchStyle(name: string, value: SubtitleStyleOverride) {
		patch({ styles: { ...selected.styles, [name]: { ...selected.styles?.[name], ...value } } });
	}
	function reset() {
		const next = { ...device.subtitles };
		delete next[key];
		onChange({ subtitles: next });
	}
	async function importFont(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		const file = input.files?.[0]; input.value = '';
		if (!file) return;
		error = '';
		try { fonts = [...fonts, await addPlayerFont(file)]; }
		catch (cause) { error = cause instanceof Error ? cause.message : 'The font could not be loaded.'; }
	}
	async function removeFont(id: string) {
		error = '';
		try { await removePlayerFont(id); fonts = fonts.filter((font) => font.id !== id); }
		catch { error = 'The font could not be removed.'; }
	}
</script>

<aside class="panel" aria-label="Scoped subtitle style">
	<header><h2>Subtitle style</h2><button type="button" aria-label="Close subtitle style" onclick={onClose}><Icon name="x" size={18} /></button></header>
	<div class="body">
		<fieldset><legend>Apply to</legend><div class="scopes">
			{#each [['global', 'Global'], ['title', titleLabel], ...(context.kind === 'show' ? [['episode', 'Episode']] : [])] as [value, label] (value)}
				<button type="button" aria-pressed={scope === value} onclick={() => { scope = value as SubtitleScope; }}>{label}</button>
			{/each}
		</div></fieldset>
		<label><span>Font scale ({selected.scale ?? applied.scale}%)</span><input type="range" min="50" max="200" value={selected.scale ?? applied.scale} oninput={(e) => patch({ scale: Number(e.currentTarget.value) })} /></label>
		<label><span>Vertical position ({selected.offset ?? applied.offset}px)</span><input type="range" min="-150" max="150" value={selected.offset ?? applied.offset} oninput={(e) => patch({ offset: Number(e.currentTarget.value) })} /></label>
		<label><span>Blend fix</span><input type="checkbox" checked={device.blendFix} onchange={(event) => onChange({ blendFix: event.currentTarget.checked })} /></label>
		<label><span>Shift overlapping cues</span><input type="checkbox" checked={device.overlap} onchange={(event) => onChange({ overlap: event.currentTarget.checked })} /></label>
		{#if styles.length}
			<h3>Track styles</h3>
			{#each styles as name (name)}
				<fieldset><legend>{name}</legend>
					<label><span>Font</span><select value={selected.styles?.[name]?.font ?? applied.styles[name]?.font ?? ''} onchange={(e) => patchStyle(name, { font: e.currentTarget.value })}><option value="">Track font</option>{#each fonts as font (font.id)}<option value={font.family}>{font.family}</option>{/each}</select></label>
					<label><span>Scale</span><input type="range" min="50" max="200" value={selected.styles?.[name]?.scale ?? applied.styles[name]?.scale ?? 100} oninput={(e) => patchStyle(name, { scale: Number(e.currentTarget.value) })} /></label>
					<label><span>Hide</span><input type="checkbox" checked={selected.styles?.[name]?.hidden ?? applied.styles[name]?.hidden ?? false} onchange={(e) => patchStyle(name, { hidden: e.currentTarget.checked })} /></label>
				</fieldset>
			{/each}
		{/if}
		<label><span>Renderer</span><select aria-label="Renderer" value={device.renderer} onchange={(e) => onChange({ renderer: e.currentTarget.value as SubtitleRenderer })}><option value="canvas2d">Canvas 2D</option><option value="webgl1">WebGL 1</option><option value="webgl2">WebGL 2</option><option value="webgpu">WebGPU</option></select></label>
		<h3>Custom fonts</h3>
		<label><span>Add font</span><input class="file" type="file" accept=".ttf,.otf,.woff,.woff2" onchange={(event) => void importFont(event)} /></label>
		{#each fonts as font (font.id)}<div class="font"><span>{font.family}</span><button type="button" onclick={() => void removeFont(font.id)}>Remove</button></div>{/each}
		{#if error}<p role="alert">{error}</p>{/if}
	</div>
	<footer><button type="button" onclick={reset}>Reset {scope === 'title' ? titleLabel.toLowerCase() : scope} style</button><button type="button" onclick={onClose}>Done</button></footer>
</aside>

<style>
	.panel { position: absolute; z-index: 24; left: 50%; top: 50%; transform: translate(-50%, -50%); width: min(495px, calc(100% - 18px)); max-height: 84vh; display: flex; flex-direction: column; background: #18181b; border: 1px solid rgb(255 255 255 / 10%); border-radius: 15px; box-shadow: 0 20px 65px #000b; }
	header, footer { display: flex; align-items: center; justify-content: space-between; padding: 9px 14px; border-bottom: 1px solid rgb(255 255 255 / 12%); }
	footer { border: 0; border-top: 1px solid rgb(255 255 255 / 12%); }
	h2, h3 { margin: 4px 0; font-size: 15px; } h3 { font-size: 13px; margin-top: 14px; }
	.body { overflow: auto; padding: 10px 14px; }
	label, .font { display: flex; justify-content: space-between; gap: 12px; min-height: 34px; align-items: center; font-size: 13px; }
	fieldset { padding: 5px 9px; margin: 6px 0; border: 1px solid rgb(255 255 255 / 12%); border-radius: 8px; }
	legend { font-size: 12px; color: rgb(255 255 255 / 85%); }
	.scopes { display: flex; gap: 5px; }
	button, select { padding: 5px 9px; color: #fff; background: #26262b; border: 1px solid rgb(255 255 255 / 14%); border-radius: 6px; cursor: pointer; font: inherit; font-size: 12.5px; }
	.file { max-width: 220px; font: inherit; font-size: 12px; color: rgb(255 255 255 / 65%); }
	.file::file-selector-button { margin-right: 8px; padding: 5px 9px; color: #fff; background: #26262b; border: 1px solid rgb(255 255 255 / 14%); border-radius: 6px; font: inherit; cursor: pointer; }
	button[aria-pressed='true'] { border-color: rgb(255 255 255 / 55%); background: rgb(255 255 255 / 12%); }
	button:focus-visible, select:focus-visible, input:focus-visible { outline: 2px solid #ffffff; outline-offset: 2px; }
	input { accent-color: #ffffff; max-width: 52%; }
	p { color: #ffffff; font-size: 13px; }
</style>
