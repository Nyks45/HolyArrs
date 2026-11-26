<script lang="ts">
	import Icon from '#lib/ui/Icon.svelte';
	import type { SessionStatus, StreamSubCandidate } from './stream-api.ts';
	import type { SubStylePrefs } from './streaming-config-api.ts';

	let {
		open,
		session,
		subMode,
		subIndex,
		subLabel,
		subDelayMS,
		subSize,
		subStyle = $bindable<SubStylePrefs>(),
		siteSubLangsInput = $bindable('en, tr'),
		siteSubCandidates,
		searchingSubs,
		loadingSub,
		subError,
		onClose,
		onTurnOff,
		onLoadEmbedded,
		onSearchOnline,
		onLoadSiteSub,
		onAdjustDelay,
		onChangeSize,
		onUploadFile,
		onStyleChange
	}: {
		open: boolean;
		session: SessionStatus | null;
		subMode: 'off' | 'embedded' | 'site' | 'upload';
		subIndex: number;
		subLabel: string;
		subDelayMS: number;
		subSize: 'sm' | 'md' | 'lg' | 'xl';
		subStyle: SubStylePrefs;
		siteSubLangsInput: string;
		siteSubCandidates: StreamSubCandidate[];
		searchingSubs: boolean;
		loadingSub: boolean;
		subError: string;
		onClose: () => void;
		onTurnOff: () => void;
		onLoadEmbedded: (idx: number) => void;
		onSearchOnline: () => void;
		onLoadSiteSub: (c: StreamSubCandidate) => void;
		onAdjustDelay: (deltaMS: number) => void;
		onChangeSize: (sz: 'sm' | 'md' | 'lg' | 'xl') => void;
		onUploadFile: (e: Event) => void;
		onStyleChange: (next: SubStylePrefs) => void;
	} = $props();

	let fileInput = $state<HTMLInputElement | undefined>(undefined);

	function patchStyle(patch: Partial<SubStylePrefs>) {
		const next = { ...subStyle, ...patch };
		subStyle = next;
		onStyleChange(next);
	}
</script>

