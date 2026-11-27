<script lang="ts">
	import Icon from '#lib/ui/Icon.svelte';
	import { formatDuration, formatSpeed, type ChapterMark, type SessionStatus, type StreamCandidate } from './stream-api.ts';
	import type { HlsQualityLevel } from './player-media.ts';
	import type { SkipSegment } from './streaming-config-api.ts';

	let {
		hudVisible,
		drawer,
		kind,
		headerTitle,
		headerSub,
		runtimeMin = 0,
		session,
		activeCandidate = null,
		candidatesCount = 0,
		loadingMoreSources = false,
		seamlessStatus = '',
		stalled = false,
		pipelineMode,
		paused,
		mpvAvailable,
		ffmpegAvailable,
		pipAvailable = false,
		isPiP = false,
		playbackRate,
		volume,
		muted,
		fitMode,
		isFullscreen,
		totalDuration,
		currentPos,
		scrubbedPct,
		bufferedPct,
		remainingText,
		endsAtText,
		chapters = [],
		skipSegments = [],
		activeSkip = null,
		skipCountdown = 0,
		hasNextEpisode = false,
		hlsLevels = [],
		hlsLevelIdx = -1,
		browserQuality = 'original',
		onLeave,
		onToggleDrawer,
		onTriggerMPV,
		onTogglePlayPause,
		onSeekBy,
		onScrubInput,
		onScrubCommit,
		onToggleMute,
		onVolumeChange,
		onSelectSpeed,
		onToggleFit,
		onSelectHlsLevel,
		onChangeBrowserQuality,
		onTogglePiP,
		onNextEpisode,
		onToggleFullscreen,
		onExecuteSkip,
		onCancelAutoSkip
	}: {
		hudVisible: boolean;
		drawer: 'none' | 'sources' | 'quality' | 'audioSubs' | 'episodes';
		kind: 'movie' | 'show';
		headerTitle: string;
		headerSub: string;
		runtimeMin?: number;
		session: SessionStatus | null;
		activeCandidate?: StreamCandidate | null;
		candidatesCount?: number;
		loadingMoreSources?: boolean;
		seamlessStatus?: string;
		stalled?: boolean;
		pipelineMode: 'none' | 'direct' | 'hls';
		paused: boolean;
		mpvAvailable: boolean;
		ffmpegAvailable: boolean;
		pipAvailable?: boolean;
		isPiP?: boolean;
		playbackRate: number;
		volume: number;
		muted: boolean;
		fitMode: 'contain' | 'cover';
		isFullscreen: boolean;
		totalDuration: number;
		currentPos: number;
		scrubbedPct: number;
		bufferedPct: number;
		remainingText: string;
		endsAtText: string;
		chapters?: ChapterMark[];
		skipSegments?: SkipSegment[];
		activeSkip?: SkipSegment | null;
		skipCountdown?: number;
		hasNextEpisode?: boolean;
		hlsLevels?: HlsQualityLevel[];
		hlsLevelIdx?: number;
		browserQuality?: 'original' | '1080p' | '720p' | '480p';
		onLeave: () => void;
		onToggleDrawer: (d: 'sources' | 'quality' | 'audioSubs' | 'episodes') => void;
		onTriggerMPV: () => void;
		onTogglePlayPause: () => void;
		onSeekBy: (delta: number) => void;
		onScrubInput: (sec: number) => void;
		onScrubCommit: (sec: number) => void;
		onToggleMute: () => void;
		onVolumeChange: (v: number) => void;
		onSelectSpeed: (spd: number) => void;
		onToggleFit: () => void;
		onSelectHlsLevel: (idx: number) => void;
		onChangeBrowserQuality: (q: 'original' | '1080p' | '720p' | '480p') => void;
		onTogglePiP: () => void;
		onNextEpisode: () => void;
		onToggleFullscreen: () => void;
		onExecuteSkip: (seg: SkipSegment) => void;
		onCancelAutoSkip: () => void;
	} = $props();

	let hoverSec = $state<number | null>(null);
	let hoverPct = $state(0);

	const hoverChapter = $derived.by(() => {
		if (hoverSec === null || chapters.length === 0) return '';
		const ch = chapters.find((c) => hoverSec! >= c.start && hoverSec! <= c.end);
		return ch?.title || '';
	});

	const subtitleLine = $derived.by(() => {
		const parts: string[] = [];
		if (headerSub) parts.push(headerSub);
		const mins = runtimeMin > 0 ? runtimeMin : totalDuration > 60 ? Math.round(totalDuration / 60) : 0;
		if (mins > 0) parts.push(`${mins}m`);
		return parts.join(' · ');
	});

	const qualityLabel = $derived.by(() => {
		if (hlsLevels.length > 1) {
			if (hlsLevelIdx < 0) return 'Auto';
			return hlsLevels.find((l) => l.index === hlsLevelIdx)?.label || 'Auto';
		}
		if (browserQuality !== 'original') return browserQuality;
		return activeCandidate?.quality || activeCandidate?.resolution || 'Original';
	});

	const sourceSummary = $derived.by(() => {
		if (!session) return '';
		const name = session.source_provider || session.source_name || 'Stream';
		const ms = activeCandidate?.probe_ms ? `${activeCandidate.probe_ms}ms` : '';
		return [name, ms].filter(Boolean).join(' · ');
	});

	function onTrackMove(e: MouseEvent) {
		if (totalDuration <= 0) return;
		const el = e.currentTarget as HTMLElement;
		const rect = el.getBoundingClientRect();
		const pct = Math.min(1, Math.max(0, (e.clientX - rect.left) / (rect.width || 1)));
		hoverPct = pct * 100;
		hoverSec = pct * totalDuration;
	}
