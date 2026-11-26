<script lang="ts">
	import { message } from '#lib/core/api.ts';
	import { toasts } from '#lib/core/toasts.svelte.ts';
	import {
		disconnectSimkl,
		getStreamingPrefs,
		getSyncStatus,
		pollSimklPIN,
		pullSyncProgress,
		saveStreamingPrefs,
		startSimklPIN,
		type SimklPINChallenge,
		type StreamPrefs,
		type SyncStatus
	} from './streaming-config-api.ts';
	import { resolveSubtitleStyle } from './subtitle-style.ts';

	let prefs = $state<StreamPrefs | null>(null);
	let sync = $state<SyncStatus | null>(null);
	let excludeInput = $state('');
	let simklClientId = $state('');
	let pinChallenge = $state<SimklPINChallenge | null>(null);
	let saving = $state(false);
	let pulling = $state(false);
	let pairing = $state(false);

	const previewStyle = $derived(prefs ? resolveSubtitleStyle(prefs.sub_style) : null);

	async function load() {
		try {
			const [p, s] = await Promise.all([getStreamingPrefs(), getSyncStatus()]);
			prefs = p;
			sync = s;
			excludeInput = (p.exclude_terms ?? []).join(', ');
		} catch (e) {
			toasts.push(message(e), 'fail');
		}
	}

	async function save() {
		if (!prefs) return;
		saving = true;
		try {
			const next: StreamPrefs = {
				...prefs,
				exclude_terms: excludeInput.split(',').map((t) => t.trim()).filter(Boolean)
			};
			prefs = await saveStreamingPrefs(next);
			excludeInput = prefs.exclude_terms.join(', ');
			toasts.push('Streaming preferences saved', 'ok');
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			saving = false;
		}
	}

	async function beginSimkl() {
		pairing = true;
		try {
			pinChallenge = await startSimklPIN(simklClientId.trim());
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			pairing = false;
		}
	}

	async function checkSimkl() {
		if (!pinChallenge) return;
		pairing = true;
		try {
			const res = await pollSimklPIN(pinChallenge.user_code);
			if (res.connected) {
				pinChallenge = null;
				sync = await getSyncStatus();
				toasts.push(`Simkl linked as ${res.username}`, 'ok');
			} else {
				toasts.push('Approve the code on Simkl first, then check again.', 'info');
			}
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			pairing = false;
		}
	}

	async function unlinkSimkl() {
		pairing = true;
		try {
			await disconnectSimkl();
			sync = await getSyncStatus();
			toasts.push('Simkl disconnected', 'ok');
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			pairing = false;
		}
	}

	async function onPullProgress() {
		pulling = true;
		try {
			const res = await pullSyncProgress();
			toasts.push(`Synced playback: ${res.merged} entries updated`, 'ok');
		} catch (e) {
			toasts.push(message(e), 'fail');
		} finally {
			pulling = false;
		}
	}

	$effect(() => {
		void load();
	});
</script>