{#if open}
	<aside class="wp-drawer" aria-label="Subtitles drawer">
		<div class="wp-drawer-head">
			<h3>Audio &amp; Subtitles</h3>
			<button type="button" class="wp-close" onclick={onClose}><Icon name="x" size={18} /></button>
		</div>

		<div class="wp-drawer-body">
			{#if subError}
				<div class="wp-err-inline">{subError}</div>
			{/if}

			<div class="wp-box">
				<div class="wp-row-between">
					<strong>Active Subtitle: {subMode === 'off' ? 'Off' : subLabel || 'Loaded'}</strong>
					{#if subMode !== 'off'}
						<button type="button" class="wp-mini-btn" onclick={onTurnOff}>Turn Off</button>
					{/if}
				</div>
				<div class="wp-sync-row">
					<span>Timing ({subDelayMS >= 0 ? `+${subDelayMS}` : subDelayMS} ms)</span>
					<div class="wp-btns">
						<button type="button" class="wp-mini-btn" onclick={() => onAdjustDelay(-250)}>-0.25s</button>
						<button type="button" class="wp-mini-btn" onclick={() => onAdjustDelay(250)}>+0.25s</button>
					</div>
				</div>
				<div class="wp-tab-row">
					{#each ['sm', 'md', 'lg', 'xl'] as sz}
						<button type="button" class="wp-chip" class:on={subSize === sz} onclick={() => onChangeSize(sz as typeof subSize)}>
							{sz.toUpperCase()}
						</button>
					{/each}
				</div>
			</div>

			<div class="wp-box">
				<div class="wp-box-title">Appearance &amp; Filters</div>
				<label class="wp-check">
					<input type="checkbox" checked={subStyle.strip_sdh} onchange={(e) => patchStyle({ strip_sdh: (e.currentTarget as HTMLInputElement).checked })} />
					<span>Strip SDH annotations ([music], speaker tags)</span>
				</label>
				<label class="wp-check">
					<input type="checkbox" checked={subStyle.hdr_dimmed} onchange={(e) => patchStyle({ hdr_dimmed: (e.currentTarget as HTMLInputElement).checked })} />
					<span>HDR-dimmed white ({subStyle.hdr_brightness}%)</span>
				</label>
				<label class="wp-slider-row">
					<span>Brightness</span>
					<input type="range" min="10" max="100" step="5" value={subStyle.hdr_brightness} oninput={(e) => patchStyle({ hdr_brightness: Number((e.currentTarget as HTMLInputElement).value) })} />
				</label>
				<label class="wp-slider-row">
					<span>Background Opacity ({Math.round(subStyle.bg_opacity * 100)}%)</span>
					<input type="range" min="0" max="1" step="0.05" value={subStyle.bg_opacity} oninput={(e) => patchStyle({ bg_opacity: Number((e.currentTarget as HTMLInputElement).value) })} />
				</label>
				<label class="wp-slider-row">
					<span>Vertical Offset ({subStyle.offset_pct}%)</span>
					<input type="range" min="2" max="28" step="1" value={subStyle.offset_pct} oninput={(e) => patchStyle({ offset_pct: Number((e.currentTarget as HTMLInputElement).value) })} />
				</label>
				<label class="wp-slider-row">
					<span>Text Color</span>
					<input type="color" value={subStyle.color} oninput={(e) => patchStyle({ color: (e.currentTarget as HTMLInputElement).value })} />
				</label>
			</div>

			{#if (session?.probe?.subtitle_tracks.length ?? 0) > 0}
				<div class="wp-box">
					<div class="wp-box-title">Embedded File Tracks</div>
					<div class="wp-stack">
						{#each session?.probe?.subtitle_tracks.filter((s) => s.text) ?? [] as tr (tr.index)}
							<button
								type="button"
								class="wp-list-pick"
								class:on={subMode === 'embedded' && subIndex === tr.index}
								disabled={loadingSub}
								onclick={() => onLoadEmbedded(tr.index)}
							>
								<span>{[tr.language?.toUpperCase(), tr.title, tr.codec?.toUpperCase()].filter(Boolean).join(' · ') || `Track ${tr.index + 1}`}</span>
							</button>
						{/each}
					</div>
				</div>
			{/if}

			<div class="wp-box">
				<div class="wp-box-title">Search Online (Sites &amp; Stremio Addons)</div>
				<form class="wp-search-row" onsubmit={(e) => { e.preventDefault(); onSearchOnline(); }}>
					<input type="text" bind:value={siteSubLangsInput} placeholder="en, tr, ja" />
					<button type="submit" class="wp-mini-btn accent" disabled={searchingSubs}>{searchingSubs ? '...' : 'Search'}</button>
				</form>
				<div class="wp-upload-row">
					<input bind:this={fileInput} type="file" accept=".srt,.vtt,.ass,.ssa" hidden onchange={onUploadFile} />
					<button type="button" class="wp-mini-btn" onclick={() => fileInput?.click()}>Upload .srt / .vtt / .ass</button>
				</div>
				{#if searchingSubs}
					<p class="wp-muted">Searching subtitle providers...</p>
				{:else if siteSubCandidates.length === 0}
					<p class="wp-muted">No online subtitle candidates yet.</p>
				{:else}
					<div class="wp-stack">
						{#each siteSubCandidates as c (c.id)}
							<button type="button" class="wp-list-pick" disabled={loadingSub} onclick={() => onLoadSiteSub(c)}>
								<div class="wp-sub-cand">
									<span>{c.language?.toUpperCase()} · {c.release_names?.[0] || c.site_name}</span>
									<small>{c.site_name} · {c.site_kind}{c.hearing_impaired ? ' · HI' : ''}</small>
								</div>
							</button>
						{/each}
					</div>
				{/if}
			</div>
		</div>
	</aside>
{/if}

<style>
	.wp-drawer {
		position: absolute; top: 0; right: 0; bottom: 0; width: min(440px, 100vw); z-index: 12;
		background: rgb(14 14 18 / 94%); border-left: 1px solid rgb(255 255 255 / 14%);
		backdrop-filter: blur(22px); display: flex; flex-direction: column;
	}
	.wp-drawer-head { display: flex; align-items: center; justify-content: space-between; padding: 16px 18px; border-bottom: 1px solid rgb(255 255 255 / 12%); }
	.wp-drawer-head h3 { margin: 0; font-size: 16px; font-weight: 700; }
	.wp-close { width: 32px; height: 32px; border-radius: 50%; border: 0; background: rgb(255 255 255 / 12%); color: #fff; display: grid; place-items: center; cursor: pointer; }
	.wp-drawer-body { flex: 1; overflow-y: auto; padding: 14px 16px 28px; display: flex; flex-direction: column; gap: 12px; }
	.wp-box { padding: 12px; border-radius: 10px; background: rgb(255 255 255 / 5%); display: flex; flex-direction: column; gap: 10px; }
	.wp-box-title { font-size: 12px; font-weight: 700; color: rgb(255 255 255 / 72%); text-transform: uppercase; letter-spacing: .04em; }
	.wp-row-between, .wp-sync-row, .wp-slider-row { display: flex; align-items: center; justify-content: space-between; gap: 8px; font-size: 12.5px; }
	.wp-slider-row input[type="range"] { width: 150px; accent-color: #e50914; }
	.wp-check { display: flex; align-items: center; gap: 8px; font-size: 12.5px; cursor: pointer; }
	.wp-btns, .wp-tab-row { display: flex; flex-wrap: wrap; gap: 6px; }
	.wp-chip { padding: 5px 10px; border: 1px solid rgb(255 255 255 / 18%); border-radius: 999px; background: transparent; color: rgb(255 255 255 / 78%); font-size: 12px; font-weight: 600; cursor: pointer; }
	.wp-chip.on { background: #fff; color: #111; border-color: #fff; }
	.wp-search-row { display: flex; gap: 8px; }
	.wp-search-row input { flex: 1; height: 34px; padding: 0 10px; border-radius: 8px; border: 1px solid rgb(255 255 255 / 20%); background: rgb(28 28 34); color: #fff; font-size: 13px; }
	.wp-stack { display: flex; flex-direction: column; gap: 7px; }
	.wp-list-pick { display: flex; width: 100%; padding: 9px 12px; border: 1px solid rgb(255 255 255 / 14%); border-radius: 9px; background: rgb(255 255 255 / 5%); color: #fff; text-align: left; font-size: 13px; cursor: pointer; }
	.wp-list-pick.on { border-color: #e50914; background: rgb(229 9 20 / 15%); }
	.wp-sub-cand { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
	.wp-sub-cand span { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
	.wp-sub-cand small { color: rgb(255 255 255 / 62%); font-size: 11px; }
	.wp-mini-btn { padding: 6px 10px; border-radius: 8px; border: 1px solid rgb(255 255 255 / 20%); background: rgb(255 255 255 / 10%); color: #fff; font-size: 12px; font-weight: 600; cursor: pointer; }
	.wp-mini-btn.accent { background: #e50914; border-color: #e50914; }
	.wp-muted { margin: 0; color: rgb(255 255 255 / 66%); font-size: 13px; }
	.wp-err-inline { padding: 8px 10px; border-radius: 8px; background: rgb(239 68 68 / 20%); color: #fca5a5; font-size: 12.5px; }
</style>