</script>

<!-- Subtle top-center indicator when seamlessly re-seeking/swapping or genuinely stalled -->
{#if (seamlessStatus || stalled) && pipelineMode !== 'none'}
	<div class="wp-status-pill" aria-live="polite">
		<span class="wp-dot-spin"></span>
		<span>{seamlessStatus || 'Buffering...'}</span>
	</div>
{/if}

<!-- Top Movy.sx Overlay HUD -->
<header class="wp-top-bar" class:visible={hudVisible || paused || drawer !== 'none'}>
	<div class="wp-top-left">
		<button type="button" class="wp-ghost-ico" title="Back" onclick={onLeave}>
			<Icon name="back" size={20} />
		</button>
		<div class="wp-title-meta">
			<strong>{headerTitle}</strong>
			<div class="wp-sub-row">
				{#if subtitleLine}<span>{subtitleLine}</span>{/if}
				{#if sourceSummary}
					<button type="button" class="wp-src-badge" onclick={() => onToggleDrawer('sources')}>
						{sourceSummary}
						{#if session?.transport === 'torrent' && session.seeders > 0}
							· {session.seeders}s ({formatSpeed(session.download_bps)})
						{/if}
					</button>
				{/if}
			</div>
		</div>
	</div>

	<div class="wp-top-right">
		<button type="button" class="wp-ghost-btn" class:on={drawer === 'sources'} onclick={() => onToggleDrawer('sources')}>
			<Icon name="source" size={16} />
			<span>Sources{candidatesCount > 0 ? ` (${candidatesCount})` : ''}</span>
			{#if loadingMoreSources}<span class="wp-mini-dot"></span>{/if}
		</button>
		<button type="button" class="wp-ghost-btn" class:on={drawer === 'quality'} onclick={() => onToggleDrawer('quality')}>
			<Icon name="sliders" size={16} />
			<span>Quality</span>
			<small>{qualityLabel}</small>
		</button>
		<button type="button" class="wp-ghost-btn" class:on={drawer === 'audioSubs'} onclick={() => onToggleDrawer('audioSubs')}>
			<Icon name="subtitle" size={16} />
			<span>Audio &amp; Subtitles</span>
		</button>
		{#if kind === 'show' && hasNextEpisode}
			<button type="button" class="wp-ghost-btn accent" onclick={onNextEpisode}>
				<span>Next Episode</span>
				<Icon name="chevronRight" size={16} />
			</button>
		{/if}
	</div>
</header>

<!-- Compact Floating Quality & Playback Menu -->
{#if drawer === 'quality'}
	<aside class="wp-q-pop" aria-label="Quality and playback options">
		{#if hlsLevels.length > 1}
			<div class="wp-q-sec">
				<span class="wp-q-lbl">Stream Quality</span>
				<div class="wp-q-chips">
					<button type="button" class="wp-q-chip" class:on={hlsLevelIdx === -1} onclick={() => onSelectHlsLevel(-1)}>Auto</button>
					{#each hlsLevels as lv (lv.index)}
						<button type="button" class="wp-q-chip" class:on={hlsLevelIdx === lv.index} onclick={() => onSelectHlsLevel(lv.index)}>
							{lv.label}
						</button>
					{/each}
				</div>
			</div>
		{:else if session && session.transport !== 'hls_remote' && ffmpegAvailable}
			<div class="wp-q-sec">
				<span class="wp-q-lbl">Transcode Quality</span>
				<div class="wp-q-chips">
					{#each ['original', '1080p', '720p', '480p'] as q (q)}
						<button
							type="button"
							class="wp-q-chip"
							class:on={browserQuality === q}
							onclick={() => onChangeBrowserQuality(q as 'original' | '1080p' | '720p' | '480p')}
						>
							{q === 'original' ? 'Original' : q}
						</button>
					{/each}
				</div>
			</div>
		{/if}

		<div class="wp-q-sec">
			<span class="wp-q-lbl">Speed</span>
			<div class="wp-q-chips">
				{#each [0.75, 1, 1.25, 1.5, 2] as spd (spd)}
					<button type="button" class="wp-q-chip" class:on={playbackRate === spd} onclick={() => onSelectSpeed(spd)}>
						{spd}x
					</button>
				{/each}
			</div>
		</div>

		<div class="wp-q-sec">
			<span class="wp-q-lbl">Display &amp; External</span>
			<div class="wp-q-chips">
				<button type="button" class="wp-q-chip" class:on={fitMode === 'contain'} onclick={() => { if (fitMode !== 'contain') onToggleFit(); }}>Fit</button>
				<button type="button" class="wp-q-chip" class:on={fitMode === 'cover'} onclick={() => { if (fitMode !== 'cover') onToggleFit(); }}>Fill</button>
				{#if mpvAvailable && session}
					<button type="button" class="wp-q-chip" onclick={onTriggerMPV}>Open in MPV</button>
				{/if}
			</div>
		</div>
	</aside>
{/if}

<!-- Floating Skip Segment Button -->
{#if activeSkip && pipelineMode !== 'none'}
	<div class="wp-skip-wrap" class:hud-up={hudVisible || paused}>
		<button type="button" class="wp-skip-btn" onclick={() => onExecuteSkip(activeSkip!)}>
			<span>{activeSkip.label || 'Skip'}</span>
			{#if skipCountdown > 0}<small>in {skipCountdown}s</small>{/if}
		</button>
		{#if skipCountdown > 0}
			<button type="button" class="wp-skip-cancel" title="Cancel auto-skip" onclick={onCancelAutoSkip}>
				<Icon name="x" size={14} />
			</button>
		{/if}
	</div>
{/if}

<!-- Bottom Movy.sx Playback Controls HUD -->
<footer class="wp-bottom-bar" class:visible={(hudVisible || paused || drawer !== 'none') && pipelineMode !== 'none'} class:hidden={pipelineMode === 'none'}>
	<div class="wp-clock-line">
		{#if endsAtText}<span class="wp-clock-end">{remainingText ? `${remainingText} · ` : ''}{endsAtText}</span>{/if}
		<span class="wp-clock-num">{formatDuration(currentPos)} <small>/ {formatDuration(totalDuration)}</small></span>
	</div>

	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="wp-scrub-wrap" onmousemove={onTrackMove} onmouseleave={() => { hoverSec = null; }}>
		{#if hoverSec !== null}
			<div class="wp-hover-tip" style:left="clamp(42px, {hoverPct}%, calc(100% - 42px))">
				<span>{formatDuration(hoverSec)}</span>
				{#if hoverChapter}<small>{hoverChapter}</small>{/if}
			</div>
		{/if}
		<div class="wp-scrub-track">
			<div class="wp-scrub-buf" style:width="{bufferedPct}%"></div>
			{#each skipSegments as seg (seg.kind + ':' + seg.start)}
				{#if totalDuration > 0}
					<div class="wp-seg-mark" style:left="{Math.min(100, (seg.start / totalDuration) * 100)}%" style:width="{Math.min(100, Math.max(0.6, ((seg.end - seg.start) / totalDuration) * 100))}%"></div>
				{/if}
			{/each}
			{#each chapters as ch (ch.start)}
				{#if totalDuration > 0 && ch.start > 0}
					<div class="wp-ch-tick" style:left="{Math.min(100, (ch.start / totalDuration) * 100)}%"></div>
				{/if}
			{/each}
			<div class="wp-scrub-fill" style:width="{scrubbedPct}%"></div>
		</div>
		<div class="wp-scrub-knob" style:left="{scrubbedPct}%"></div>
		<input
			type="range"
			class="wp-scrub-input"
			min="0"
			max={totalDuration || 100}
			step="0.5"
			value={currentPos}
			aria-label="Playback position"
			oninput={(e) => onScrubInput(Number((e.currentTarget as HTMLInputElement).value))}
			onchange={(e) => onScrubCommit(Number((e.currentTarget as HTMLInputElement).value))}
		/>
	</div>

	<div class="wp-ctrl-row">
		<div class="wp-ctrl-left">
			<button type="button" class="wp-ghost-ico" title={paused ? 'Play' : 'Pause'} onclick={onTogglePlayPause}>
				<Icon name={paused ? 'play' : 'pause'} size={22} />
			</button>
			<button type="button" class="wp-ghost-ico wp-step" title="Back 10s" onclick={() => onSeekBy(-10)}>-10</button>
			<button type="button" class="wp-ghost-ico wp-step" title="Forward 10s" onclick={() => onSeekBy(10)}>+10</button>
			<div class="wp-vol-group">
				<button type="button" class="wp-ghost-ico" title={muted ? 'Unmute' : 'Mute'} onclick={onToggleMute}>
					<Icon name={muted || volume === 0 ? 'soundOff' : 'sound'} size={18} />
				</button>
				<input
					type="range"
					class="wp-vol-slider"
					min="0"
					max="1"
					step="0.05"
					value={muted ? 0 : volume}
					aria-label="Volume"
					oninput={(e) => onVolumeChange(Number((e.currentTarget as HTMLInputElement).value))}
				/>
			</div>
		</div>

		<div class="wp-ctrl-center">
			{#if kind === 'show'}
				<button type="button" class="wp-ep-toggle" class:on={drawer === 'episodes'} onclick={() => onToggleDrawer('episodes')}>
					<span>Episodes</span>
					<Icon name={drawer === 'episodes' ? 'chevronDown' : 'chevronUp'} size={15} />
				</button>
			{/if}
		</div>

		<div class="wp-ctrl-right">
			{#if pipAvailable}
				<button type="button" class="wp-ghost-ico" class:on={isPiP} title="Picture in Picture" onclick={onTogglePiP}>
					<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
						<rect x="3" y="5" width="18" height="14" rx="2" />
						<rect x="12" y="11" width="7" height="5" rx="1" fill="currentColor" stroke="none" />
					</svg>
				</button>
			{/if}
			<button type="button" class="wp-ghost-ico" title={isFullscreen ? 'Exit fullscreen' : 'Fullscreen'} onclick={onToggleFullscreen}>
				<Icon name="expand" size={18} />
			</button>
		</div>
	</div>
</footer>

<style>
	.wp-status-pill {
		position: absolute; top: 18px; left: 50%; transform: translateX(-50%); z-index: 15;
		display: inline-flex; align-items: center; gap: 8px; padding: 7px 14px; border-radius: 999px;
		background: rgb(16 16 22 / 84%); border: 1px solid rgb(255 255 255 / 16%);
		backdrop-filter: blur(16px); font-size: 12px; font-weight: 600; pointer-events: none;
	}
	.wp-dot-spin { width: 12px; height: 12px; border-radius: 50%; border: 2px solid rgb(255 255 255 / 22%); border-top-color: #e50914; animation: wp-spin 0.7s linear infinite; }
	@keyframes wp-spin { to { transform: rotate(360deg); } }

	.wp-top-bar, .wp-bottom-bar { position: absolute; left: 0; right: 0; z-index: 9; opacity: 0; pointer-events: none; transition: opacity 200ms ease; }
	.wp-top-bar.visible, .wp-bottom-bar.visible { opacity: 1; pointer-events: auto; }
	.wp-bottom-bar.hidden { opacity: 0 !important; pointer-events: none !important; }

	.wp-top-bar { top: 0; display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 18px 24px 48px; background: linear-gradient(to bottom, rgb(0 0 0 / 82%), rgb(0 0 0 / 32%) 62%, transparent); }
	.wp-top-left { display: flex; align-items: center; gap: 12px; min-width: 0; }
	.wp-top-right { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
	.wp-title-meta { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
	.wp-title-meta strong { font-size: 16px; font-weight: 700; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
	.wp-sub-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; color: rgb(255 255 255 / 70%); font-size: 12.5px; }
	.wp-src-badge { border: 0; padding: 2px 7px; border-radius: 6px; background: rgb(255 255 255 / 11%); color: rgb(255 255 255 / 84%); font-size: 11px; font-weight: 600; cursor: pointer; }
	.wp-src-badge:hover { background: rgb(255 255 255 / 20%); color: #fff; }

	.wp-ghost-ico { width: 38px; height: 38px; border-radius: 10px; border: 0; background: transparent; color: rgb(255 255 255 / 90%); display: grid; place-items: center; cursor: pointer; transition: background 140ms ease, transform 120ms ease; }
	.wp-ghost-ico:hover { background: rgb(255 255 255 / 14%); color: #fff; }
	.wp-ghost-ico.on { color: #e50914; }
	.wp-ghost-ico.wp-step { font-size: 12px; font-weight: 800; }
	.wp-ghost-btn { display: inline-flex; align-items: center; gap: 7px; height: 36px; padding: 0 11px; border-radius: 10px; border: 0; background: transparent; color: rgb(255 255 255 / 92%); font: 600 13px/1 var(--font-ui); cursor: pointer; transition: background 140ms ease; }
	.wp-ghost-btn:hover { background: rgb(255 255 255 / 14%); }
	.wp-ghost-btn.on { background: rgb(255 255 255 / 18%); color: #fff; }
	.wp-ghost-btn.accent { color: #fff; }
	.wp-ghost-btn.accent:hover { background: rgb(229 9 20 / 28%); }
	.wp-ghost-btn small { color: rgb(255 255 255 / 58%); font-weight: 500; }
	.wp-mini-dot { width: 6px; height: 6px; border-radius: 50%; background: #38bdf8; animation: wp-pulse 1s ease infinite; }
	@keyframes wp-pulse { 50% { opacity: 0.3; } }

	.wp-q-pop { position: absolute; top: 62px; right: 24px; width: min(320px, calc(100vw - 32px)); z-index: 14; padding: 14px; border-radius: 16px; background: rgb(16 16 22 / 92%); border: 1px solid rgb(255 255 255 / 14%); backdrop-filter: blur(22px); display: flex; flex-direction: column; gap: 12px; box-shadow: 0 18px 44px rgb(0 0 0 / 55%); }
	.wp-q-sec { display: flex; flex-direction: column; gap: 7px; }
	.wp-q-lbl { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .05em; color: rgb(255 255 255 / 60%); }
	.wp-q-chips { display: flex; flex-wrap: wrap; gap: 6px; }
	.wp-q-chip { padding: 6px 11px; border-radius: 999px; border: 1px solid rgb(255 255 255 / 16%); background: rgb(255 255 255 / 6%); color: rgb(255 255 255 / 86%); font-size: 12px; font-weight: 600; cursor: pointer; }
	.wp-q-chip.on { background: #fff; color: #111; border-color: #fff; }

	.wp-skip-wrap { position: absolute; right: 26px; bottom: 54px; z-index: 10; display: flex; align-items: center; gap: 6px; transition: bottom 180ms ease; }
	.wp-skip-wrap.hud-up { bottom: 118px; }
	.wp-skip-btn { display: inline-flex; align-items: center; gap: 8px; padding: 10px 18px; border-radius: 10px; border: 1px solid rgb(255 255 255 / 38%); background: rgb(16 16 22 / 88%); color: #fff; font: 700 13.5px/1 var(--font-ui); cursor: pointer; backdrop-filter: blur(12px); }
	.wp-skip-btn small { color: #fcd34d; font-weight: 600; }
	.wp-skip-cancel { width: 32px; height: 32px; border-radius: 10px; border: 1px solid rgb(255 255 255 / 26%); background: rgb(16 16 22 / 82%); color: #fff; display: grid; place-items: center; cursor: pointer; }

	.wp-bottom-bar { bottom: 0; padding: 40px 24px 16px; display: flex; flex-direction: column; gap: 6px; background: linear-gradient(to top, rgb(0 0 0 / 88%), rgb(0 0 0 / 42%) 62%, transparent); }
	.wp-clock-line { display: flex; justify-content: flex-end; align-items: center; gap: 12px; font-size: 12px; font-weight: 600; color: rgb(255 255 255 / 86%); }
	.wp-clock-end { color: rgb(255 255 255 / 58%); }
	.wp-clock-num small { color: rgb(255 255 255 / 55%); font-weight: 500; font-size: 12px; }

	.wp-scrub-wrap { position: relative; height: 20px; display: flex; align-items: center; }
	.wp-hover-tip { position: absolute; bottom: 24px; transform: translateX(-50%); padding: 4px 9px; border-radius: 6px; background: rgb(16 16 22 / 94%); border: 1px solid rgb(255 255 255 / 20%); font-size: 11.5px; font-weight: 700; display: flex; flex-direction: column; align-items: center; white-space: nowrap; pointer-events: none; }
	.wp-hover-tip small { color: rgb(255 255 255 / 70%); font-weight: 500; }
	.wp-scrub-track { position: relative; width: 100%; height: 4px; border-radius: 999px; background: rgb(255 255 255 / 24%); overflow: hidden; transition: height 120ms ease; }
	.wp-scrub-wrap:hover .wp-scrub-track { height: 7px; }
	.wp-scrub-buf { position: absolute; left: 0; top: 0; bottom: 0; background: rgb(255 255 255 / 34%); }
	.wp-seg-mark { position: absolute; top: 0; bottom: 0; background: rgb(250 204 21 / 55%); z-index: 1; }
	.wp-ch-tick { position: absolute; top: 0; bottom: 0; width: 2px; background: rgb(255 255 255 / 85%); z-index: 2; }
	.wp-scrub-fill { position: relative; z-index: 3; height: 100%; background: #fff; transition: box-shadow 120ms ease; }
	.wp-scrub-wrap:hover .wp-scrub-fill { box-shadow: 0 0 12px rgb(255 255 255 / 48%); }
	.wp-scrub-knob { position: absolute; top: 50%; width: 12px; height: 12px; border-radius: 50%; background: #fff; transform: translate(-50%, -50%) scale(0.8); pointer-events: none; z-index: 4; box-shadow: 0 0 8px rgb(0 0 0 / 60%); transition: transform 120ms ease; }
	.wp-scrub-wrap:hover .wp-scrub-knob { transform: translate(-50%, -50%) scale(1.1); }
	.wp-scrub-input { position: absolute; inset: 0; width: 100%; height: 100%; opacity: 0; cursor: pointer; z-index: 5; margin: 0; }

	.wp-ctrl-row { display: grid; grid-template-columns: 1fr auto 1fr; align-items: center; gap: 10px; }
	.wp-ctrl-left { display: flex; align-items: center; gap: 6px; }
	.wp-ctrl-center { display: flex; justify-content: center; }
	.wp-ctrl-right { display: flex; align-items: center; justify-content: flex-end; gap: 6px; }
	.wp-vol-group { display: flex; align-items: center; gap: 4px; }
	.wp-vol-slider { width: 0; opacity: 0; accent-color: #fff; cursor: pointer; transition: width 160ms ease, opacity 160ms ease; }
	.wp-vol-group:hover .wp-vol-slider, .wp-vol-slider:focus-visible { width: 72px; opacity: 1; }
	.wp-ep-toggle { display: inline-flex; align-items: center; gap: 6px; padding: 7px 14px; border-radius: 999px; border: 0; background: transparent; color: #fff; font: 700 13px/1 var(--font-ui); cursor: pointer; }
	.wp-ep-toggle:hover, .wp-ep-toggle.on { background: rgb(255 255 255 / 14%); }

	@media (max-width: 720px) {
		.wp-ghost-btn span { display: none; }
		.wp-clock-end, .wp-vol-slider { display: none !important; }
	}
</style>
