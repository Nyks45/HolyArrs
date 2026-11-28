<!-- The Watchlist's header: a featured title's art behind the page name, the counts, and the title to pick up next. -->
<script lang="ts" module>
	import type { IconName } from '#lib/ui/Icon.svelte';
	export type HeroStat = { key: string; label: string; value: string; icon: IconName; tone?: 'ok' | 'info' | 'muted' };
	export type HeroFeature = { name: string; year?: number; poster?: string | null; href: string; note: string; pct?: number; backdrop?: string | null; play?: { href: string; label: string } | null };
</script>

<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import { sized } from '../core/tmdb-image.ts';
	import { posterSrc } from '#lib/ui/library-art.ts';
	import { t } from '#lib/core/i18n/i18n.svelte.ts';

	let { heading, backdrop = null, stats = [], feature = null, tabs }: { heading: string; backdrop?: string | null; stats?: HeroStat[]; feature?: HeroFeature | null; tabs?: Snippet } = $props();

	let artIn = $state(false);
	$effect(() => {
		void backdrop;
		artIn = false;
	});
</script>

<section class="wl-hero" data-testid="watchlist-hero">
	<div class="wl-hero-art" aria-hidden="true">
		{#if backdrop}<img src={sized(backdrop, 'backdrop', 1280)} alt="" class:in={artIn} onload={() => (artIn = true)} />{/if}
		<span class="wl-hero-glow"></span>
	</div>
	<div class="wl-hero-in">
		<div class="wl-hero-text">
			<p class="wl-eyebrow"><Icon name="sparkle" size={14} />{t('library.watch.eyebrow')}</p>
			<h1 class="wl-heading" dir="auto">{heading}</h1>
			{#if stats.length}
				<ul class="wl-stats" aria-label={t('library.watch.statsLabel')}>
					{#each stats as s (s.key)}
						<li class="wl-stat {s.tone ?? ''}" data-testid="watchlist-stat-{s.key}"><Icon name={s.icon} size={15} /><b>{s.value}</b><span>{s.label}</span></li>
					{/each}
				</ul>
			{/if}
		</div>
		{#if feature}
			<div class="wl-feature" data-testid="watchlist-feature">
				<a class="wl-feature-art" href={feature.href} tabindex="-1" aria-hidden="true">{#if feature.poster}<img src={posterSrc(feature.poster, 52)} alt="" loading="lazy" />{:else}<Icon name="film" size={20} />{/if}</a>
				<div class="wl-feature-text">
					<span class="wl-feature-note">{feature.note}</span>
					<a class="wl-feature-name" href={feature.href} dir="auto">{feature.name}{#if feature.year}<small>{feature.year}</small>{/if}</a>
					{#if feature.pct !== undefined}<span class="wl-feature-bar" aria-hidden="true"><i style:width="{Math.max(3, Math.min(100, feature.pct))}%"></i></span>{/if}
				</div>
				{#if feature.play}<a class="wl-feature-play" href={feature.play.href} data-testid="watchlist-feature-play"><Icon name="play" size={16} />{feature.play.label}</a>{/if}
			</div>
		{/if}
	</div>
	{#if tabs}<div class="wl-hero-tabs">{@render tabs()}</div>{/if}
</section>

<style>
	.wl-hero { position: relative; isolation: isolate; display: flex; flex-direction: column; gap: 22px; margin: calc(-1 * var(--s-6, 24px)) calc(-1 * var(--gutter, 0px)) 0; padding: clamp(36px, 6vh, 64px) var(--gutter, 16px) 8px; overflow: hidden; color: #fff; }
	.wl-hero-art { position: absolute; inset: 0; z-index: -1; background: radial-gradient(120% 90% at 85% 0%, rgb(255 69 58 / 16%), transparent 60%), radial-gradient(90% 80% at 0% 100%, rgb(64 156 255 / 10%), transparent 60%), #0b0b0c; }
	.wl-hero-art img { position: absolute; inset: -12px; width: calc(100% + 24px); height: calc(100% + 24px); object-fit: cover; object-position: center 30%; filter: blur(6px) saturate(1.15); opacity: 0; transition: opacity var(--dur-slow) var(--ease-out); }
	.wl-hero-art img.in { opacity: .5; }
	.wl-hero-art::after { content: ''; position: absolute; inset: 0; background: linear-gradient(180deg, rgb(11 11 12 / 30%) 0%, rgb(11 11 12 / 62%) 55%, #0b0b0c 100%), linear-gradient(90deg, rgb(11 11 12 / 78%) 0%, rgb(11 11 12 / 20%) 70%); }
	.wl-hero-glow { position: absolute; right: -10%; top: -40%; width: 60%; height: 120%; background: radial-gradient(closest-side, rgb(255 69 58 / 14%), transparent); pointer-events: none; }

	.wl-hero-in { display: flex; align-items: flex-end; justify-content: space-between; gap: 24px; min-width: 0; }
	.wl-hero-text { display: flex; flex-direction: column; gap: 10px; min-width: 0; }
	.wl-eyebrow { display: inline-flex; align-items: center; gap: 6px; margin: 0; font-size: 12px; font-weight: 700; letter-spacing: .12em; text-transform: uppercase; color: rgb(255 255 255 / 70%); }
	.wl-eyebrow :global(svg) { color: var(--accent); }
	.wl-heading { margin: 0; font: 800 clamp(32px, 4.2vw, 54px)/1.02 var(--font-display); letter-spacing: -.02em; text-shadow: 0 2px 24px rgb(0 0 0 / 45%); overflow-wrap: anywhere; }

	.wl-stats { display: flex; flex-wrap: wrap; gap: 8px; margin: 6px 0 0; padding: 0; list-style: none; }
	.wl-stat { display: inline-flex; align-items: center; gap: 7px; height: 34px; padding: 0 13px 0 11px; border-radius: 999px; background: rgb(255 255 255 / 9%); box-shadow: inset 0 0 0 1px rgb(255 255 255 / 12%); -webkit-backdrop-filter: blur(14px) saturate(160%); backdrop-filter: blur(14px) saturate(160%); font-size: 13px; color: rgb(255 255 255 / 75%); white-space: nowrap; }
	.wl-stat :global(svg) { color: rgb(255 255 255 / 70%); }
	.wl-stat b { font: 700 15px/1 var(--font-display); color: #fff; font-variant-numeric: tabular-nums; }
	.wl-stat.ok :global(svg) { color: var(--ok); }
	.wl-stat.info :global(svg) { color: var(--info); }
	.wl-stat.muted :global(svg) { color: rgb(255 255 255 / 50%); }

	.wl-feature { flex: 0 1 380px; display: flex; align-items: center; gap: 12px; min-width: 0; padding: 10px 12px 10px 10px; border-radius: 16px; background: rgb(22 22 26 / 58%); box-shadow: inset 0 0 0 1px rgb(255 255 255 / 10%), 0 18px 40px rgb(0 0 0 / 35%); -webkit-backdrop-filter: blur(22px) saturate(170%); backdrop-filter: blur(22px) saturate(170%); }
	.wl-feature-art { flex: none; display: grid; place-items: center; width: 52px; aspect-ratio: 2 / 3; border-radius: 8px; overflow: hidden; background: rgb(255 255 255 / 8%); color: rgb(255 255 255 / 60%); box-shadow: 0 6px 16px rgb(0 0 0 / 40%); }
	.wl-feature-art img { width: 100%; height: 100%; object-fit: cover; }
	.wl-feature-text { display: flex; flex-direction: column; gap: 4px; min-width: 0; flex: 1 1 auto; }
	.wl-feature-note { font-size: 11px; font-weight: 700; letter-spacing: .1em; text-transform: uppercase; color: var(--accent); }
	.wl-feature-name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: #fff; font: 700 15px/1.25 var(--font-display); text-decoration: none; }
	.wl-feature-name:hover { text-decoration: underline; }
	.wl-feature-name small { margin-left: 6px; font: 500 12px/1 var(--font-text, inherit); color: rgb(255 255 255 / 55%); }
	.wl-feature-bar { display: block; height: 4px; border-radius: 99px; background: rgb(255 255 255 / 16%); overflow: hidden; }
	.wl-feature-bar i { display: block; height: 100%; border-radius: inherit; background: var(--accent); }
	.wl-feature-play { flex: none; display: inline-flex; align-items: center; gap: 6px; height: 38px; padding: 0 16px 0 13px; border-radius: 999px; background: #fff; color: #111; font: 700 13.5px/1 var(--font-ui, inherit); text-decoration: none; box-shadow: 0 8px 20px rgb(0 0 0 / 30%); transition: transform var(--dur-fast) var(--ease-out), background var(--dur-fast) var(--ease-out); }
	.wl-feature-play:hover { background: rgb(255 255 255 / 88%); transform: translateY(-1px); }
	.wl-feature-play:focus-visible, .wl-feature-name:focus-visible { outline: none; box-shadow: var(--focus-ring); }

	.wl-hero-tabs { min-width: 0; }

	@media (max-width: 900px) {
		.wl-hero-in { flex-direction: column; align-items: stretch; }
		.wl-feature { flex-basis: auto; }
	}
	@media (max-width: 639px) {
		.wl-hero { gap: 16px; margin: calc(-1 * var(--s-4, 16px)) calc(-1 * var(--gutter, 16px)) 0; padding: 28px var(--gutter, 16px) 14px; }
		.wl-heading { font-size: 32px; }
		.wl-stats { gap: 6px; }
		.wl-stat { gap: 5px; height: 28px; padding: 0 10px 0 9px; font-size: 11.5px; }
		.wl-stat b { font-size: 13.5px; }
		.wl-feature-play { padding: 0 12px; }
	}
	@media (prefers-reduced-transparency: reduce) {
		.wl-stat, .wl-feature { backdrop-filter: none; -webkit-backdrop-filter: none; background: rgb(28 28 32 / 96%); }
	}
	@media (prefers-reduced-motion: reduce) {
		.wl-hero-art img, .wl-feature-play { transition: none; }
	}
</style>
