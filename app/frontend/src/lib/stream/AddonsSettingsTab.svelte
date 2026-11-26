<script lang="ts">
	import { message } from '#lib/core/api.ts';
	import { toasts } from '#lib/core/toasts.svelte.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import {
		addStremioAddon,
		applyStreamingPresets,
		deleteStremioAddon,
		getStreamingPresets,
		listStremioAddons,
		refreshStremioAddon,
		toggleStremioAddon,
		type PresetAddon,
		type StremioAddon
	} from './streaming-config-api.ts';

	let addons = $state<StremioAddon[]>([]);
	let presets = $state<PresetAddon[]>([]);
	let loading = $state(true);
	let manifestUrl = $state('');
	let busy = $state(false);
	let busyId = $state('');
	let installingPreset = $state('');

	async function load() {
		loading = true;
		try {
			const [addonsRes, presetRes] = await Promise.all([
				listStremioAddons(),
				getStreamingPresets().catch(() => ({ addons: [], plugins: [] }))
			]);
			addons = addonsRes.addons ?? [];
			presets = presetRes.addons ?? [];
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			loading = false;
		}
	}

	async function installAddon(ev: Event) {
		ev.preventDefault();
		const url = manifestUrl.trim();
		if (!url) return;
		busy = true;
		try {
			await addStremioAddon(url);
			manifestUrl = '';
			toasts.push('Stremio addon installed', 'ok');
			await load();
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			busy = false;
		}
	}

	async function installPreset(p: PresetAddon) {
		if (installingPreset || p.installed) return;
		installingPreset = p.id;
		try {
			const res = await applyStreamingPresets({ kind: 'addon', id: p.id });
			if (res.errors?.length) {
				toasts.push(res.errors[0], 'fail');
			} else {
				toasts.push(`${p.name} installed`, 'ok');
			}
			presets = res.catalog?.addons ?? presets;
			await load();
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			installingPreset = '';
		}
	}

	async function onToggle(a: StremioAddon) {
		busyId = a.id;
		try {
			const updated = await toggleStremioAddon(a.id, { enabled: !a.enabled });
			addons = addons.map((x) => (x.id === a.id ? updated : x));
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			busyId = '';
		}
	}

	async function onRefresh(id: string) {
		busyId = id;
		try {
			const updated = await refreshStremioAddon(id);
			addons = addons.map((x) => (x.id === id ? updated : x));
			toasts.push('Addon manifest refreshed', 'ok');
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			busyId = '';
		}
	}

	async function onDelete(a: StremioAddon) {
		busyId = a.id;
		try {
			await deleteStremioAddon(a.id);
			addons = addons.filter((x) => x.id !== a.id);
			toasts.push(`Removed ${a.name}`, 'ok');
			const presetRes = await getStreamingPresets().catch(() => null);
			if (presetRes) presets = presetRes.addons ?? [];
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
			<h2>Stremio Addons</h2>
			<p class="sub">Install any Stremio addon manifest (`https://` or `stremio://`) for fast HTTP/HLS streams, subtitle feeds, and Explore shelves.</p>
		</div>
	</header>

	{#if presets.length > 0}
		<div class="preset-box">
			<div class="preset-title">Recommended Addons</div>
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

	<form class="add-row" onsubmit={installAddon}>
		<input
			type="url"
			bind:value={manifestUrl}
			placeholder="https://example.com/manifest.json or stremio://..."
			aria-label="Addon manifest URL"
			disabled={busy}
		/>
		<button type="submit" class="btn-primary" disabled={busy || !manifestUrl.trim()}>
			<Icon name="plus" size={16} />
			<span>{busy ? 'Installing...' : 'Install Addon'}</span>
		</button>
	</form>

	{#if loading}
		<p class="empty">Loading addons...</p>
	{:else if addons.length === 0}
		<p class="empty">No Stremio addons installed yet. Pick one above or paste a custom manifest URL.</p>
	{:else}
		<ul class="list">
			{#each addons as a (a.id)}
				<li class="item" class:disabled={!a.enabled}>
					<div class="meta">
						{#if a.logo}
							<img class="logo" src={a.logo} alt="" />
						{:else}
							<div class="logo placeholder"><Icon name="plug" size={18} /></div>
						{/if}
						<div class="text">
							<div class="title-row">
								<strong>{a.name}</strong>
								{#if a.version}<span class="pill">v{a.version}</span>{/if}
								{#each a.resources as res}<span class="pill muted">{res}</span>{/each}
								{#if a.catalogs?.length}<span class="pill accent">{a.catalogs.length} catalogs</span>{/if}
							</div>
							{#if a.description}<p class="desc">{a.description}</p>{/if}
							<p class="url">{a.manifest_url}</p>
						</div>
					</div>
					<div class="actions">
						<button type="button" class="btn-ghost" disabled={busyId === a.id} onclick={() => onToggle(a)}>
							{a.enabled ? 'Enabled' : 'Disabled'}
						</button>
						<button type="button" class="btn-icon" title="Refresh manifest" disabled={busyId === a.id} onclick={() => onRefresh(a.id)}>
							<Icon name="refresh" size={16} />
						</button>
						<button type="button" class="btn-icon danger" title="Remove addon" disabled={busyId === a.id} onclick={() => onDelete(a)}>
							<Icon name="trash" size={16} />
						</button>
					</div>
				</li>
			{/each}
		</ul>
	{/if}
</section>

<style>
	.panel { display: flex; flex-direction: column; gap: 18px; }
	.head h2 { margin: 0; font-size: 18px; font-weight: 700; color: var(--text); }
	.sub { margin: 4px 0 0; font-size: 13px; color: var(--text-muted, var(--muted)); }
	.preset-box { display: flex; flex-direction: column; gap: 10px; padding: 14px; border-radius: 12px; border: 1px solid var(--line, var(--border)); background: var(--surface-1); }
	.preset-title { font-size: 12px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted, var(--muted)); }
	.preset-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 10px; }
	.preset-card { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 10px 12px; border-radius: 10px; border: 1px solid var(--line, var(--border)); background: var(--surface-2); }
	.preset-meta { min-width: 0; display: flex; flex-direction: column; gap: 4px; }
	.preset-top { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; color: var(--text); font-size: 13px; }
	.preset-btn { display: inline-flex; align-items: center; gap: 5px; flex: none; padding: 6px 10px; border-radius: 8px; border: 1px solid var(--line, var(--border)); background: var(--surface-1); color: var(--text); font-size: 12px; font-weight: 600; cursor: pointer; }
	.preset-btn.done { border-color: color-mix(in srgb, #30a46c 45%, transparent); color: #30a46c; background: color-mix(in srgb, #30a46c 12%, transparent); cursor: default; }
	.add-row { display: flex; gap: 10px; }
	.add-row input { flex: 1; padding: 10px 12px; border-radius: 10px; border: 1px solid var(--line, var(--border)); background: var(--surface-2); color: var(--text); font-size: 14px; }
	.btn-primary { display: inline-flex; align-items: center; gap: 6px; padding: 0 16px; border-radius: 10px; border: 0; background: var(--accent); color: #fff; font-weight: 600; cursor: pointer; }
	.btn-primary:disabled { opacity: 0.55; cursor: not-allowed; }
	.empty { margin: 8px 0; color: var(--text-muted, var(--muted)); font-size: 14px; }
	.list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 10px; }
	.item { display: flex; align-items: center; justify-content: space-between; gap: 14px; padding: 14px; border-radius: 12px; border: 1px solid var(--line, var(--border)); background: var(--surface-2); }
	.item.disabled { opacity: 0.65; }
	.meta { display: flex; align-items: flex-start; gap: 12px; min-width: 0; }
	.logo { width: 40px; height: 40px; border-radius: 9px; object-fit: contain; background: var(--surface-3); flex: none; }
	.logo.placeholder { display: grid; place-items: center; color: var(--text-muted, var(--muted)); }
	.text { min-width: 0; display: flex; flex-direction: column; gap: 4px; }
	.title-row { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; color: var(--text); }
	.pill { font-size: 11px; padding: 2px 7px; border-radius: 999px; background: var(--surface-3); color: var(--text); font-weight: 600; }
	.pill.muted { color: var(--text-muted, var(--muted)); }
	.pill.accent { background: color-mix(in srgb, var(--accent) 18%, transparent); color: var(--accent); }
	.desc { margin: 0; font-size: 12px; color: var(--text-muted, var(--muted)); }
	.url { margin: 0; font-size: 11px; color: var(--text-muted, var(--muted)); word-break: break-all; }
	.actions { display: flex; align-items: center; gap: 8px; flex: none; }
	.btn-ghost { padding: 7px 12px; border-radius: 8px; border: 1px solid var(--line, var(--border)); background: var(--surface-1); color: var(--text); font-size: 12px; font-weight: 600; cursor: pointer; }
	.btn-icon { width: 34px; height: 34px; border-radius: 8px; border: 1px solid var(--line, var(--border)); background: var(--surface-1); color: var(--text); display: grid; place-items: center; cursor: pointer; }
	.btn-icon.danger:hover { border-color: #e5484d; color: #e5484d; }
</style>
