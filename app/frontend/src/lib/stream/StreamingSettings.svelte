<script lang="ts">
	import AddonsSettingsTab from './AddonsSettingsTab.svelte';
	import PluginsSettingsTab from './PluginsSettingsTab.svelte';
	import DebridSettingsTab from './DebridSettingsTab.svelte';
	import PlaybackSyncSettingsTab from './PlaybackSyncSettingsTab.svelte';

	type TabId = 'addons' | 'plugins' | 'debrid' | 'playback';
	let tab = $state<TabId>('addons');
</script>

<div class="streaming-settings">
	<header class="page-head">
		<h1>Streaming &amp; Addons</h1>
		<p>Combine Stremio addons, Nuvio JS scrapers, encrypted Debrid resolvers, Local Library playback, skip segments, and Trakt/Simkl sync.</p>
	</header>

	<nav class="tabs" aria-label="Streaming configuration sections">
		<button type="button" class:active={tab === 'addons'} onclick={() => { tab = 'addons'; }}>Stremio Addons</button>
		<button type="button" class:active={tab === 'plugins'} onclick={() => { tab = 'plugins'; }}>Nuvio Plugins</button>
		<button type="button" class:active={tab === 'debrid'} onclick={() => { tab = 'debrid'; }}>Debrid Services</button>
		<button type="button" class:active={tab === 'playback'} onclick={() => { tab = 'playback'; }}>Playback &amp; Sync</button>
	</nav>

	<div class="tab-body">
		{#if tab === 'addons'}
			<AddonsSettingsTab />
		{:else if tab === 'plugins'}
			<PluginsSettingsTab />
		{:else if tab === 'debrid'}
			<DebridSettingsTab />
		{:else}
			<PlaybackSyncSettingsTab />
		{/if}
	</div>
</div>

<style>
	.streaming-settings { max-width: 980px; margin: 0 auto; padding: 24px 20px 48px; display: flex; flex-direction: column; gap: 20px; }
	.page-head h1 { margin: 0; font-size: 24px; font-weight: 800; color: var(--text); }
	.page-head p { margin: 6px 0 0; font-size: 14px; color: var(--muted); }
	.tabs { display: flex; flex-wrap: wrap; gap: 8px; border-bottom: 1px solid var(--border); padding-bottom: 10px; }
	.tabs button { padding: 8px 14px; border-radius: 9px; border: 1px solid transparent; background: transparent; color: var(--muted); font-size: 13px; font-weight: 600; cursor: pointer; }
	.tabs button.active { background: var(--surface-2); border-color: var(--border); color: var(--text); }
	.tab-body { padding-top: 4px; }
</style>
