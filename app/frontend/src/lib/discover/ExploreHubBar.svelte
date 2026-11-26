<script lang="ts">
	import Icon from '#lib/ui/Icon.svelte';
	import type { ExploreHubInfo } from './explore-hub.svelte.ts';

	let {
		hubs = [],
		activeId = '',
		activeInfo = null,
		onpick
	}: {
		hubs: ExploreHubInfo[];
		activeId: string;
		activeInfo: ExploreHubInfo | null;
		onpick: (id: string | null) => void;
	} = $props();
</script>

<div class="hub-strip" data-testid="explore-hub-bar" role="navigation" aria-label="Category hubs">
	<div class="hub-scroll">
		{#each hubs as h (h.id)}
			<button
				type="button"
				class="hub-pill accent-{h.accent}"
				class:active={activeId === h.id}
				class:spotlight={h.group === 'spotlight'}
				aria-pressed={activeId === h.id}
				title={h.tagline}
				onclick={() => onpick(h.id)}
			>
				{#if h.id === 'anime'}<Icon name="sparkle" size={14} />{/if}
				<span>{h.name}</span>
			</button>
		{/each}
	</div>
	{#if activeId && activeInfo}
		<div class="hub-banner" data-testid="explore-hub-banner">
			<div class="hub-banner-copy">
				<h2>{activeInfo.name}</h2>
				<p>{activeInfo.tagline}</p>
			</div>
			<button type="button" class="hub-clear" onclick={() => onpick(null)}>
				<Icon name="x" size={14} />
				All categories
			</button>
		</div>
	{/if}
</div>

<style>
	.hub-strip { display: flex; flex-direction: column; gap: 10px; margin-top: -16px; }
	.hub-scroll { display: flex; flex-wrap: nowrap; gap: 8px; overflow-x: auto; padding: 4px 2px 6px; scrollbar-width: none; overscroll-behavior-x: contain; }
	.hub-scroll::-webkit-scrollbar { display: none; }
	.hub-pill {
		flex: none; display: inline-flex; align-items: center; gap: 6px; min-height: 34px; padding: 0 14px;
		border-radius: 999px; border: 1px solid var(--line); background: color-mix(in srgb, var(--surface-2) 78%, transparent);
		color: var(--text-muted); font: 600 13px/1 var(--font-ui); cursor: pointer; white-space: nowrap;
		transition: background-color var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out), border-color var(--dur-fast) var(--ease-out), transform var(--dur-fast) var(--ease-out);
	}
	.hub-pill:hover { color: var(--text); border-color: var(--text-faint); transform: translateY(-1px); }
	.hub-pill.spotlight:not(.active) { border-color: color-mix(in srgb, #f43f5e 45%, var(--line)); color: var(--text); background: color-mix(in srgb, #f43f5e 10%, var(--surface-2)); }
	.hub-pill.active { background: var(--text); color: var(--bg); border-color: var(--text); }
	.hub-pill.accent-rose.active { background: #e11d48; color: #fff; border-color: #e11d48; }
	.hub-pill.accent-violet.active { background: #7c3aed; color: #fff; border-color: #7c3aed; }
	.hub-pill.accent-red.active { background: #dc2626; color: #fff; border-color: #dc2626; }
	.hub-pill.accent-amber.active { background: #d97706; color: #fff; border-color: #d97706; }
	.hub-pill.accent-cyan.active { background: #0891b2; color: #fff; border-color: #0891b2; }
	.hub-pill.accent-emerald.active { background: #059669; color: #fff; border-color: #059669; }
	.hub-banner {
		display: flex; align-items: center; justify-content: space-between; gap: 16px; flex-wrap: wrap;
		padding: 12px 16px; border-radius: 14px; border: 1px solid var(--line);
		background: color-mix(in srgb, var(--surface-2) 68%, transparent);
	}
	.hub-banner-copy h2 { margin: 0; font: 700 20px/1.2 var(--font-ui); color: var(--text); }
	.hub-banner-copy p { margin: 3px 0 0; font: 400 13px/1.35 var(--font-ui); color: var(--text-muted); }
	.hub-clear {
		display: inline-flex; align-items: center; gap: 6px; min-height: 34px; padding: 0 12px;
		border-radius: 999px; border: 1px solid var(--line); background: var(--surface-1); color: var(--text);
		font: 600 12.5px/1 var(--font-ui); cursor: pointer;
	}
	.hub-clear:hover { border-color: var(--text-faint); }
	@media (pointer: coarse) { .hub-pill { min-height: 40px; } }
</style>
