<script lang="ts">
	import { message } from '#lib/core/api.ts';
	import { toasts } from '#lib/core/toasts.svelte.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import {
		addPluginRepo,
		deletePluginRepo,
		listPluginRepos,
		refreshPluginRepo,
		updatePluginRepo,
		type NuvioRepo,
		type NuvioScraper
	} from './streaming-config-api.ts';

	let repos = $state<NuvioRepo[]>([]);
	let nodeAvailable = $state(true);
	let loading = $state(true);
	let manifestUrl = $state('');
	let busy = $state(false);
	let busyId = $state('');

	async function load() {
		loading = true;
		try {
			const res = await listPluginRepos();
			repos = res.repos ?? [];
			nodeAvailable = res.node_available;
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			loading = false;
		}
	}

	async function installRepo(ev: Event) {
		ev.preventDefault();
		const url = manifestUrl.trim();
		if (!url) return;
		busy = true;
		try {
			await addPluginRepo(url);
			manifestUrl = '';
			toasts.push('Plugin repository added', 'ok');
			await load();
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			busy = false;
		}
	}

	async function toggleRepo(r: NuvioRepo) {
		busyId = r.id;
		try {
			const updated = await updatePluginRepo(r.id, { enabled: !r.enabled });
			repos = repos.map((x) => (x.id === r.id ? updated : x));
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			busyId = '';
		}
	}

	async function toggleScraper(r: NuvioRepo, sc: NuvioScraper) {
		busyId = `${r.id}:${sc.id}`;
		try {
			const updated = await updatePluginRepo(r.id, { scraper_id: sc.id, scraper_enabled: !sc.enabled });
			repos = repos.map((x) => (x.id === r.id ? updated : x));
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			busyId = '';
		}
	}

	async function onRefresh(id: string) {
		busyId = id;
		try {
			const updated = await refreshPluginRepo(id);
			repos = repos.map((x) => (x.id === id ? updated : x));
			toasts.push('Repository refreshed', 'ok');
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			busyId = '';
		}
	}

	async function onDelete(r: NuvioRepo) {
		busyId = r.id;
		try {
			await deletePluginRepo(r.id);
			repos = repos.filter((x) => x.id !== r.id);
			toasts.push(`Removed ${r.name}`, 'ok');
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			busyId = '';
		}
	}

	$effect(() => {
		void load();
	});
</script>

