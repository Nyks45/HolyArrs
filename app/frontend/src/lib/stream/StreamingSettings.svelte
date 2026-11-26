<script lang="ts">
	import { onMount } from 'svelte';
	import { message } from '#lib/core/api.ts';
	import { toasts } from '#lib/core/toasts.svelte.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import { applyStreamingPresets } from './streaming-config-api.ts';
	import AddonsSettingsTab from './AddonsSettingsTab.svelte';
	import PluginsSettingsTab from './PluginsSettingsTab.svelte';
	import DebridSettingsTab from './DebridSettingsTab.svelte';
	import PlaybackSyncSettingsTab from './PlaybackSyncSettingsTab.svelte';

	type TabId = 'addons' | 'plugins' | 'debrid' | 'playback';
	let tab = $state<TabId>('addons');
	let reloadKey = $state(0);
	let settingUp = $state(false);

	function syncFromHash() {
		const h = (window.location.hash || '').replace(/^#/, '').toLowerCase();
		if (h === 'addons' || h === 'plugins' || h === 'debrid' || h === 'playback') {
			tab = h;
		} else if (h === 'sync') {
			tab = 'playback';
		}
	}

	async function quickSetup() {
		if (settingUp) return;
		settingUp = true;
		try {
			const res = await applyStreamingPresets({ recommended: true });
			const nInst = res.installed?.length ?? 0;
			const nSkip = res.skipped?.length ?? 0;
			const nErr = res.errors?.length ?? 0;
			const summary = `Installed ${nInst}, already set ${nSkip}${nErr ? `, ${nErr} unavailable` : ''}`;
			toasts.push(summary, nErr && !nInst && !nSkip ? 'fail' : 'ok');
			reloadKey++;
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			settingUp = false;
		}
	}

	onMount(() => {
		syncFromHash();
		window.addEventListener('hashchange', syncFromHash);
		return () => window.removeEventListener('hashchange', syncFromHash);
	});
</script>

<div class="streaming-settings">
	<header class="page-head">
		<div>
			<h1>Streaming &amp; Addons</h1>
			<p>Combine Stremio addons, Nuvio JS scrapers, encrypted Debrid resolvers, Local Library playback, skip segments, and Trakt/Simkl sync.</p>
		</div>
		<button type="button" class="quick-btn" disabled={settingUp} onclick={quickSetup} data-testid="streaming-quick-setup">
			<Icon name="sparkle" size={16} />
			<span>{settingUp ? 'Setting up pack...' : 'Quick Setup Recommended Pack'}</span>
		</button>
	</header>

	<nav class="tabs" aria-label="Streaming configuration sections">
		<button type="button" id="addons" class:active={tab === 'addons'} onclick={() => { tab = 'addons'; }}>Stremio Addons</button>
		<button type="button" id="plugins" class:active={tab === 'plugins'} onclick={() => { tab = 'plugins'; }}>Nuvio Plugins</button>
		<button type="button" id="debrid" class:active={tab === 'debrid'} onclick={() => { tab = 'debrid'; }}>Debrid Services</button>
		<button type="button" id="playback" class:active={tab === 'playback'} onclick={() => { tab = 'playback'; }}>Playback &amp; Sync</button>
	</nav>

	<div class="tab-body" id="sync">
		{#key reloadKey}
			{#if tab === 'addons'}
				<AddonsSettingsTab />
			{:else if tab === 'plugins'}
				<PluginsSettingsTab />
			{:else if tab === 'debrid'}
				<DebridSettingsTab />
			{:else}
				<PlaybackSyncSettingsTab />
			{/if}
		{/key}
	</div>
</div>

<style>
	.streaming-settings { max-width: 980px; margin: 0 auto; padding: 12px 0 48px; display: flex; flex-direction: column; gap: 20px; }
	.page-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; flex-wrap: wrap; }
	.page-head h1 { margin: 0; font-size: 24px; font-weight: 800; color: var(--text); }
	.page-head p { margin: 6px 0 0; font-size: 14px; color: var(--text-muted, var(--muted)); max-width: 640px; }
	.quick-btn { display: inline-flex; align-items: center; gap: 8px; min-height: 40px; padding: 0 16px; border-radius: 10px; border: 0; background: var(--accent); color: #fff; font: 600 13px/1 var(--font-ui); cursor: pointer; flex: none; }
	.quick-btn:disabled { opacity: 0.6; cursor: not-allowed; }
	.tabs { display: flex; flex-wrap: wrap; gap: 8px; border-bottom: 1px solid var(--line, var(--border)); padding-bottom: 10px; }
	.tabs button { padding: 8px 14px; border-radius: 9px; border: 1px solid transparent; background: transparent; color: var(--text-muted, var(--muted)); font-size: 13px; font-weight: 600; cursor: pointer; }
	.tabs button.active { background: var(--surface-2); border-color: var(--line, var(--border)); color: var(--text); }
	.tab-body { padding-top: 4px; }
</style>
