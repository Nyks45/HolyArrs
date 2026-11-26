<script lang="ts">
	import Icon from '#lib/ui/Icon.svelte';
	import type { SessionStatus, StreamCandidate, WatchEntry } from './stream-api.ts';

	let {
		loadingPage,
		pageError,
		playMode,
		session,
		loadingMedia,
		loadingMoreSources = false,
		autoPlayOn = true,
		sourcesOpen = false,
		headerTitle,
		headerSub,
		overview = '',
		activeSaved,
		bestCandidate,
		candidatesCount,
		starting,
		searching,
		playerError,
		onRetryLoad,
		onSwitchToBrowser,
		onResumeSaved,
		onPlayBest,
		onOpenSources
	}: {
		loadingPage: boolean;
		pageError: string;
		playMode: 'browser' | 'mpv';
		session: SessionStatus | null;
		loadingMedia: boolean;
		loadingMoreSources?: boolean;
		autoPlayOn?: boolean;
		sourcesOpen?: boolean;
		headerTitle: string;
		headerSub: string;
		overview?: string;
		activeSaved: WatchEntry | null;
		bestCandidate: StreamCandidate | null;
		candidatesCount: number;
		starting: boolean;
		searching: boolean;
		playerError: string;
		onRetryLoad: () => void;
		onSwitchToBrowser: () => void;
		onResumeSaved: (pos: number) => void;
		onPlayBest: () => void;
		onOpenSources: () => void;
	} = $props();

	const pendingAutoPlay = $derived(autoPlayOn && loadingMoreSources && !session && !playerError);
	const activeSessionLoading = $derived(!!session && playMode === 'browser' && session.state !== 'error');
	const isBusy = $derived(loadingPage || searching || starting || loadingMedia || activeSessionLoading || pendingAutoPlay);
	const loaderText = $derived.by(() => {
		if (loadingPage && !searching) return 'Loading title...';
		if (searching || pendingAutoPlay) return 'Finding best source...';
		if (starting || session?.state === 'resolving') return 'Connecting to stream...';
		return 'Buffering playback...';
	});
</script>

<div class="wp-center-panel">
	{#if loadingPage}
		<div class="wp-loader"><div class="wp-spin"></div><div class="wp-load-title">{loaderText}</div></div>
	{:else if pageError}
		<div class="wp-card">
			<h2>Could not load player</h2>
			<p class="wp-muted">{pageError}</p>
			<button type="button" class="wp-cta wp-cta-white" onclick={onRetryLoad}>Retry</button>
		</div>
	{:else if playMode === 'mpv' && session}
		<div class="wp-card">
			<h2>Playing in MPV</h2>
			<p class="wp-muted">{session.release_title}</p>
			<button type="button" class="wp-cta wp-cta-glass" onclick={onSwitchToBrowser}>Play in Browser Instead</button>
		</div>
	{:else if sourcesOpen}
		<!-- Keep center clean while user picks from the Sources drawer -->
	{:else if isBusy}
		<div class="wp-loader">
			<div class="wp-spin"></div>
			<div class="wp-load-title">{loaderText}</div>
		</div>
	{:else}
		<div class="wp-card wp-landing">
			<h1>{headerTitle}</h1>
			{#if headerSub}<p class="wp-ep-tag">{headerSub}</p>{/if}
			{#if overview}<p class="wp-overview">{overview}</p>{/if}
			<div class="wp-card-acts">
				{#if activeSaved && !activeSaved.completed && activeSaved.position_sec > 15 && bestCandidate}
					<button type="button" class="wp-cta wp-cta-white" disabled={starting} onclick={() => onResumeSaved(activeSaved!.position_sec)}>
						<Icon name="play" size={18} />
						<span>Resume ({Math.round(activeSaved.progress_pct ?? 0)}%)</span>
					</button>
				{/if}
				<button
					type="button"
					class="wp-cta {activeSaved && !activeSaved.completed ? 'wp-cta-glass' : 'wp-cta-white'}"
					disabled={starting || searching}
					onclick={() => (bestCandidate ? onPlayBest() : onOpenSources())}
				>
					<Icon name="play" size={18} />
					<span>{searching ? 'Finding Streams...' : bestCandidate ? 'Play Best Source' : 'Choose Source'}</span>
				</button>
				<button type="button" class="wp-cta wp-cta-glass" onclick={onOpenSources}>Sources ({candidatesCount})</button>
			</div>
			{#if playerError || session?.error}<div class="wp-err-inline">{playerError || session?.error}</div>{/if}
		</div>
	{/if}
</div>

<style>
	.wp-center-panel { position: absolute; inset: 0; z-index: 5; display: grid; place-items: center; padding: 24px; pointer-events: none; }
	.wp-loader, .wp-card { pointer-events: auto; }
	.wp-loader { display: flex; flex-direction: column; align-items: center; gap: 12px; text-align: center; }
	.wp-load-title { font: 600 17px/1.3 var(--font-ui); }
	.wp-spin { width: 42px; height: 42px; border-radius: 50%; border: 3px solid rgb(255 255 255 / 20%); border-top-color: #e50914; animation: wp-spin 0.8s linear infinite; }
	@keyframes wp-spin { to { transform: rotate(360deg); } }
	.wp-card { width: min(680px, 100%); padding: 28px 32px; border-radius: 20px; background: rgb(16 16 20 / 78%); border: 1px solid rgb(255 255 255 / 14%); backdrop-filter: blur(20px); display: flex; flex-direction: column; gap: 14px; }
	.wp-landing h1 { margin: 0; font: 700 clamp(24px, 3.4vw, 38px)/1.12 var(--font-ui); }
	.wp-ep-tag, .wp-overview, .wp-muted { margin: 0; color: rgb(255 255 255 / 76%); font-size: 14px; }
	.wp-card-acts { display: flex; flex-wrap: wrap; gap: 10px; }
	.wp-cta { display: inline-flex; align-items: center; gap: 8px; height: 44px; padding: 0 20px; border: 0; border-radius: 999px; font: 700 14px/1 var(--font-ui); cursor: pointer; }
	.wp-cta-white { background: #fff; color: #111; }
	.wp-cta-glass { background: rgb(255 255 255 / 16%); color: #fff; border: 1px solid rgb(255 255 255 / 22%); }
	.wp-err-inline { padding: 8px 10px; border-radius: 8px; background: rgb(239 68 68 / 20%); color: #fca5a5; font-size: 12.5px; }
</style>
