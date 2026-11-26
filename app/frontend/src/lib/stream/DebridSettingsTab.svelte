<script lang="ts">
	import { message } from '#lib/core/api.ts';
	import { toasts } from '#lib/core/toasts.svelte.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import {
		deleteDebridKey,
		listDebridProviders,
		saveDebridKey,
		type DebridProviderStatus
	} from './streaming-config-api.ts';

	let providers = $state<DebridProviderStatus[]>([]);
	let inputs = $state<Record<string, string>>({});
	let busy = $state('');
	let loading = $state(true);

	async function load() {
		loading = true;
		try {
			const res = await listDebridProviders();
			providers = res.providers ?? [];
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			loading = false;
		}
	}

	async function onSave(provider: string) {
		const key = (inputs[provider] ?? '').trim();
		if (!key) return;
		busy = provider;
		try {
			const st = await saveDebridKey(provider, key);
			providers = providers.map((p) => (p.provider === provider ? st : p));
			inputs = { ...inputs, [provider]: '' };
			toasts.push(`${st.name} connected (${st.username || 'verified'})`, 'ok');
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			busy = '';
		}
	}

	async function onDelete(provider: string) {
		busy = provider;
		try {
			await deleteDebridKey(provider);
			providers = providers.map((p) => (p.provider === provider ? { ...p, configured: false, masked_key: '', username: '' } : p));
			toasts.push('Debrid key removed', 'ok');
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			busy = '';
		}
	}

	$effect(() => {
		void load();
	});
</script>

<section class="panel">
	<header class="head">
		<div>
			<h2>Native Debrid Resolvers</h2>
			<p class="sub">Keys are verified online and stored encrypted inside HolyArrs Vault. Cached torrents automatically promote to instant HTTP streams.</p>
		</div>
	</header>

	{#if loading}
		<p class="empty">Loading debrid status...</p>
	{:else}
		<div class="cards">
			{#each providers as p (p.provider)}
				<article class="card">
					<div class="card-top">
						<div>
							<h3>{p.name}</h3>
							{#if p.configured}
								<span class="badge ok">Connected · {p.masked_key}</span>
							{:else}
								<span class="badge">Not configured</span>
							{/if}
						</div>
						{#if p.configured}
							<button type="button" class="btn-icon danger" title="Remove key" disabled={busy === p.provider} onclick={() => onDelete(p.provider)}>
								<Icon name="trash" size={16} />
							</button>
						{/if}
					</div>
					<form class="key-form" onsubmit={(e) => { e.preventDefault(); void onSave(p.provider); }}>
						<input
							type="password"
							value={inputs[p.provider] ?? ''}
							oninput={(e) => { inputs = { ...inputs, [p.provider]: (e.currentTarget as HTMLInputElement).value }; }}
							placeholder={p.configured ? 'Paste new API key to rotate' : `Paste ${p.name} API key`}
							disabled={busy === p.provider}
						/>
						<button type="submit" class="btn-primary" disabled={busy === p.provider || !(inputs[p.provider] ?? '').trim()}>
							{busy === p.provider ? 'Verifying...' : 'Verify & Save'}
						</button>
					</form>
				</article>
			{/each}
		</div>
	{/if}
</section>

<style>
	.panel { display: flex; flex-direction: column; gap: 18px; }
	.head h2 { margin: 0; font-size: 18px; font-weight: 700; color: var(--text); }
	.sub { margin: 4px 0 0; font-size: 13px; color: var(--muted); }
	.empty { color: var(--muted); font-size: 14px; }
	.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 14px; }
	.card { padding: 16px; border-radius: 12px; border: 1px solid var(--border); background: var(--surface-2); display: flex; flex-direction: column; gap: 12px; }
	.card-top { display: flex; justify-content: space-between; align-items: flex-start; gap: 10px; }
	.card-top h3 { margin: 0 0 4px; font-size: 15px; font-weight: 700; color: var(--text); }
	.badge { display: inline-block; font-size: 11px; font-weight: 600; padding: 3px 8px; border-radius: 999px; background: var(--surface-3); color: var(--muted); }
	.badge.ok { background: color-mix(in srgb, #30a46c 18%, transparent); color: #30a46c; }
	.key-form { display: flex; gap: 8px; }
	.key-form input { flex: 1; min-width: 0; padding: 9px 10px; border-radius: 9px; border: 1px solid var(--border); background: var(--surface-1); color: var(--text); font-size: 13px; }
	.btn-primary { padding: 0 14px; border-radius: 9px; border: 0; background: var(--accent); color: #fff; font-size: 13px; font-weight: 600; cursor: pointer; }
	.btn-primary:disabled { opacity: 0.55; cursor: not-allowed; }
	.btn-icon { width: 32px; height: 32px; border-radius: 8px; border: 1px solid var(--border); background: var(--surface-1); color: var(--text); display: grid; place-items: center; cursor: pointer; }
	.btn-icon.danger:hover { border-color: #e5484d; color: #e5484d; }
</style>