{#if !prefs}
	<p class="empty">Loading preferences...</p>
{:else}
	<div class="grid">
		<section class="card">
			<h3>Source Ranking &amp; Auto-Play</h3>
			<label class="field">
				<span>Preferred resolution</span>
				<select bind:value={prefs.preferred_quality}>
					<option value="2160p">2160p (4K)</option>
					<option value="1080p">1080p</option>
					<option value="720p">720p</option>
					<option value="">Any resolution</option>
				</select>
			</label>
			<label class="field">
				<span>Maximum release size (GB, 0 = unlimited)</span>
				<input type="number" min="0" step="1" bind:value={prefs.max_size_gb} />
			</label>
			<label class="field">
				<span>Exclude release terms (comma-separated)</span>
				<input type="text" bind:value={excludeInput} placeholder="CAM, HDCAM, TS, TELESYNC" />
			</label>
			<label class="toggle"><input type="checkbox" bind:checked={prefs.exclude_3d} /><span>Hide 3D / HSBS releases</span></label>
			<label class="toggle"><input type="checkbox" bind:checked={prefs.auto_resolve_debrid} /><span>Resolve cached torrents via Debrid when available</span></label>
			<label class="toggle"><input type="checkbox" bind:checked={prefs.auto_fallback} /><span>Automatically fall back if primary source fails</span></label>
			<label class="toggle"><input type="checkbox" bind:checked={prefs.auto_play_fastest} /><span>Auto-start top instant source when opening Watch</span></label>
		</section>

		<section class="card">
			<h3>Skip Segments &amp; Subtitles</h3>
			<label class="toggle"><input type="checkbox" bind:checked={prefs.auto_skip_intro} /><span>Auto-skip Intro (IntroDB / AniSkip / Chapters)</span></label>
			<label class="toggle"><input type="checkbox" bind:checked={prefs.auto_skip_recap} /><span>Auto-skip Recap</span></label>
			<label class="toggle"><input type="checkbox" bind:checked={prefs.auto_skip_outro} /><span>Auto-skip Outro</span></label>
			<label class="toggle"><input type="checkbox" bind:checked={prefs.sub_style.strip_sdh} /><span>Strip SDH cues ([music], speaker names)</span></label>
			<label class="toggle"><input type="checkbox" bind:checked={prefs.sub_style.hdr_dimmed} /><span>HDR-dimmed white subtitles (comfortable in dark rooms)</span></label>
			<label class="field">
				<span>HDR Subtitle Brightness ({prefs.sub_style.hdr_brightness}%)</span>
				<input type="range" min="10" max="100" step="5" bind:value={prefs.sub_style.hdr_brightness} />
			</label>
			<div class="sub-preview" style:background="#111">
				{#if previewStyle}
					<span style:color={previewStyle.color} style:background={previewStyle.background} style:font-family={previewStyle.fontFamilyCss}>
						HDR-safe subtitle preview
					</span>
				{/if}
			</div>
			<label class="field">
				<span>Anime-Skip Client ID (optional)</span>
				<input type="text" bind:value={prefs.anime_skip_client_id} placeholder="Optional Anime-Skip Client ID" />
			</label>
			<label class="field">
				<span>TMDB API key for Nuvio JS scrapers (optional)</span>
				<input type="password" bind:value={prefs.tmdb_api_key} placeholder="Optional TMDB v3 key" />
			</label>
		</section>

		<section class="card wide">
			<div class="sync-head">
				<div>
					<h3>Trakt &amp; Simkl Playback Sync</h3>
					<p class="sub">Scrobble start, pause, and stop events while watching and pull remote resume points.</p>
				</div>
				<button type="button" class="btn-ghost" disabled={pulling} onclick={onPullProgress}>
					{pulling ? 'Pulling...' : 'Pull Progress Now'}
				</button>
			</div>
			<div class="sync-grid">
				<div class="sync-box">
					<strong>Trakt</strong>
					<span class="pill" class:ok={sync?.trakt_connected}>{sync?.trakt_connected ? 'Connected via Outside Lists' : 'Not signed in'}</span>
					<label class="toggle"><input type="checkbox" bind:checked={prefs.scrobble_trakt} /><span>Scrobble to Trakt</span></label>
				</div>
				<div class="sync-box">
					<strong>Simkl</strong>
					{#if sync?.simkl_connected}
						<span class="pill ok">Connected ({sync.simkl_username})</span>
						<button type="button" class="btn-ghost danger" disabled={pairing} onclick={unlinkSimkl}>Disconnect Simkl</button>
					{:else if pinChallenge}
						<p class="pin-note">Open <a href={pinChallenge.verification_url} target="_blank" rel="noreferrer">{pinChallenge.verification_url}</a> and enter code <code>{pinChallenge.user_code}</code></p>
						<button type="button" class="btn-primary" disabled={pairing} onclick={checkSimkl}>I Approved Code</button>
					{:else}
						<input type="text" bind:value={simklClientId} placeholder="Optional custom Simkl Client ID" />
						<button type="button" class="btn-ghost" disabled={pairing} onclick={beginSimkl}>Connect Simkl via PIN</button>
					{/if}
					<label class="toggle"><input type="checkbox" bind:checked={prefs.scrobble_simkl} /><span>Scrobble to Simkl</span></label>
				</div>
			</div>
			<div class="save-bar">
				<button type="button" class="btn-primary" disabled={saving} onclick={save}>
					{saving ? 'Saving...' : 'Save Streaming Settings'}
				</button>
			</div>
		</section>
	</div>
{/if}

<style>
	.empty { color: var(--muted); }
	.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 16px; }
	.card { padding: 16px; border-radius: 12px; border: 1px solid var(--border); background: var(--surface-2); display: flex; flex-direction: column; gap: 12px; }
	.card.wide { grid-column: 1 / -1; }
	h3 { margin: 0; font-size: 16px; font-weight: 700; color: var(--text); }
	.sub { margin: 4px 0 0; font-size: 13px; color: var(--muted); }
	.field { display: flex; flex-direction: column; gap: 5px; font-size: 13px; color: var(--muted); }
	.field input, .field select, .sync-box input { padding: 8px 10px; border-radius: 8px; border: 1px solid var(--border); background: var(--surface-1); color: var(--text); font-size: 13px; }
	.toggle { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--text); cursor: pointer; }
	.sub-preview { padding: 12px; border-radius: 9px; text-align: center; font-size: 16px; font-weight: 600; }
	.sub-preview span { padding: 4px 10px; border-radius: 6px; }
	.sync-head { display: flex; justify-content: space-between; align-items: center; gap: 12px; flex-wrap: wrap; }
	.sync-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 12px; }
	.sync-box { padding: 12px; border-radius: 10px; background: var(--surface-1); border: 1px solid var(--border); display: flex; flex-direction: column; gap: 8px; }
	.pill { align-self: flex-start; font-size: 11px; font-weight: 600; padding: 3px 8px; border-radius: 999px; background: var(--surface-3); color: var(--muted); }
	.pill.ok { background: color-mix(in srgb, #30a46c 18%, transparent); color: #30a46c; }
	.pin-note { margin: 0; font-size: 12px; color: var(--text); }
	.pin-note a { color: var(--accent); }
	.save-bar { display: flex; justify-content: flex-end; }
	.btn-primary { padding: 9px 18px; border-radius: 9px; border: 0; background: var(--accent); color: #fff; font-size: 13px; font-weight: 600; cursor: pointer; }
	.btn-ghost { padding: 8px 12px; border-radius: 9px; border: 1px solid var(--border); background: var(--surface-1); color: var(--text); font-size: 12px; font-weight: 600; cursor: pointer; }
	.btn-ghost.danger:hover { border-color: #e5484d; color: #e5484d; }
</style>
