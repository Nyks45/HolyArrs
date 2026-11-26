<script lang="ts">
	import Icon from '#lib/ui/Icon.svelte';
	import { formatDuration, formatSpeed, type ChapterMark, type SessionStatus } from './stream-api.ts';
	import type { SkipSegment } from './streaming-config-api.ts';

	let {
		hudVisible,
		drawer,
		kind,
		headerTitle,
		headerSub,
		session,
		pipelineMode,
		paused,
		mpvAvailable,
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
		onLeave,
		onToggleDrawer,
		onTriggerMPV,
		onTogglePlayPause,
		onSeekBy,
		onScrubInput,
		onScrubCommit,
		onToggleMute,
		onVolumeChange,
		onCycleSpeed,
		onToggleFit,
		onNextEpisode,
		onShuffleEpisode,
		onToggleFullscreen,
		onExecuteSkip,
		onCancelAutoSkip
	}: {
		hudVisible: boolean;
		drawer: 'none' | 'sources' | 'audioSubs' | 'episodes';
		kind: 'movie' | 'show';
		headerTitle: string;
		headerSub: string;
		session: SessionStatus | null;
		pipelineMode: 'none' | 'direct' | 'hls';
		paused: boolean;
		mpvAvailable: boolean;
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
		onLeave: () => void;
		onToggleDrawer: (d: 'sources' | 'audioSubs' | 'episodes') => void;
		onTriggerMPV: () => void;
		onTogglePlayPause: () => void;
		onSeekBy: (delta: number) => void;
		onScrubInput: (sec: number) => void;
		onScrubCommit: (sec: number) => void;
		onToggleMute: () => void;
		onVolumeChange: (v: number) => void;
		onCycleSpeed: () => void;
		onToggleFit: () => void;
		onNextEpisode: () => void;
		onShuffleEpisode: () => void;
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

	const transportBadge = $derived.by(() => {
		const tr = session?.transport ?? 'torrent';
		if (tr === 'local') return { cls: 'local', label: session?.source_provider || 'Local Library' };
		if (tr === 'direct' || tr === 'hls_remote') return { cls: 'instant', label: session?.source_provider || 'Instant' };
		return { cls: 'torrent', label: session?.source_provider || session?.source_name || 'Torrent' };
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

<!-- Top Overlay HUD -->
<header class="wp-top-bar" class:visible={hudVisible || paused || drawer !== 'none'}>
	<div class="wp-top-left">
		<button type="button" class="wp-icon-btn" title="Back" onclick={onLeave}>
			<Icon name="back" size={20} />
		</button>
		<div class="wp-title-meta">
			<div class="wp-title-line">
				<strong>{headerTitle}</strong>
				{#if headerSub}<span class="wp-sub-ep">{headerSub}</span>{/if}
			</div>
			{#if session}
				<div class="wp-badge-line">
					<span class="wp-tr-pill {transportBadge.cls}">{transportBadge.label}</span>
					{#if session.transport === 'torrent' || !session.transport}
						<span class="wp-telemetry-pill">
							{session.seeders} seeds · {formatSpeed(session.download_bps)}
						</span>
					{/if}
				</div>
			{/if}
		</div>
	</div>

	<div class="wp-top-right">
		{#if kind === 'show'}
			<button type="button" class="wp-pill-btn" class:on={drawer === 'episodes'} onclick={() => onToggleDrawer('episodes')}>
				<Icon name="list" size={16} />
				<span>Episodes</span>
			</button>
			<button type="button" class="wp-pill-btn" title="Shuffle Episode" onclick={onShuffleEpisode}>
				<Icon name="sparkle" size={15} />
				<span>Shuffle</span>
			</button>
		{/if}
		<button type="button" class="wp-pill-btn" class:on={drawer === 'audioSubs'} onclick={() => onToggleDrawer('audioSubs')}>
			<Icon name="subtitle" size={16} />
			<span>Audio &amp; Subs</span>
		</button>
		<button type="button" class="wp-pill-btn" class:on={drawer === 'sources'} onclick={() => onToggleDrawer('sources')}>
			<Icon name="source" size={16} />
			<span>Sources</span>
		</button>
		{#if mpvAvailable && session}
			<button type="button" class="wp-pill-btn" title="Open in desktop MPV" onclick={onTriggerMPV}>
				<Icon name="expand" size={15} />
				<span>MPV</span>
			</button>
		{/if}
	</div>
</header>

<!-- Floating Skip Segment Button (Intro / Recap / Outro / Credits) -->
{#if activeSkip && pipelineMode !== 'none'}
	<div class="wp-skip-wrap" class:hud-up={hudVisible || paused}>
		<button type="button" class="wp-skip-btn" onclick={() => onExecuteSkip(activeSkip!)}>
			<span>{activeSkip.label || 'Skip'}</span>
			{#if skipCountdown > 0}
				<small>in {skipCountdown}s</small>
			{/if}
		</button>
		{#if skipCountdown > 0}
			<button type="button" class="wp-skip-cancel" title="Cancel auto-skip" onclick={onCancelAutoSkip}>
				<Icon name="x" size={14} />
			</button>
		{/if}
	</div>
{/if}

<!-- Bottom Playback Controls HUD -->
<footer class="wp-bottom-bar" class:visible={(hudVisible || paused) && pipelineMode !== 'none'} class:hidden={pipelineMode === 'none'}>
	<!-- Timeline Scrubber with Chapter Ticks & Skip Highlights -->
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="wp-scrub-wrap" onmousemove={onTrackMove} onmouseleave={() => { hoverSec = null; }}>
		{#if hoverSec !== null}
			<div class="wp-hover-tip" style:left="{hoverPct}%">
				<span>{formatDuration(hoverSec)}</span>
				{#if hoverChapter}<small>{hoverChapter}</small>{/if}
			</div>
		{/if}
		<div class="wp-scrub-track">
			<div class="wp-scrub-buf" style:width="{bufferedPct}%"></div>
			{#each skipSegments as seg (seg.kind + ':' + seg.start)}
				{#if totalDuration > 0}
					<div
						class="wp-seg-mark"
						style:left="{Math.min(100, (seg.start / totalDuration) * 100)}%"
						style:width="{Math.min(100, Math.max(0.6, ((seg.end - seg.start) / totalDuration) * 100))}%"
					></div>
				{/if}
			{/each}
			{#each chapters as ch (ch.start)}
				{#if totalDuration > 0 && ch.start > 0}
					<div class="wp-ch-tick" style:left="{Math.min(100, (ch.start / totalDuration) * 100)}%"></div>
				{/if}
			{/each}
			<div class="wp-scrub-fill" style:width="{scrubbedPct}%"></div>
		</div>
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
		<div class="wp-ctrl-group">
			<button type="button" class="wp-icon-btn" title={paused ? 'Play' : 'Pause'} onclick={onTogglePlayPause}>
				<Icon name={paused ? 'play' : 'pause'} size={21} />
			</button>
			<button type="button" class="wp-icon-btn wp-step" title="Back 10s" onclick={() => onSeekBy(-10)}>-10</button>
			<button type="button" class="wp-icon-btn wp-step" title="Forward 10s" onclick={() => onSeekBy(10)}>+10</button>
			<div class="wp-vol-group">
				<button type="button" class="wp-icon-btn" title={muted ? 'Unmute' : 'Mute'} onclick={onToggleMute}>
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
			<div class="wp-clock">
				<span>{formatDuration(currentPos)}</span>
				<span class="wp-dim">/ {formatDuration(totalDuration)}</span>
				{#if remainingText}<span class="wp-clock-rem">· {remainingText}</span>{/if}
				{#if endsAtText}<span class="wp-clock-end">· {endsAtText}</span>{/if}
			</div>
		</div>

		<div class="wp-ctrl-group">
			<button type="button" class="wp-pill-btn" title="Playback speed" onclick={onCycleSpeed}>{playbackRate}x</button>
			<button type="button" class="wp-pill-btn" title="Video aspect fit" onclick={onToggleFit}>
				{fitMode === 'cover' ? 'Fill' : 'Fit'}
			</button>
			{#if kind === 'show' && hasNextEpisode}
				<button type="button" class="wp-pill-btn accent" onclick={onNextEpisode}>
					<span>Next Ep</span>
					<Icon name="chevronRight" size={15} />
				</button>
			{/if}
			<button type="button" class="wp-icon-btn" title={isFullscreen ? 'Exit fullscreen' : 'Fullscreen'} onclick={onToggleFullscreen}>
				<Icon name="expand" size={18} />
			</button>
		</div>
	</div>
</footer>

<style>
	.wp-top-bar, .wp-bottom-bar {
		position: absolute; left: 0; right: 0; z-index: 9;
		opacity: 0; pointer-events: none; transition: opacity 220ms ease;
	}
	.wp-top-bar.visible, .wp-bottom-bar.visible { opacity: 1; pointer-events: auto; }
	.wp-bottom-bar.hidden { opacity: 0 !important; pointer-events: none !important; }

	.wp-top-bar {
		top: 0; display: flex; align-items: center; justify-content: space-between; gap: 14px;
		padding: 18px 22px 44px;
		background: linear-gradient(to bottom, rgb(0 0 0 / 84%), rgb(0 0 0 / 35%) 62%, transparent);
	}
	.wp-top-left, .wp-top-right, .wp-ctrl-group { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
	.wp-title-meta { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
	.wp-title-line { display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap; font-size: 16px; }
	.wp-sub-ep { color: rgb(255 255 255 / 72%); font-size: 13px; font-weight: 500; }
	.wp-badge-line { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
	.wp-tr-pill { font-size: 11px; font-weight: 700; padding: 2px 8px; border-radius: 999px; background: rgb(255 255 255 / 16%); color: #fff; }
	.wp-tr-pill.local { background: rgb(34 197 94 / 26%); color: #86efac; }
	.wp-tr-pill.instant { background: rgb(56 189 248 / 26%); color: #7dd3fc; }
	.wp-tr-pill.torrent { background: rgb(245 158 11 / 26%); color: #fcd34d; }
	.wp-telemetry-pill { font-size: 11.5px; color: rgb(255 255 255 / 70%); }

	.wp-icon-btn {
		width: 38px; height: 38px; border-radius: 50%; border: 0; background: rgb(255 255 255 / 12%);
		color: #fff; display: grid; place-items: center; cursor: pointer;
	}
	.wp-icon-btn:hover { background: rgb(255 255 255 / 24%); }
	.wp-icon-btn.wp-step { font-size: 11px; font-weight: 800; }
	.wp-pill-btn {
		display: inline-flex; align-items: center; gap: 6px; height: 34px; padding: 0 12px;
		border-radius: 999px; border: 1px solid rgb(255 255 255 / 20%); background: rgb(20 20 26 / 74%);
		color: #fff; font: 600 12.5px/1 var(--font-ui); cursor: pointer;
	}
	.wp-pill-btn.on, .wp-pill-btn.accent { background: #e50914; border-color: #e50914; }

	.wp-skip-wrap {
		position: absolute; right: 26px; bottom: 56px; z-index: 10; display: flex; align-items: center; gap: 6px;
		transition: bottom 180ms ease;
	}
	.wp-skip-wrap.hud-up { bottom: 122px; }
	.wp-skip-btn {
		display: inline-flex; align-items: center; gap: 8px; padding: 10px 18px; border-radius: 8px;
		border: 1px solid rgb(255 255 255 / 45%); background: rgb(15 15 20 / 88%); color: #fff;
		font: 700 14px/1 var(--font-ui); cursor: pointer; backdrop-filter: blur(10px);
	}
	.wp-skip-btn small { color: #fcd34d; font-weight: 600; }
	.wp-skip-cancel {
		width: 32px; height: 32px; border-radius: 8px; border: 1px solid rgb(255 255 255 / 28%);
		background: rgb(15 15 20 / 80%); color: #fff; display: grid; place-items: center; cursor: pointer;
	}

	.wp-bottom-bar {
		bottom: 0; padding: 36px 22px 18px; display: flex; flex-direction: column; gap: 10px;
		background: linear-gradient(to top, rgb(0 0 0 / 90%), rgb(0 0 0 / 48%) 65%, transparent);
	}
	.wp-scrub-wrap { position: relative; height: 22px; display: flex; align-items: center; }
	.wp-hover-tip {
		position: absolute; bottom: 26px; transform: translateX(-50%); padding: 4px 9px; border-radius: 6px;
		background: rgb(15 15 20 / 92%); border: 1px solid rgb(255 255 255 / 22%); font-size: 11.5px; font-weight: 700;
		display: flex; flex-direction: column; align-items: center; gap: 2px; white-space: nowrap; pointer-events: none;
	}
	.wp-hover-tip small { font-weight: 500; color: rgb(255 255 255 / 76%); }
	.wp-scrub-track { position: relative; width: 100%; height: 6px; border-radius: 999px; background: rgb(255 255 255 / 22%); overflow: hidden; }
	.wp-scrub-buf { position: absolute; left: 0; top: 0; bottom: 0; background: rgb(255 255 255 / 32%); }
	.wp-seg-mark { position: absolute; top: 0; bottom: 0; background: rgb(250 204 21 / 52%); z-index: 1; }
	.wp-ch-tick { position: absolute; top: 0; bottom: 0; width: 2px; background: rgb(255 255 255 / 85%); z-index: 2; }
	.wp-scrub-fill { position: relative; z-index: 3; height: 100%; background: #e50914; }
	.wp-scrub-input { position: absolute; inset: 0; width: 100%; height: 100%; opacity: 0; cursor: pointer; z-index: 4; margin: 0; }

	.wp-ctrl-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
	.wp-vol-group { display: flex; align-items: center; gap: 6px; }
	.wp-vol-slider { width: 74px; accent-color: #e50914; cursor: pointer; }
	.wp-clock { display: flex; align-items: center; gap: 5px; font: 600 13px/1 var(--font-ui); }
	.wp-dim { color: rgb(255 255 255 / 60%); }
	.wp-clock-rem, .wp-clock-end { color: rgb(255 255 255 / 72%); font-size: 12px; }

	@media (max-width: 720px) {
		.wp-telemetry-pill, .wp-clock-rem, .wp-clock-end, .wp-vol-slider { display: none; }
	}
</style>