<section class="panel">
	<header class="head">
		<div>
			<h2>Nuvio JS Provider Plugins</h2>
			<p class="sub">Install Nuvio-compatible JS scraper repositories. Scripts run in isolated Node workers with SSRF network guards.</p>
		</div>
		<span class="status-pill" class:warn={!nodeAvailable}>
			{nodeAvailable ? 'Node Worker Ready' : 'Node Runtime Missing'}
		</span>
	</header>

	<form class="add-row" onsubmit={installRepo}>
		<input
			type="url"
			bind:value={manifestUrl}
			placeholder="https://raw.githubusercontent.com/.../manifest.json"
			aria-label="Nuvio repository manifest URL"
			disabled={busy}
		/>
		<button type="submit" class="btn-primary" disabled={busy || !manifestUrl.trim()}>
			<Icon name="plus" size={16} />
			<span>{busy ? 'Downloading...' : 'Add Repository'}</span>
		</button>
	</form>

	{#if loading}
		<p class="empty">Loading plugin repositories...</p>
	{:else if repos.length === 0}
		<p class="empty">No Nuvio scraper repositories installed yet.</p>
	{:else}
		<div class="repos">
			{#each repos as r (r.id)}
				<article class="repo-card" class:disabled={!r.enabled}>
					<div class="repo-head">
						<div class="repo-info">
							<strong>{r.name}</strong>
							<span class="pill">{r.scrapers.length} scrapers</span>
							<p class="url">{r.manifest_url}</p>
						</div>
						<div class="actions">
							<button type="button" class="btn-ghost" disabled={busyId === r.id} onclick={() => toggleRepo(r)}>
								{r.enabled ? 'Enabled' : 'Disabled'}
							</button>
							<button type="button" class="btn-icon" title="Refresh scripts" disabled={busyId === r.id} onclick={() => onRefresh(r.id)}>
								<Icon name="refresh" size={16} />
							</button>
							<button type="button" class="btn-icon danger" title="Remove repository" disabled={busyId === r.id} onclick={() => onDelete(r)}>
								<Icon name="trash" size={16} />
							</button>
						</div>
					</div>
					<ul class="scraper-list">
						{#each r.scrapers as sc (sc.id)}
							<li class="scraper-row">
								<div class="sc-info">
									<span>{sc.name}</span>
									{#if sc.version}<span class="pill">v{sc.version}</span>{/if}
									{#each sc.supported_types ?? [] as tp}<span class="pill muted">{tp}</span>{/each}
								</div>
								<button
									type="button"
									class="btn-mini"
									class:active={sc.enabled}
									disabled={busyId === `${r.id}:${sc.id}`}
									onclick={() => toggleScraper(r, sc)}
								>
									{sc.enabled ? 'On' : 'Off'}
								</button>
							</li>
						{/each}
					</ul>
				</article>
			{/each}
		</div>
	{/if}
</section>

<style>
	.panel { display: flex; flex-direction: column; gap: 18px; }
	.head { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; flex-wrap: wrap; }
	.head h2 { margin: 0; font-size: 18px; font-weight: 700; color: var(--text); }
	.sub { margin: 4px 0 0; font-size: 13px; color: var(--muted); }
	.status-pill { font-size: 12px; font-weight: 600; padding: 5px 10px; border-radius: 999px; background: color-mix(in srgb, #30a46c 18%, transparent); color: #30a46c; }
	.status-pill.warn { background: color-mix(in srgb, #e5484d 18%, transparent); color: #e5484d; }
	.add-row { display: flex; gap: 10px; }
	.add-row input { flex: 1; padding: 10px 12px; border-radius: 10px; border: 1px solid var(--border); background: var(--surface-2); color: var(--text); font-size: 14px; }
	.btn-primary { display: inline-flex; align-items: center; gap: 6px; padding: 0 16px; border-radius: 10px; border: 0; background: var(--accent); color: #fff; font-weight: 600; cursor: pointer; }
	.btn-primary:disabled { opacity: 0.55; cursor: not-allowed; }
	.empty { margin: 8px 0; color: var(--muted); font-size: 14px; }
	.repos { display: flex; flex-direction: column; gap: 14px; }
	.repo-card { padding: 14px; border-radius: 12px; border: 1px solid var(--border); background: var(--surface-2); display: flex; flex-direction: column; gap: 12px; }
	.repo-card.disabled { opacity: 0.7; }
	.repo-head { display: flex; justify-content: space-between; gap: 12px; align-items: center; flex-wrap: wrap; }
	.repo-info { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
	.url { margin: 0; font-size: 11px; color: var(--muted); word-break: break-all; }
	.actions { display: flex; gap: 8px; }
	.btn-ghost { padding: 7px 12px; border-radius: 8px; border: 1px solid var(--border); background: var(--surface-1); color: var(--text); font-size: 12px; font-weight: 600; cursor: pointer; }
	.btn-icon { width: 34px; height: 34px; border-radius: 8px; border: 1px solid var(--border); background: var(--surface-1); color: var(--text); display: grid; place-items: center; cursor: pointer; }
	.btn-icon.danger:hover { border-color: #e5484d; color: #e5484d; }
	.scraper-list { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 8px; }
	.scraper-row { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 8px 10px; border-radius: 9px; background: var(--surface-1); border: 1px solid var(--border); }
	.sc-info { display: flex; align-items: center; gap: 6px; min-width: 0; font-size: 13px; color: var(--text); }
	.pill { font-size: 11px; padding: 2px 6px; border-radius: 999px; background: var(--surface-3); color: var(--text); font-weight: 600; }
	.pill.muted { color: var(--muted); }
	.btn-mini { padding: 4px 10px; border-radius: 999px; border: 1px solid var(--border); background: transparent; color: var(--muted); font-size: 11px; font-weight: 700; cursor: pointer; }
	.btn-mini.active { background: var(--accent); border-color: var(--accent); color: #fff; }
</style>
