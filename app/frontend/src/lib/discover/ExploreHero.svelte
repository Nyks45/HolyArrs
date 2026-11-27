<script lang="ts">
	import { t } from '#lib/core/i18n/i18n.svelte.ts';
	import { serverWords } from '#lib/core/i18n/server-words.ts';
	import { richMetadata, type RichMeta } from '#lib/ui/TitleHero.svelte';
	import Billboard from '#lib/ui/Billboard.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import { shortSynopsis } from './synopsis.ts';
	import type { Snippet } from 'svelte';
	import { api } from '../core/api.ts';
	import { to } from '../core/paths.ts';
	import { heroPictures } from '../core/trailer/trailer.ts';
	import { proxied, sized } from '../core/tmdb-image.ts';

	// The Explore hero: one featured title in the shared billboard. Watch Now plays right away; More info opens the title's page.
	let {
		kind,
		tmdb,
		title,
		href,
		backdrop,
		poster,
		subtitle,
		settled = true,
		vtName,
		onPlayHover,
		slideIndex = 0,
		slideCount = 0,
		onPrev,
		onNext,
		onSelectSlide,
		badge,
		action
	}: {
		kind: 'movie' | 'show';
		tmdb: string;
		title: string;
		href?: string;
		/** A backdrop known already, shown at once. */
		backdrop?: string | null;
		/** The card's poster, shown at once in the minimal look. */
		poster?: string | null;
		subtitle?: string;
		/** The page has finished its first load. */
		settled?: boolean;
		/** The art's view-transition name, so it grows into the title page's hero. */
		vtName?: string;
		onPlayHover?: (kind: 'movie' | 'show', tmdb: string, title: string, year?: number) => void;
		slideIndex?: number;
		slideCount?: number;
		onPrev?: () => void;
		onNext?: () => void;
		onSelectSlide?: (idx: number) => void;
		badge?: Snippet;
		action?: Snippet;
	} = $props();

	let rich = $state<RichMeta | null>(null);
	$effect(() => {
		const key = `${kind}:${tmdb}`;
		let live = true;
		rich = null;
		void richMetadata(api.get, kind, tmdb).then((m) => live && key === `${kind}:${tmdb}` && (rich = m));
		return () => {
			live = false;
		};
	});
	const pictures = $derived(heroPictures(rich?.backdrop ?? backdrop, rich?.backdrops).map((u) => sized(u, 'backdrop')));
	const line = $derived([rich?.year, ...(rich?.genres ?? []).slice(0, 3).map((g) => serverWords(g))].filter(Boolean).join(' · ') || subtitle || '');
	const watchHref = $derived(to(`/watch/${kind}/${encodeURIComponent(tmdb)}?from=/explore`));
</script>

