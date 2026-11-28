<script lang="ts">
	import Icon from '#lib/ui/Icon.svelte';
	import type { SessionStatus, StreamSubCandidate } from './stream-api.ts';
	import type { SubStylePrefs } from './streaming-config-api.ts';

	let {
		open,
		session,
		audioIdx = 0,
		ffmpegAvailable = false,
		boost, secondary, target, onTarget, onClearSecondary, onBoost, onOpenStyle,
		subMode,
		subIndex,
		subLabel,
		subDelayMS,
		subSize,
		activeSubVtt = '',
		syncingSub = false,
		syncNote = '',
		subStyle = $bindable<SubStylePrefs>(),
		siteSubLangsInput = $bindable('en, tr'),
		siteSubCandidates,
		searchingSubs,
		loadingSub,
		subError,
		onClose,
		onChangeAudio,
		onTurnOff,
		onLoadEmbedded,
		onSearchOnline,
		onLoadSiteSub,
		onAutoFindAndSync,
		onSyncCurrentSub,
		onAdjustDelay,
		onChangeSize,
		onUploadFile,
		onStyleChange
	}: {
		open: boolean;
		session: SessionStatus | null;
		audioIdx?: number;
		ffmpegAvailable?: boolean;
		boost: number; secondary: string; target: 'primary' | 'secondary';
		onTarget: (target: 'primary' | 'secondary') => void; onClearSecondary: () => void;
		onBoost: (percent: number) => void; onOpenStyle: () => void;
		subMode: 'off' | 'embedded' | 'site' | 'upload';
		subIndex: number;
		subLabel: string;
		subDelayMS: number;
		subSize: 'sm' | 'md' | 'lg' | 'xl';
		activeSubVtt?: string;
		syncingSub?: boolean;
		syncNote?: string;
		subStyle: SubStylePrefs;
		siteSubLangsInput: string;
		siteSubCandidates: StreamSubCandidate[];
		searchingSubs: boolean;
		loadingSub: boolean;
		subError: string;
		onClose: () => void;
		onChangeAudio: (idx: number) => void;
		onTurnOff: () => void;
		onLoadEmbedded: (idx: number) => void;
		onSearchOnline: () => void;
		onLoadSiteSub: (c: StreamSubCandidate) => void;
		onAutoFindAndSync?: () => void;
		onSyncCurrentSub?: () => void;
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
	<aside class="wp-pop" aria-label="Audio and subtitles menu">
		<div class="wp-pop-head">
			<h3>Audio &amp; Subtitles</h3>
			<button type="button" class="wp-close" onclick={onClose}><Icon name="x" size={16} /></button>
		</div>

		<div class="wp-pop-body">
			{#if subError}<div class="wp-err-inline">{subError}</div>{/if}
			<div class="wp-box"><label class="wp-slider-row"><span>Volume boost ({boost}%)</span><input type="range" min="100" max="300" step="5" value={boost} oninput={(event) => onBoost(Number(event.currentTarget.value))} /></label></div>

			{#if (session?.probe?.audio_tracks.length ?? 0) > 0}
				<div class="wp-box">
					<div class="wp-box-title">Audio Track</div>
					<div class="wp-stack">
						{#each session?.probe?.audio_tracks ?? [] as tr (tr.index)}
							<button
								type="button"
								class="wp-list-pick"
								class:on={audioIdx === tr.index}
								disabled={!ffmpegAvailable}
								onclick={() => onChangeAudio(tr.index)}
							>
								<span>{[tr.language?.toUpperCase(), tr.title, tr.codec?.toUpperCase(), tr.channels ? `${tr.channels}ch` : ''].filter(Boolean).join(' · ') || `Audio ${tr.index + 1}`}</span>
							</button>
						{/each}
					</div>
				</div>
			{/if}

			<div class="wp-box">
				<div class="wp-row-between">
					<strong>Subtitle: {subMode === 'off' ? 'Off' : subLabel || 'Loaded'}</strong>
					{#if subMode !== 'off'}<button type="button" class="wp-mini-btn" onclick={onTurnOff}>Turn Off</button>{/if}
				</div>
				<div class="wp-btns">
					<button type="button" class="wp-mini-btn" aria-pressed={target === 'primary'} onclick={() => onTarget('primary')}>Primary track</button>
					<button type="button" class="wp-mini-btn" aria-pressed={target === 'secondary'} onclick={() => onTarget('secondary')}>{secondary || '+ Secondary track'}</button>
					{#if secondary}<button type="button" class="wp-mini-btn" onclick={onClearSecondary}>Remove secondary</button>{/if}
				</div>
				<div class="wp-sync-row">
					<span>Timing ({subDelayMS >= 0 ? `+${subDelayMS}` : subDelayMS} ms)</span>
					<div class="wp-btns">
						<button type="button" class="wp-mini-btn" onclick={() => onAdjustDelay(-250)}>-0.25s</button>
						<label class="wp-delay"><span class="sr-only">Subtitle delay in milliseconds</span><input type="number" step="50" value={subDelayMS} onchange={(e) => { const next = Number(e.currentTarget.value); if (Number.isFinite(next)) onAdjustDelay(Math.round(next) - subDelayMS); }} /></label>
						<button type="button" class="wp-mini-btn" onclick={() => onAdjustDelay(250)}>+0.25s</button>
						<button type="button" class="wp-mini-btn" onclick={() => onAdjustDelay(-subDelayMS)}>Reset</button>
					</div>
				</div>
				{#if session && ffmpegAvailable && (onAutoFindAndSync || onSyncCurrentSub)}
					<div class="wp-btns">
						{#if onAutoFindAndSync}
							<button type="button" class="wp-mini-btn accent" disabled={syncingSub || loadingSub} onclick={onAutoFindAndSync}>
								{syncingSub ? 'Listening to audio...' : 'Auto-Find & Sync (Audio)'}
							</button>
						{/if}
						{#if onSyncCurrentSub && activeSubVtt && subMode !== 'off'}
							<button type="button" class="wp-mini-btn" disabled={syncingSub || loadingSub} onclick={onSyncCurrentSub}>
								{syncingSub ? '...' : 'Sync to Audio'}
							</button>
						{/if}
					</div>
					{#if syncNote}<small class="wp-sync-note">{syncNote}</small>{/if}
				{/if}
				<div class="wp-tab-row">
					{#each ['sm', 'md', 'lg', 'xl'] as sz (sz)}
						<button type="button" class="wp-chip" class:on={subSize === sz} onclick={() => onChangeSize(sz as typeof subSize)}>{sz.toUpperCase()}</button>
					{/each}
				</div>
			</div>

			<div class="wp-box">
				<div class="wp-box-title">Appearance &amp; Filters</div>
				<button type="button" class="wp-mini-btn" onclick={onOpenStyle}>Scoped subtitle style</button>
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
				<label class="wp-slider-row"><span>Background color</span><input type="color" value={subStyle.bg_color} oninput={(e) => patchStyle({ bg_color: e.currentTarget.value })} /></label>
				<label class="wp-slider-row"><span>Font family</span><select value={subStyle.font_family} onchange={(e) => patchStyle({ font_family: e.currentTarget.value as SubStylePrefs['font_family'] })}><option value="sans">Sans</option><option value="serif">Serif</option><option value="mono">Mono</option></select></label>
			</div>

			{#if (session?.probe?.subtitle_tracks.length ?? 0) > 0}
				<div class="wp-box">
					<div class="wp-box-title">Embedded File Tracks</div>
					<div class="wp-stack">
						{#each session?.probe?.subtitle_tracks.filter((s) => s.text) ?? [] as tr (tr.index)}
							<button type="button" class="wp-list-pick" class:on={subMode === 'embedded' && subIndex === tr.index} disabled={loadingSub} onclick={() => onLoadEmbedded(tr.index)}>
								<span>{[tr.language?.toUpperCase(), tr.title, tr.codec?.toUpperCase()].filter(Boolean).join(' · ') || `Track ${tr.index + 1}`}</span>
							</button>
						{/each}
					</div>
				</div>
			{/if}

			<div class="wp-box">
				<div class="wp-box-title">Online Subtitles &amp; Upload</div>
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
	.wp-pop { position: absolute; bottom: 122px; right: clamp(8px, 3vw, 36px); width: min(410px, calc(100vw - 32px)); max-height: min(62vh, 560px); z-index: 14; background: rgb(24 24 27 / 97%); border: 1px solid rgb(255 255 255 / 10%); border-radius: 12px; backdrop-filter: blur(22px); display: flex; flex-direction: column; box-shadow: 0 20px 50px rgb(0 0 0 / 60%); animation: wp-pop-in 150ms cubic-bezier(0.22, 1, 0.36, 1); }
	@keyframes wp-pop-in { from { opacity: 0; transform: translateY(-6px) scale(0.985); } }
	.wp-pop-head { display: flex; align-items: center; justify-content: space-between; padding: 13px 16px; border-bottom: 1px solid rgb(255 255 255 / 10%); }
	.wp-pop-head h3 { margin: 0; font-size: 15px; font-weight: 700; }
	.wp-close { width: 28px; height: 28px; border-radius: 50%; border: 0; background: rgb(255 255 255 / 12%); color: #fff; display: grid; place-items: center; cursor: pointer; }
	.wp-pop-body { flex: 1; min-height: 0; overflow-y: auto; padding: 12px 14px 16px; display: flex; flex-direction: column; gap: 10px; scrollbar-width: thin; scrollbar-color: rgb(255 255 255 / 24%) transparent; }
	.wp-pop-body::-webkit-scrollbar { width: 6px; }
	.wp-pop-body::-webkit-scrollbar-thumb { background: rgb(255 255 255 / 24%); border-radius: 999px; }
	.wp-sync-note { color: #86efac; font-size: 11.5px; }
	.wp-box { padding: 11px; border-radius: 10px; background: rgb(255 255 255 / 5%); display: flex; flex-direction: column; gap: 9px; }
	.wp-box-title { font-size: 11px; font-weight: 700; color: rgb(255 255 255 / 66%); text-transform: uppercase; letter-spacing: .04em; }
	.wp-row-between, .wp-sync-row, .wp-slider-row { display: flex; align-items: center; justify-content: space-between; gap: 8px; font-size: 12.5px; }
	.wp-delay { display: flex; }
	.wp-delay input { width: 68px; padding: 0 6px; border: 1px solid rgb(255 255 255 / 14%); border-radius: 4px; background: #26262b; color: inherit; font: inherit; font-variant-numeric: tabular-nums; }
	.wp-slider-row input[type="range"] { width: 140px; accent-color: #fff; }
	.wp-check { display: flex; align-items: center; gap: 8px; font-size: 12.5px; cursor: pointer; }
	.wp-btns, .wp-tab-row { display: flex; flex-wrap: wrap; gap: 6px; }
	.wp-chip { padding: 4px 9px; border: 1px solid rgb(255 255 255 / 16%); border-radius: 999px; background: transparent; color: rgb(255 255 255 / 78%); font-size: 11.5px; font-weight: 600; cursor: pointer; }
	.wp-chip.on { background: #fff; color: #111; border-color: #fff; }
	.wp-search-row { display: flex; gap: 8px; }
	.wp-search-row input { flex: 1; height: 34px; padding: 0 10px; border-radius: 8px; border: 1px solid rgb(255 255 255 / 20%); background: rgb(24 24 27); color: #fff; font-size: 12.5px; }
	.wp-stack { display: flex; flex-direction: column; gap: 6px; }
	.wp-list-pick { display: flex; width: 100%; padding: 8px 11px; border: 1px solid rgb(255 255 255 / 12%); border-radius: 9px; background: rgb(255 255 255 / 5%); color: #fff; text-align: left; font-size: 12.5px; cursor: pointer; }
	.wp-list-pick.on { border-color: rgb(255 255 255 / 55%); background: rgb(255 255 255 / 8%); }
	.wp-sub-cand { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
	.wp-sub-cand span { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
	.wp-sub-cand small { color: rgb(255 255 255 / 62%); font-size: 11px; }
	.wp-mini-btn { padding: 6px 10px; border-radius: 8px; border: 1px solid rgb(255 255 255 / 20%); background: rgb(255 255 255 / 10%); color: #fff; font-size: 11.5px; font-weight: 600; cursor: pointer; }
	.wp-mini-btn.accent { background: #f4f4f5; border-color: #f4f4f5; color: #09090b; }
	.wp-muted { margin: 0; color: rgb(255 255 255 / 66%); font-size: 12.5px; }
	.wp-err-inline { padding: 7px 10px; border-radius: 8px; background: rgb(239 68 68 / 20%); color: #fca5a5; font-size: 12px; }
</style>
