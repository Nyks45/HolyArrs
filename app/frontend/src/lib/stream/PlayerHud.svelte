<script lang="ts">
	import SeekPreviewCard from './SeekPreviewCard.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import PlayerIcons from './PlayerIcons.svelte';
	import { formatDuration, formatSpeed, type ChapterMark, type SessionStatus, type StreamCandidate } from './stream-api.ts';
	import type { HlsQualityLevel } from './player-media.ts';
	import type { SkipSegment } from './streaming-config-api.ts';

	let {
		hudVisible, immersive, captionsOn, onCaptions, onMore, onImmersive, episodeLabel = '', episodeName = '',
		drawer,
		kind,
		headerTitle,
		headerSub,
		previewSource = '',
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
		pipAvailable = false,
		isPiP = false,
		volume,
		muted,
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
		skipCountdown = 0, nextCountdown = 0,
		hasNextEpisode = false,
		hlsLevels = [],
		hlsLevelIdx = -1,
		browserQuality = 'original',
		onLeave,
		onToggleDrawer,
		onTogglePlayPause,
		onSeekBy,
		onScrubInput,
		onScrubCommit,
		onToggleMute,
		onVolumeChange,
		onTogglePiP,
		onNextEpisode,
		onToggleFullscreen,
		onExecuteSkip,
		onCancelAutoSkip
	}: {
		hudVisible: boolean; immersive: boolean; captionsOn: boolean; onCaptions: () => void; onMore: () => void; onImmersive: () => void; episodeLabel?: string; episodeName?: string;
		drawer: 'none' | 'sources' | 'quality' | 'audioSubs' | 'audio' | 'episodes';
		kind: 'movie' | 'show';
		headerTitle: string;
		headerSub: string;
		previewSource?: string;
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
		pipAvailable?: boolean;
		isPiP?: boolean;
		volume: number;
		muted: boolean;
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
		skipCountdown?: number; nextCountdown?: number;
		hasNextEpisode?: boolean;
		hlsLevels?: HlsQualityLevel[];
		hlsLevelIdx?: number;
		browserQuality?: 'original' | '1080p' | '720p' | '480p';
		onLeave: () => void;
		onToggleDrawer: (d: 'sources' | 'quality' | 'audioSubs' | 'audio' | 'episodes') => void;
		onTogglePlayPause: () => void;
		onSeekBy: (delta: number) => void;
		onScrubInput: (sec: number) => void;
		onScrubCommit: (sec: number) => void;
		onToggleMute: () => void;
		onVolumeChange: (v: number) => void;
		onTogglePiP: () => void;
		onNextEpisode: () => void;
		onToggleFullscreen: () => void;
		onExecuteSkip: (seg: SkipSegment) => void;
		onCancelAutoSkip: () => void;
	} = $props();

	let hoverSec = $state<number | null>(null);
	let hoverPct = $state(0);

	const hoverChapter = $derived.by(() => {
		if (hoverSec === null) return '';
		const ch = chapters.find((c) => hoverSec! >= c.start && hoverSec! <= c.end);
		return ch?.title || skipSegments.find((seg) => hoverSec! >= seg.start && hoverSec! <= seg.end)?.label || '';
	});

	// The track splits into segments at chapter and skip boundaries, with a small gap between them.
	const segments = $derived.by(() => {
		if (totalDuration <= 0) return [{ start: 0, end: 1 }];
		const cuts = new Set<number>([0, totalDuration]);
		for (const ch of chapters) if (ch.start > 1 && ch.start < totalDuration - 1) cuts.add(ch.start);
		for (const seg of skipSegments) for (const at of [seg.start, seg.end]) if (at > 1 && at < totalDuration - 1) cuts.add(at);
		const sorted = [...cuts].sort((x, y) => x - y);
		return sorted.slice(1).map((end, i) => ({ start: sorted[i], end })).filter((seg) => seg.end - seg.start >= totalDuration * 0.004);
	});
	const fillOf = (seg: { start: number; end: number }, pct: number) => {
		if (totalDuration <= 0) return 0;
		const at = (pct / 100) * totalDuration;
		return Math.min(100, Math.max(0, ((at - seg.start) / (seg.end - seg.start)) * 100));
	};
	const clock = (sec: number) => {
		const t = Math.max(0, Math.floor(sec || 0)), h = Math.floor(t / 3600), m = Math.floor((t % 3600) / 60), x = t % 60;
		return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(x).padStart(2, '0')}` : `${String(m).padStart(2, '0')}:${String(x).padStart(2, '0')}`;
	};
	const shown = $derived(hudVisible || paused || drawer !== 'none');

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

<!-- Top: back arrow, and the title stack in immersive mode. -->
<header class="wp-top-bar" class:visible={shown}>
	<button type="button" class="wp-ico wp-back" aria-label="Back" title="Back" onclick={onLeave}><PlayerIcons name="back" size={26} /></button>
</header>
{#if immersive && shown}
	<div class="wp-now"><small>You're Watching</small><strong>{headerTitle}</strong>{#if kind === 'show' && episodeLabel}<span>{episodeLabel.replace('EP', 'Episode').replace(/ \/ \d+$/, '')}</span>{/if}</div>
{/if}

{#if activeSkip && pipelineMode !== 'none'}
	<div class="wp-skip-wrap" class:hud-up={shown}>
		<button type="button" class="wp-skip-btn" onclick={() => onExecuteSkip(activeSkip!)}>
			<span>{activeSkip.label || 'Skip'}</span>
			{#if skipCountdown > 0}<small>in {skipCountdown}s</small>{/if}
		</button>
		{#if skipCountdown > 0}
			<button type="button" class="wp-skip-cancel" title="Cancel auto-skip" onclick={onCancelAutoSkip}><Icon name="x" size={14} /></button>
		{/if}
	</div>
{/if}

<footer class="wp-bottom-bar" class:visible={shown && pipelineMode !== 'none'} class:hidden={pipelineMode === 'none'}>
	<div class="wp-clock-line">
		<span class="wp-time">{clock(currentPos)}</span>
		{#if kind === 'show' && episodeLabel}<span class="wp-ep-pill">{episodeLabel}</span>{/if}
		{#if nextCountdown > 0}<button type="button" class="wp-next-soon" onclick={onNextEpisode}>Next episode in {nextCountdown}s</button>{/if}
		<span class="wp-time wp-total">{clock(totalDuration)}</span>
	</div>

	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="wp-scrub-wrap" onmousemove={onTrackMove} onmouseleave={() => { hoverSec = null; }}>
		{#if hoverSec !== null}<SeekPreviewCard source={previewSource} seconds={hoverSec} chapter={hoverChapter} percent={hoverPct} />{/if}
		<div class="wp-scrub-track">
			{#each segments as seg (seg.start)}
				<div class="wp-seg" style:flex-grow={seg.end - seg.start}>
					<div class="wp-seg-buf" style:width="{fillOf(seg, bufferedPct)}%"></div>
					<div class="wp-seg-fill" style:width="{fillOf(seg, scrubbedPct)}%"></div>
				</div>
			{/each}
		</div>
		<div class="wp-scrub-knob" style:left="{scrubbedPct}%"></div>
		<input type="range" class="wp-scrub-input" min="0" max={totalDuration || 100} step="0.5" value={currentPos} aria-label="Playback position"
			oninput={(e) => onScrubInput(Number((e.currentTarget as HTMLInputElement).value))}
			onchange={(e) => onScrubCommit(Number((e.currentTarget as HTMLInputElement).value))} />
	</div>

	<div class="wp-ctrl-row">
		<div class="wp-ctrl-left">
			<div class="wp-transport">
				<button type="button" class="wp-ico wp-sm" aria-label="Back 10 seconds" title="Back 10s" onclick={() => onSeekBy(-10)}><PlayerIcons name="replay10" size={24} /></button>
				<button type="button" class="wp-ico wp-play" aria-label={paused ? 'Play' : 'Pause'} title={paused ? 'Play' : 'Pause'} onclick={onTogglePlayPause}><PlayerIcons name={paused ? 'play' : 'pause'} size={34} /></button>
				<button type="button" class="wp-ico wp-sm" aria-label="Forward 10 seconds" title="Forward 10s" onclick={() => onSeekBy(10)}><PlayerIcons name="forward10" size={24} /></button>
			</div>
			{#if kind === 'show' && hasNextEpisode}
				<button type="button" class="wp-ico wp-next" aria-label="Next episode" title="Next episode" onclick={onNextEpisode}><PlayerIcons name="next" size={30} /></button>
			{/if}
			<div class="wp-vol-group">
				<button type="button" class="wp-ico" aria-label={muted ? 'Unmute' : 'Mute'} title={muted ? 'Unmute' : 'Mute'} onclick={onToggleMute}><PlayerIcons name={muted || volume === 0 ? 'mute' : volume < 0.5 ? 'volumeLow' : 'volume'} size={30} /></button>
				<input type="range" class="wp-vol-slider" min="0" max="1" step="0.05" value={muted ? 0 : volume} aria-label="Volume" oninput={(e) => onVolumeChange(Number((e.currentTarget as HTMLInputElement).value))} />
			</div>
			<div class="wp-title-stack"><small>{headerTitle}</small><strong>{kind === 'show' ? episodeName || headerSub : headerSub || (session?.release_title ?? '')}</strong></div>
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
			<button type="button" class="wp-ico" aria-label="Captions" title="Subtitles (C)" aria-pressed={captionsOn} class:on={captionsOn} onclick={onCaptions}><PlayerIcons name="captions" size={30} /></button>
			<button type="button" class="wp-ico" aria-label="Audio" title="Audio" class:on={drawer === 'audio'} onclick={() => onToggleDrawer('audio')}><PlayerIcons name="audio" size={30} /></button>
			<button type="button" class="wp-ico" aria-label="Settings" title="Settings" class:on={drawer === 'quality'} onclick={() => onToggleDrawer('quality')}><PlayerIcons name="settings" size={30} /></button>
			<button type="button" class="wp-ico" aria-label="More options" title="More options" onclick={onMore}><PlayerIcons name="more" size={30} /></button>
			<button type="button" class="wp-ico wp-wide" aria-label="Immersive mode" title="Immersive mode (T)" aria-pressed={immersive} class:on={immersive} onclick={onImmersive}><PlayerIcons name="theater" size={30} /></button>
			<button type="button" class="wp-ico" aria-label={isFullscreen ? 'Exit fullscreen' : 'Fullscreen'} title={isFullscreen ? 'Exit fullscreen (F)' : 'Fullscreen (F)'} onclick={onToggleFullscreen}><PlayerIcons name={isFullscreen ? 'exitFullscreen' : 'fullscreen'} size={30} /></button>
		</div>
	</div>
</footer>

<style>
	.wp-status-pill {
		position: absolute; top: 18px; left: 50%; transform: translateX(-50%); z-index: 15;
		display: inline-flex; align-items: center; gap: 8px; padding: 7px 14px; border-radius: 999px;
		background: rgb(20 20 24 / 88%); border: 1px solid rgb(255 255 255 / 10%);
		backdrop-filter: blur(16px); font-size: 12px; font-weight: 600; pointer-events: none;
	}
	.wp-dot-spin { width: 12px; height: 12px; border-radius: 50%; border: 2px solid rgb(255 255 255 / 22%); border-top-color: #fff; animation: wp-spin 0.7s linear infinite; }
	@keyframes wp-spin { to { transform: rotate(360deg); } }

	.wp-top-bar, .wp-bottom-bar { position: absolute; left: 0; right: 0; z-index: 9; opacity: 0; pointer-events: none; transition: opacity 200ms ease; }
	.wp-top-bar.visible, .wp-bottom-bar.visible { opacity: 1; pointer-events: auto; }
	.wp-bottom-bar.hidden { opacity: 0 !important; pointer-events: none !important; }
	.wp-top-bar { top: 0; padding: 18px 18px 40px; background: linear-gradient(to bottom, rgb(0 0 0 / 55%), transparent); }

	.wp-now { position: absolute; z-index: 8; left: 6.5%; top: 36%; display: flex; flex-direction: column; gap: 6px; max-width: 60%; pointer-events: none; text-shadow: 0 2px 18px rgb(0 0 0 / 55%); }
	.wp-now small { font-size: 17px; color: rgb(255 255 255 / 70%); }
	.wp-now strong { font-size: clamp(22px, 2.6vw, 34px); font-weight: 800; line-height: 1.15; }
	.wp-now span { font-size: 18px; font-weight: 700; color: rgb(255 255 255 / 88%); }

	.wp-ico { min-width: 44px; height: 44px; padding: 0 4px; border-radius: 999px; border: 0; background: transparent; color: #fff; display: grid; place-items: center; cursor: pointer; transition: background 140ms ease, opacity 140ms ease; }
	.wp-ico:hover, .wp-ico.on { background: rgb(255 255 255 / 12%); }
	.wp-ico:focus-visible, .wp-ep-toggle:focus-visible { outline: 2px solid #fff; outline-offset: 2px; }
	.wp-back { min-width: 40px; height: 40px; }
	.wp-transport { display: flex; align-items: center; }
	.wp-transport .wp-sm { min-width: 36px; height: 36px; }
	.wp-play { min-width: 48px; height: 48px; }

	.wp-skip-wrap { position: absolute; right: 26px; bottom: 54px; z-index: 10; display: flex; align-items: center; gap: 6px; transition: bottom 180ms ease; }
	.wp-skip-wrap.hud-up { bottom: 150px; }
	.wp-skip-btn { display: inline-flex; align-items: center; gap: 8px; padding: 10px 18px; border-radius: 10px; border: 1px solid rgb(255 255 255 / 30%); background: rgb(20 20 24 / 90%); color: #fff; font: 700 13.5px/1 var(--font-ui); cursor: pointer; backdrop-filter: blur(12px); }
	.wp-skip-btn small { color: rgb(255 255 255 / 60%); font-weight: 600; }
	.wp-skip-cancel { width: 32px; height: 32px; border-radius: 10px; border: 1px solid rgb(255 255 255 / 20%); background: rgb(20 20 24 / 88%); color: #fff; display: grid; place-items: center; cursor: pointer; }

	.wp-bottom-bar { bottom: 0; padding: 48px 12px 10px; display: flex; flex-direction: column; gap: 4px; background: linear-gradient(to top, rgb(0 0 0 / 90%), rgb(0 0 0 / 55%) 55%, transparent); }
	.wp-clock-line { display: flex; align-items: center; gap: 12px; padding: 0 1px; font-size: 12.5px; font-weight: 600; }
	.wp-time { font-variant-numeric: tabular-nums; color: #fff; }
	.wp-total { margin-left: auto; color: rgb(255 255 255 / 85%); }
	.wp-ep-pill { padding: 3px 8px; border-radius: 4px; background: rgb(255 255 255 / 12%); color: rgb(255 255 255 / 82%); font-size: 11px; font-weight: 700; letter-spacing: .06em; }
	.wp-next-soon { border: 1px solid rgb(255 255 255 / 22%); border-radius: 6px; padding: 3px 8px; background: rgb(20 20 24 / 80%); color: #fff; font: 600 11.5px/1.2 var(--font-ui); cursor: pointer; }

	.wp-scrub-wrap { position: relative; height: 22px; display: flex; align-items: center; }
	.wp-scrub-track { position: relative; width: 100%; height: 4px; display: flex; gap: 3px; transition: height 120ms ease; }
	.wp-scrub-wrap:hover .wp-scrub-track { height: 6px; }
	.wp-seg { position: relative; flex-basis: 0; min-width: 2px; height: 100%; border-radius: 999px; background: rgb(255 255 255 / 22%); overflow: hidden; }
	.wp-seg-buf { position: absolute; inset: 0 auto 0 0; background: rgb(255 255 255 / 38%); }
	.wp-seg-fill { position: absolute; inset: 0 auto 0 0; background: #fff; }
	.wp-scrub-knob { position: absolute; top: 50%; width: 12px; height: 12px; border-radius: 50%; background: #fff; transform: translate(-50%, -50%) scale(0); pointer-events: none; z-index: 4; box-shadow: 0 0 8px rgb(0 0 0 / 60%); transition: transform 120ms ease; }
	.wp-scrub-wrap:hover .wp-scrub-knob { transform: translate(-50%, -50%) scale(1); }
	.wp-scrub-input { position: absolute; inset: 0; width: 100%; height: 100%; opacity: 0; cursor: pointer; z-index: 5; margin: 0; }

	.wp-ctrl-row { display: grid; grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr); align-items: center; gap: 10px; padding-top: 6px; }
	.wp-ctrl-left { display: flex; align-items: center; gap: 14px; min-width: 0; }
	.wp-ctrl-center { display: flex; justify-content: center; }
	.wp-ctrl-right { display: flex; align-items: center; justify-content: flex-end; gap: 14px; min-width: 0; }
	.wp-vol-group { display: flex; align-items: center; gap: 4px; }
	.wp-vol-slider { width: 0; opacity: 0; accent-color: #fff; cursor: pointer; transition: width 160ms ease, opacity 160ms ease; }
	.wp-vol-group:hover .wp-vol-slider, .wp-vol-slider:focus-visible { width: 80px; opacity: 1; }
	.wp-title-stack { display: flex; flex-direction: column; gap: 3px; min-width: 0; margin-left: 10px; }
	.wp-title-stack small, .wp-title-stack strong { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
	.wp-title-stack small { font-size: 12.5px; color: rgb(255 255 255 / 62%); }
	.wp-title-stack strong { font-size: 14px; font-weight: 700; }
	.wp-ep-toggle { display: inline-flex; align-items: center; gap: 6px; padding: 9px 16px; border-radius: 999px; border: 0; background: transparent; color: #fff; font: 700 14px/1 var(--font-ui); cursor: pointer; }
	.wp-ep-toggle:hover, .wp-ep-toggle.on { background: rgb(255 255 255 / 12%); }

	@media (max-width: 1100px) { .wp-title-stack { display: none; } .wp-ctrl-left, .wp-ctrl-right { gap: 6px; } }
	@media (max-width: 720px) {
		.wp-vol-slider, .wp-wide { display: none !important; }
		.wp-bottom-bar { padding-inline: 8px; }
		.wp-ctrl-row { gap: 2px; } .wp-ctrl-left, .wp-ctrl-right { gap: 0; }
		.wp-ctrl-row .wp-ico { min-width: clamp(22px, calc((100vw - 150px) / 8), 40px); height: 40px; padding: 0; }
		.wp-ctrl-row .wp-ico :global(svg) { width: 22px; height: 22px; }
		.wp-transport .wp-sm { display: none; }
		.wp-now { display: none; }
	}
</style>