<div class="hero-stage">
	<Billboard label={t('discover.exploreHero.featured', { title })} testid="explore-hero" {kind} {tmdb} {title} videos={rich?.videos} {pictures} {vtName} {settled} thumb={sized(poster ?? rich?.poster, 'poster', 148) || null}>
		{#snippet heading()}
			{#if rich?.logo}
				<h2 class="bb-logo"><img src={proxied(rich.logo)} alt={title} /></h2>
			{:else}
				<h2 class="bb-name">{title}</h2>
			{/if}
		{/snippet}
		{#snippet meta()}
			{#if line}<p>{line}</p>{/if}
			{#if rich?.score}<span class="hero-pill score">★ {rich.score.toFixed(1)}</span>{/if}
			{#if rich?.certification}<span class="hero-pill cert">{rich.certification}</span>{/if}
			{#if badge}{@render badge()}{/if}
		{/snippet}
		{#snippet fold()}
			<!-- The title box and overview keep their space while the details load. -->
			<div class="about">
				{#if rich?.overview}<p class="bb-desc" title={rich.overview} dir="auto">{shortSynopsis(rich.overview, 220)}</p>{/if}
			</div>
		{/snippet}
		{#snippet actions()}
			<a
				class="watch"
				href={watchHref}
				onmouseenter={() => onPlayHover?.(kind, tmdb, title, rich?.year)}
				onfocus={(e) => {
					onPlayHover?.(kind, tmdb, title, rich?.year);
					e.currentTarget.scrollIntoView({ block: 'nearest', behavior: 'instant' });
				}}
			>
				<Icon name="play" size={16} />
				<span>Watch Now</span>
			</a>
			{#if href}<a class="info" {href} onfocus={(e) => e.currentTarget.scrollIntoView({ block: 'nearest', behavior: 'instant' })}>{t('discover.exploreHero.moreInfo')}</a>{/if}
			{#if action}{@render action()}{/if}
		{/snippet}
	</Billboard>
	{#if slideCount > 1}
		<div class="hero-pager" role="group" aria-label="Featured carousel controls">
			<button type="button" class="pager-btn" aria-label="Previous featured title" onclick={() => onPrev?.()}>
				<Icon name="chevronLeft" size={15} />
			</button>
			<div class="pager-dots">
				{#each Array.from({ length: slideCount }, (_, idx) => idx) as idx (idx)}
					<button
						type="button"
						class="pager-dot"
						class:active={idx === slideIndex}
						aria-label={`Show featured title ${idx + 1}`}
						aria-current={idx === slideIndex ? 'true' : undefined}
						onclick={() => onSelectSlide?.(idx)}
					></button>
				{/each}
			</div>
			<button type="button" class="pager-btn" aria-label="Next featured title" onclick={() => onNext?.()}>
				<Icon name="chevronRight" size={15} />
			</button>
		</div>
	{/if}
</div>

<style>
	.hero-stage { position: relative; }
	.about { min-height: calc(16px * 1.45 * 3); }
	.hero-pill {
		display: inline-flex; align-items: center; padding: 2px 8px; border-radius: 6px;
		font: 600 12px/1.35 var(--font-ui); letter-spacing: 0.01em;
		border: 1px solid rgb(255 255 255 / 26%); background: rgb(0 0 0 / 32%); color: #fff;
		backdrop-filter: blur(8px); font-variant-numeric: tabular-nums;
	}
	.hero-pill.score { color: #ffd666; border-color: rgb(255 214 102 / 35%); }
	.watch {
		display: inline-flex; align-items: center; gap: 8px; min-height: var(--hero-btn, 44px);
		padding: 0 clamp(20px, 1.5vw, 32px); border-radius: var(--r-pill);
		background: #fff; color: #111; font: 700 clamp(15px, 1vw, 20px)/1 var(--font-ui);
		text-decoration: none; box-shadow: 0 8px 24px rgb(0 0 0 / 32%);
		transition: background var(--dur-fast) var(--ease-out), transform var(--dur-instant) var(--ease-out);
	}
	.watch:hover { background: #e9e9ec; }
	.watch:active { transform: scale(.97); }
	.watch:focus-visible { outline: none; box-shadow: 0 0 0 2px #fff, 0 0 0 4px rgb(255 255 255 / 45%); }
	.info { display: inline-flex; align-items: center; min-height: var(--hero-btn, 44px); padding: 0 clamp(20px, 1.4vw, 32px); font-size: clamp(16px, 1vw, 22px) !important; border-radius: var(--r-pill); background: rgb(255 255 255 / 16%); border: 1px solid rgb(255 255 255 / 22%); color: #fff; font: var(--t-headline); -webkit-backdrop-filter: blur(18px) saturate(160%); backdrop-filter: blur(18px) saturate(160%); transition: background var(--dur-fast) var(--ease-out), transform var(--dur-instant) var(--ease-out); }
	.info:hover { background: rgb(255 255 255 / 26%); }
	.info:active { transform: scale(.97); }
	.info:focus-visible { outline: none; box-shadow: 0 0 0 2px #fff; }
	.hero-pager {
		position: absolute; right: clamp(16px, 2.6vw, 36px); bottom: clamp(16px, 2.4vw, 32px); z-index: 4;
		display: inline-flex; align-items: center; gap: 6px; padding: 5px 8px;
		border-radius: 999px; background: rgb(14 14 18 / 58%); border: 1px solid rgb(255 255 255 / 16%);
		backdrop-filter: blur(14px);
	}
	.pager-btn {
		width: 26px; height: 26px; border: 0; border-radius: 999px; padding: 0;
		display: inline-grid; place-items: center; background: transparent; color: rgb(255 255 255 / 86%); cursor: pointer;
		transition: background var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out);
	}
	.pager-btn:hover { background: rgb(255 255 255 / 16%); color: #fff; }
	.pager-dots { display: inline-flex; align-items: center; gap: 5px; padding: 0 2px; }
	.pager-dot {
		width: 7px; height: 7px; border-radius: 999px; border: 0; padding: 0; cursor: pointer;
		background: rgb(255 255 255 / 35%); transition: width var(--dur-fast) var(--ease-out), background var(--dur-fast) var(--ease-out);
	}
	.pager-dot.active { width: 18px; background: #fff; }
	:global(.ui-billboard.minimal) .watch, :global(.ui-billboard.minimal) .info { font-size: 15px !important; }
	@media (max-width: 767px) {
		:global(html .ui-billboard.minimal[data-testid='explore-hero']) { padding-top: 48px; }
		.hero-pager { display: none; }
	}
</style>
