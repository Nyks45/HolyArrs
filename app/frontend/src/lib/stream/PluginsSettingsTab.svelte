<script lang="ts">
	import { message } from '#lib/core/api.ts';
	import { toasts } from '#lib/core/toasts.svelte.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import {
		addPluginRepo,
		applyStreamingPresets,
		deleteCustomPlugin,
		deletePluginRepo,
		getCustomPlugin,
		getStreamingPresets,
		listPluginRepos,
		refreshPluginRepo,
		saveCustomPlugin,
		updatePluginRepo,
		type NuvioRepo,
		type NuvioScraper,
		type PresetPluginRepo
	} from './streaming-config-api.ts';

	const DEFAULT_TEMPLATE = `// Nuvio-compatible JS scraper for personal streaming APIs
async function getStreams(tmdbId, mediaType, season, episode) {
\tconst path = mediaType === 'movie' ? \`movie/\${tmdbId}\` : \`tv/\${tmdbId}/\${season}/\${episode}\`;
\tconst res = await fetch(\`https://example.com/api/\${path}\`, {
\t\theaders: { 'Referer': 'https://example.com/' }
\t});
\tif (!res.ok) return [];
\tconst data = await res.json();
\treturn (data.sources || []).map((s) => ({
\t\tname: 'My Source',
\t\ttitle: s.label || 'Direct Stream',
\t\turl: s.url,
\t\tquality: s.quality || '1080p',
\t\theaders: { Referer: 'https://example.com/' }
\t}));
}`;

	let repos = $state<NuvioRepo[]>([]);
	let presets = $state<PresetPluginRepo[]>([]);
	let scraperFilters = $state<Record<string, string>>({});
	let nodeAvailable = $state(true);
	let loading = $state(true);
	let manifestUrl = $state('');
	let busy = $state(false);
	let busyId = $state('');
	let installingPreset = $state('');

	let customOpen = $state(false);
	let customEditingId = $state('');
	let customName = $state('');
	let customDesc = $state('');
	let customTypes = $state<string[]>(['movie', 'tv']);
	let customCode = $state(DEFAULT_TEMPLATE);
	let savingCustom = $state(false);

	async function load() {
		loading = true;
		try {
			const [res, presetRes] = await Promise.all([
				listPluginRepos(),
				getStreamingPresets().catch(() => ({ addons: [], plugins: [] }))
			]);
			repos = res.repos ?? [];
			nodeAvailable = res.node_available;
			presets = presetRes.plugins ?? [];
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

	async function installPreset(p: PresetPluginRepo) {
		if (installingPreset || p.installed) return;
		installingPreset = p.id;
		try {
			const res = await applyStreamingPresets({ kind: 'plugin', id: p.id });
			if (res.errors?.length) {
				toasts.push(res.errors[0], 'fail');
			} else {
				toasts.push(`${p.name} installed`, 'ok');
			}
			presets = res.catalog?.plugins ?? presets;
			await load();
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			installingPreset = '';
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
			const presetRes = await getStreamingPresets().catch(() => null);
			if (presetRes) presets = presetRes.plugins ?? [];
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			busyId = '';
		}
	}

	function filteredScrapers(r: NuvioRepo): NuvioScraper[] {
		const q = (scraperFilters[r.id] ?? '').trim().toLowerCase();
		if (!q) return r.scrapers;
		return r.scrapers.filter((sc) =>
			sc.name.toLowerCase().includes(q) ||
			sc.id.toLowerCase().includes(q) ||
			(sc.description ?? '').toLowerCase().includes(q) ||
			(sc.supported_types ?? []).some((t) => t.toLowerCase().includes(q))
		);
	}

	function toggleCustomType(tp: string) {
		if (customTypes.includes(tp)) {
			if (customTypes.length > 1) customTypes = customTypes.filter((x) => x !== tp);
		} else {
			customTypes = [...customTypes, tp];
		}
	}

	function resetCustomEditor() {
		customEditingId = '';
		customName = '';
		customDesc = '';
		customTypes = ['movie', 'tv'];
		customCode = DEFAULT_TEMPLATE;
	}

	async function openCustomEditor(id = '') {
		if (!id) {
			if (customOpen && !customEditingId) {
				customOpen = false;
				return;
			}
			resetCustomEditor();
			customOpen = true;
			return;
		}
		busyId = `local-custom:${id}`;
		try {
			const detail = await getCustomPlugin(id);
			customEditingId = detail.id;
			customName = detail.name;
			customDesc = detail.description ?? '';
			customTypes = detail.supported_types?.length ? [...detail.supported_types] : ['movie', 'tv'];
			customCode = detail.code;
			customOpen = true;
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			busyId = '';
		}
	}

	async function submitCustomPlugin(ev: Event) {
		ev.preventDefault();
		if (!customName.trim() || !customCode.trim()) return;
		savingCustom = true;
		try {
			await saveCustomPlugin({
				id: customEditingId || undefined,
				name: customName.trim(),
				description: customDesc.trim(),
				supported_types: customTypes,
				code: customCode
			});
			toasts.push(customEditingId ? 'Custom plugin updated' : 'Custom plugin saved', 'ok');
			resetCustomEditor();
			customOpen = false;
			await load();
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			savingCustom = false;
		}
	}

	async function onDeleteCustom(sc: NuvioScraper) {
		busyId = `local-custom:${sc.id}`;
		try {
			await deleteCustomPlugin(sc.id);
			if (customEditingId === sc.id) {
				resetCustomEditor();
				customOpen = false;
			}
			toasts.push(`Removed ${sc.name}`, 'ok');
			await load();
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

	{#if presets.length > 0}
		<div class="preset-box">
			<div class="preset-title">Recommended Nuvio Repositories</div>
			<div class="preset-grid">
				{#each presets as p (p.id)}
					<div class="preset-card">
						<div class="preset-meta">
							<div class="preset-top">
								<strong>{p.name}</strong>
								<span class="pill muted">{p.category}</span>
							</div>
							<p class="desc">{p.description}</p>
						</div>
						<button
							type="button"
							class="preset-btn"
							class:done={p.installed}
							disabled={p.installed || installingPreset === p.id}
							onclick={() => installPreset(p)}
						>
							{#if p.installed}
								<Icon name="check" size={14} /><span>Installed</span>
							{:else}
								<Icon name="plus" size={14} /><span>{installingPreset === p.id ? 'Adding...' : 'Install'}</span>
							{/if}
						</button>
					</div>
				{/each}
			</div>
		</div>
	{/if}

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

	<div class="custom-wrap">
		<div class="custom-bar">
			<button type="button" class="btn-ghost" onclick={() => void openCustomEditor()}>
				<Icon name={customOpen && !customEditingId ? 'x' : 'plus'} size={15} />
				<span>{customOpen && !customEditingId ? 'Close Custom Editor' : 'Create Custom JS Plugin'}</span>
			</button>
			<span class="desc">Run personal Nuvio-compatible <code>getStreams</code> JavaScript scrapers in the local Node sandbox.</span>
		</div>

		{#if customOpen}
			<form class="custom-form" onsubmit={submitCustomPlugin}>
				<div class="custom-fields">
					<label class="field">
						<span>Plugin Name</span>
						<input type="text" bind:value={customName} placeholder="My Stream API" disabled={savingCustom} required />
					</label>
					<label class="field">
						<span>Description (optional)</span>
						<input type="text" bind:value={customDesc} placeholder="Direct HLS provider" disabled={savingCustom} />
					</label>
				</div>
				<div class="type-row">
					<span class="type-label">Supported Types</span>
					{#each [{ id: 'movie', label: 'Movie' }, { id: 'tv', label: 'TV Show' }, { id: 'anime', label: 'Anime' }] as tp (tp.id)}
						<label class="chk">
							<input type="checkbox" checked={customTypes.includes(tp.id)} onchange={() => toggleCustomType(tp.id)} disabled={savingCustom} />
							<span>{tp.label}</span>
						</label>
					{/each}
				</div>
				<label class="field">
					<span>Scraper JavaScript (must define <code>getStreams(tmdbId, mediaType, season, episode)</code>)</span>
					<textarea bind:value={customCode} rows={12} spellcheck="false" disabled={savingCustom}></textarea>
				</label>
				<div class="custom-actions">
					<button type="submit" class="btn-primary" disabled={savingCustom || !customName.trim() || !customCode.trim()}>
						<Icon name="check" size={15} />
						<span>{savingCustom ? 'Saving...' : 'Save Custom Plugin'}</span>
					</button>
					<button type="button" class="btn-ghost" disabled={savingCustom} onclick={() => { resetCustomEditor(); customOpen = false; }}>
						Cancel
					</button>
					<button type="button" class="btn-ghost" disabled={savingCustom} onclick={resetCustomEditor}>
						Reset
					</button>
				</div>
			</form>
		{/if}
	</div>

	{#if loading}
		<p class="empty">Loading plugin repositories...</p>
	{:else if repos.length === 0}
		<p class="empty">No Nuvio scraper repositories installed yet. Pick a recommended repository above or paste a manifest URL.</p>
	{:else}
		<div class="repos">
			{#each repos as r (r.id)}
				{@const onCount = r.scrapers.filter((s) => s.enabled).length}
				{@const visibleScrapers = filteredScrapers(r)}
				<article class="repo-card" class:disabled={!r.enabled}>
					<div class="repo-head">
						<div class="repo-info">
							<div class="repo-title">
								<strong>{r.name}</strong>
								<span class="pill">{onCount} / {r.scrapers.length} on</span>
							</div>
							<p class="url">{r.manifest_url}</p>
						</div>
						<div class="actions">
							<button type="button" class="btn-ghost" disabled={busyId === r.id} onclick={() => toggleRepo(r)}>
								{r.enabled ? 'Enabled' : 'Disabled'}
							</button>
							{#if r.id !== 'local-custom' && r.manifest_url !== 'local://custom'}
								<button type="button" class="btn-icon" title="Refresh scripts" disabled={busyId === r.id} onclick={() => onRefresh(r.id)}>
									<Icon name="refresh" size={16} />
								</button>
							{/if}
							<button type="button" class="btn-icon danger" title="Remove repository" disabled={busyId === r.id} onclick={() => onDelete(r)}>
								<Icon name="trash" size={16} />
							</button>
						</div>
					</div>
					{#if r.scrapers.length > 8}
						<div class="scraper-filter">
							<Icon name="search" size={15} />
							<input
								type="search"
								placeholder="Filter scrapers by name, ID or type (movie, tv, anime)..."
								value={scraperFilters[r.id] ?? ''}
								oninput={(ev) => { scraperFilters = { ...scraperFilters, [r.id]: (ev.currentTarget as HTMLInputElement).value }; }}
							/>
						</div>
					{/if}
					<ul class="scraper-list">
						{#each visibleScrapers as sc, idx (`${sc.id}:${idx}`)}
							<li class="scraper-row">
								<div class="sc-info">
									<span class="sc-name">{sc.name}</span>
									{#if sc.version}<span class="pill">v{sc.version}</span>{/if}
									{#each sc.supported_types ?? [] as tp}<span class="pill muted">{tp}</span>{/each}
								</div>
								<div class="sc-actions">
									{#if r.id === 'local-custom' || r.manifest_url === 'local://custom'}
										<button type="button" class="btn-mini" title="Edit script" disabled={busyId === `local-custom:${sc.id}`} onclick={() => void openCustomEditor(sc.id)}>
											<Icon name="pencil" size={12} />
											<span>Edit</span>
										</button>
										<button type="button" class="btn-mini danger" title="Remove script" disabled={busyId === `local-custom:${sc.id}`} onclick={() => void onDeleteCustom(sc)}>
											<Icon name="trash" size={12} />
										</button>
									{/if}
									<button
										type="button"
										class="btn-mini"
										class:active={sc.enabled}
										disabled={busyId === `${r.id}:${sc.id}`}
										onclick={() => toggleScraper(r, sc)}
									>
										{sc.enabled ? 'On' : 'Off'}
									</button>
								</div>
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
	.sub { margin: 4px 0 0; font-size: 13px; color: var(--text-muted, var(--muted)); }
	.status-pill { font-size: 12px; font-weight: 600; padding: 5px 10px; border-radius: 999px; background: color-mix(in srgb, #30a46c 18%, transparent); color: #30a46c; }
	.status-pill.warn { background: color-mix(in srgb, #e5484d 18%, transparent); color: #e5484d; }
	.preset-box { display: flex; flex-direction: column; gap: 10px; padding: 14px; border-radius: 12px; border: 1px solid var(--line, var(--border)); background: var(--surface-1); }
	.preset-title { font-size: 12px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted, var(--muted)); }
	.preset-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(270px, 1fr)); gap: 10px; }
	.preset-card { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 10px 12px; border-radius: 10px; border: 1px solid var(--line, var(--border)); background: var(--surface-2); }
	.preset-meta { min-width: 0; display: flex; flex-direction: column; gap: 4px; }
	.preset-top { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; color: var(--text); font-size: 13px; }
	.desc { margin: 0; font-size: 12px; color: var(--text-muted, var(--muted)); }
	.preset-btn { display: inline-flex; align-items: center; gap: 5px; flex: none; padding: 6px 10px; border-radius: 8px; border: 1px solid var(--line, var(--border)); background: var(--surface-1); color: var(--text); font-size: 12px; font-weight: 600; cursor: pointer; }
	.preset-btn.done { border-color: color-mix(in srgb, #30a46c 45%, transparent); color: #30a46c; background: color-mix(in srgb, #30a46c 12%, transparent); cursor: default; }
	.add-row { display: flex; gap: 10px; }
	.add-row input { flex: 1; padding: 10px 12px; border-radius: 10px; border: 1px solid var(--line, var(--border)); background: var(--surface-2); color: var(--text); font-size: 14px; }
	.btn-primary { display: inline-flex; align-items: center; gap: 6px; padding: 0 16px; border-radius: 10px; border: 0; background: var(--accent); color: #fff; font-weight: 600; cursor: pointer; }
	.btn-primary:disabled { opacity: 0.55; cursor: not-allowed; }
	.empty { margin: 8px 0; color: var(--text-muted, var(--muted)); font-size: 14px; }
	.repos { display: flex; flex-direction: column; gap: 14px; }
	.repo-card { padding: 14px; border-radius: 12px; border: 1px solid var(--line, var(--border)); background: var(--surface-2); display: flex; flex-direction: column; gap: 12px; }
	.repo-card.disabled { opacity: 0.7; }
	.repo-head { display: flex; justify-content: space-between; gap: 12px; align-items: center; flex-wrap: wrap; }
	.repo-info { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
	.repo-title { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; color: var(--text); }
	.url { margin: 0; font-size: 11px; color: var(--text-muted, var(--muted)); word-break: break-all; }
	.actions { display: flex; gap: 8px; }
	.btn-ghost { padding: 7px 12px; border-radius: 8px; border: 1px solid var(--line, var(--border)); background: var(--surface-1); color: var(--text); font-size: 12px; font-weight: 600; cursor: pointer; }
	.btn-icon { width: 34px; height: 34px; border-radius: 8px; border: 1px solid var(--line, var(--border)); background: var(--surface-1); color: var(--text); display: grid; place-items: center; cursor: pointer; }
	.btn-icon.danger:hover { border-color: #e5484d; color: #e5484d; }
	.scraper-filter { position: relative; display: flex; align-items: center; }
	.scraper-filter > :global(.icon) { position: absolute; left: 10px; color: var(--text-muted, var(--muted)); pointer-events: none; }
	.scraper-filter input { width: 100%; padding: 8px 12px 8px 32px; border-radius: 9px; border: 1px solid var(--line, var(--border)); background: var(--surface-1); color: var(--text); font-size: 13px; }
	.scraper-list { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 8px; max-height: 440px; overflow-y: auto; }
	.scraper-row { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 8px 10px; border-radius: 9px; background: var(--surface-1); border: 1px solid var(--line, var(--border)); }
	.sc-info { display: flex; align-items: center; gap: 6px; min-width: 0; font-size: 13px; color: var(--text); flex-wrap: wrap; }
	.sc-name { font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.pill { font-size: 11px; padding: 2px 6px; border-radius: 999px; background: var(--surface-3); color: var(--text); font-weight: 600; }
	.pill.muted { color: var(--text-muted, var(--muted)); }
	.btn-mini { display: inline-flex; align-items: center; gap: 4px; padding: 4px 10px; border-radius: 999px; border: 1px solid var(--line, var(--border)); background: transparent; color: var(--text-muted, var(--muted)); font-size: 11px; font-weight: 700; cursor: pointer; flex: none; }
	.btn-mini.active { background: var(--accent); border-color: var(--accent); color: #fff; }
	.btn-mini.danger:hover { border-color: #e5484d; color: #e5484d; }
	.sc-actions { display: flex; align-items: center; gap: 6px; flex: none; }
	.custom-wrap { display: flex; flex-direction: column; gap: 10px; padding: 12px 14px; border-radius: 12px; border: 1px solid var(--line, var(--border)); background: var(--surface-1); }
	.custom-bar { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
	.custom-bar .btn-ghost { display: inline-flex; align-items: center; gap: 6px; }
	.custom-form { display: flex; flex-direction: column; gap: 12px; padding-top: 6px; border-top: 1px solid var(--line, var(--border)); }
	.custom-fields { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 10px; }
	.field { display: flex; flex-direction: column; gap: 5px; font-size: 12px; font-weight: 600; color: var(--text-muted, var(--muted)); }
	.field input, .field textarea { padding: 9px 11px; border-radius: 9px; border: 1px solid var(--line, var(--border)); background: var(--surface-2); color: var(--text); font-size: 13px; }
	.field textarea { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 12px; line-height: 1.45; resize: vertical; }
	.type-row { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; font-size: 13px; color: var(--text); }
	.type-label { font-size: 12px; font-weight: 600; color: var(--text-muted, var(--muted)); }
	.chk { display: inline-flex; align-items: center; gap: 6px; cursor: pointer; }
	.custom-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
	.custom-actions .btn-primary { min-height: 36px; }
</style>
