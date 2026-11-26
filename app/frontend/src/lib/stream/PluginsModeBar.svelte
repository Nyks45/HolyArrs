<script lang="ts">
	import { onMount } from 'svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import { listPluginRepos, listStremioAddons } from './streaming-config-api.ts';

	let { mode = $bindable<'app' | 'streaming'>('app') }: { mode?: 'app' | 'streaming' } = $props();

	let addonCount = $state<number | null>(null);
	let scraperCount = $state<number | null>(null);

	async function loadCounts() {
		try {
			const [addonsRes, reposRes] = await Promise.all([listStremioAddons(), listPluginRepos()]);
			addonCount = (addonsRes.addons ?? []).filter((a) => a.enabled).length;
			scraperCount = (reposRes.repos ?? [])
				.filter((r) => r.enabled)
				.reduce((sum, r) => sum + (r.scrapers ?? []).filter((s) => s.enabled).length, 0);
		} catch {
			/* Counts are optional when running without streaming permissions. */
		}
	}

	function pickMode(next: 'app' | 'streaming') {
		mode = next;
		const url = new URL(window.location.href);
		if (next === 'streaming') url.searchParams.set('tab', 'streaming');
		else url.searchParams.delete('tab');
		history.replaceState(history.state, '', url.pathname + url.search + url.hash);
		if (next === 'streaming') void loadCounts();
	}

	onMount(() => {
		const sp = new URLSearchParams(window.location.search);
		if (sp.get('tab') === 'streaming') mode = 'streaming';
		void loadCounts();
	});
</script>

<div class="mode-bar" role="tablist" aria-label="Plugin sections" data-testid="plugins-mode-bar">
	<button
		type="button"
		role="tab"
		aria-selected={mode === 'app'}
		class="mode-btn"
		class:active={mode === 'app'}
		onclick={() => pickMode('app')}
		data-testid="plugins-mode-app"
	>
		<Icon name="grid" size={16} />
		<span>App Plugins</span>
	</button>
	<button
		type="button"
		role="tab"
		aria-selected={mode === 'streaming'}
		class="mode-btn"
		class:active={mode === 'streaming'}
		onclick={() => pickMode('streaming')}
		data-testid="plugins-mode-streaming"
	>
		<Icon name="play" size={16} />
		<span>Streaming &amp; Nuvio Plugins</span>
		{#if addonCount !== null && scraperCount !== null}
			<span class="count-pill">{addonCount} addons · {scraperCount} scrapers</span>
		{/if}
	</button>
</div>

<style>
	.mode-bar {
		display: inline-flex;
		flex-wrap: wrap;
		gap: 6px;
		padding: 5px;
		margin: 0 0 18px;
		border-radius: 14px;
		border: 1px solid var(--line);
		background: color-mix(in srgb, var(--surface-1) 82%, transparent);
	}
	.mode-btn {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		min-height: 38px;
		padding: 0 14px;
		border-radius: 10px;
		border: 1px solid transparent;
		background: transparent;
		color: var(--text-muted);
		font: 600 13px/1 var(--font-ui);
		cursor: pointer;
	}
	.mode-btn:hover { color: var(--text); }
	.mode-btn.active {
		background: var(--surface-2);
		border-color: var(--line-strong);
		color: var(--text);
		box-shadow: 0 2px 10px rgb(0 0 0 / 0.16);
	}
	.count-pill {
		padding: 3px 8px;
		border-radius: 999px;
		background: color-mix(in srgb, var(--accent) 16%, transparent);
		color: var(--accent);
		font-size: 11px;
		font-weight: 700;
	}
</style>
