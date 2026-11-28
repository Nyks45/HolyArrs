<!-- Title page layout: poster and facts on the left, title, actions, synopsis and tabs on the right, over the title's own art. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { proxied, sized } from '#lib/core/tmdb-image.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import { claimPlayer, pickTrailer, type Video } from '../core/trailer/trailer.ts';
	import { missReason, trailerSources, videoReason, type TrailerSource } from '../core/trailer/autoplay.svelte.ts';
	import { openTheater } from '../core/trailer/theater.svelte.ts';

	type Row = { label: string; value: string };
	type Tab = 'episodes' | 'relations' | 'recommendations';
	let {
		kind, tmdb, name, originalName = '', poster = '', backdrop = '', logo = '', genres = [], overview = '',
		facts = [], themes = [], studios = [], languages = [], videos = [], nextAir = null, pill = '',
		actions, scores, byline, library, episodes, relations, recommendations, showTabs = true
	}: {
		kind: 'movie' | 'show'; tmdb: string; name: string; originalName?: string | null; poster?: string | null; backdrop?: string | null; logo?: string | null;
		genres?: string[]; overview?: string; facts?: Row[]; themes?: string[]; studios?: string[]; languages?: string[];
		videos?: Video[]; nextAir?: { season: number; episode: number; airDate: string } | null; pill?: string;
		actions?: Snippet; scores?: Snippet; byline?: Snippet; library?: Snippet;
		episodes?: Snippet; relations?: Snippet; recommendations?: Snippet; showTabs?: boolean;
	} = $props();

	const tabs = $derived(([['episodes', 'Episodes', episodes], ['relations', 'Relations', relations], ['recommendations', 'Recommendations', recommendations]] as const).filter(([, , s]) => !!s));
	let picked = $state<Tab | null>(null);
	const tab = $derived(picked && tabs.some(([v]) => v === picked) ? picked : (tabs[0]?.[0] ?? 'relations'));
	let longStory = $state(false);
	let now = $state(Date.now());
	const trailer = $derived(pickTrailer(videos));

	// Counts down to the next episode's air day (local midnight, as TMDB lists days without a time).
	const airAt = $derived.by(() => {
		if (!nextAir?.airDate) return 0;
		const [y, m, d] = nextAir.airDate.slice(0, 10).split('-').map(Number);
		return y && m && d ? new Date(y, m - 1, d).getTime() : 0;
	});
	const countdown = $derived.by(() => {
		if (!airAt || now >= airAt + 86_400_000) return '';
		const left = Math.max(0, Math.floor((airAt - now) / 1000));
		if (left === 0) return 'Out today';
		const dd = Math.floor(left / 86400), hh = Math.floor((left % 86400) / 3600), mm = Math.floor((left % 3600) / 60), ss = left % 60;
		return `${dd ? `${dd}d ` : ''}${hh}h ${mm}m ${ss}s`;
	});
	$effect(() => {
		if (!airAt) return;
		const id = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(id);
	});

	async function playTrailer(e: MouseEvent) {
		const v = trailer;
		if (!v) return;
		const el = e.currentTarget as HTMLElement;
		const list: TrailerSource[] = await trailerSources(kind, tmdb, videos);
		const why = missReason(kind, tmdb);
		const [source, ...fallbacks]: TrailerSource[] = list.length ? list : [why ? { kind: 'embed', key: v.key, why, told: videoReason(why) || undefined } : { kind: 'embed', key: v.key }];
		claimPlayer(() => {})();
		const r = el.getBoundingClientRect();
		openTheater({ kind, tmdb, title: name, what: 'trailer', source, fallbacks, time: 0, rect: { left: r.left, top: r.top, width: r.width, height: r.height }, from: 'panel', returnTo: el });
	}
</script>

{#snippet chips(list: string[])}<ul class="sc-chips">{#each list as c (c)}<li>{c}</li>{/each}</ul>{/snippet}

<section class="sc" data-theme="dark" data-testid="title-showcase">
	<div class="sc-bg" aria-hidden="true">{#if backdrop}<img src={sized(backdrop, 'backdrop', 1280)} alt="" />{/if}</div>
	<div class="sc-in">
		<aside class="sc-side">
			<div class="sc-poster">{#if poster}<img src={sized(poster, 'poster', 500)} alt="" />{:else}<span>{name}</span>{/if}</div>
			{#if trailer}<button type="button" class="sc-trailer" onclick={playTrailer}><Icon name="play" size={15} />Watch Trailer</button>{/if}
			{#if countdown}
				<div class="sc-next" data-testid="next-episode"><span>Next episode{nextAir ? ` · S${nextAir.season} E${nextAir.episode}` : ''}</span><b>{countdown}</b></div>
			{/if}
			<dl class="sc-info" data-testid="title-info">
				{#each facts as r (r.label)}<div><dt>{r.label}</dt><dd>{r.value}</dd></div>{/each}
				{#if languages.length}<div><dt>Audio languages</dt><dd>{@render chips(languages)}</dd></div>{/if}
				{#if themes.length}<div class="sc-sep"><dt>Themes</dt><dd>{@render chips(themes)}</dd></div>{/if}
				{#if studios.length}<div class="sc-sep"><dt>{kind === 'show' ? 'Studios and networks' : 'Studios'}</dt><dd>{@render chips(studios)}</dd></div>{/if}
			</dl>
		</aside>

		<div class="sc-main">
			<header class="sc-head">
				<div class="sc-names">
					{#if originalName && originalName !== name}<p class="sc-orig" dir="auto">{originalName}</p>{/if}
					<h1 dir="auto">{name}</h1>
					{#if pill}<span class="sc-pill">{pill}</span>{/if}
					{#if byline}<div class="sc-byline">{@render byline()}</div>{/if}
				</div>
				{#if logo}<img class="sc-logo" src={proxied(logo)} alt="" />{/if}
			</header>
			{#if genres.length}{@render chips(genres)}{/if}
			<div class="sc-bar">
				<div class="sc-acts">{@render actions?.()}</div>
				{#if scores}<div class="sc-scores">{@render scores()}</div>{/if}
			</div>
			{#if overview}
				<section class="sc-story">
					<h2>Synopsis</h2>
					<p class:sc-clamp={!longStory} dir="auto" data-testid="title-story">{overview}</p>
					{#if overview.length > 280}<button type="button" class="sc-more" onclick={() => (longStory = !longStory)}>{longStory ? 'Show less' : 'Read more'} <Icon name={longStory ? 'chevronUp' : 'chevronRight'} size={13} /></button>{/if}
				</section>
			{/if}
			{@render library?.()}
			{#if showTabs && tabs.length}
				<div class="sc-tabs" role="tablist" aria-label="Title sections">
					{#each tabs as [v, label] (v)}
						<button type="button" role="tab" aria-selected={tab === v} class:on={tab === v} onclick={() => (picked = v)}>{label}</button>
					{/each}
				</div>
				<div class="sc-panel" role="tabpanel">
					{#if tab === 'episodes'}{@render episodes?.()}{:else if tab === 'relations'}{@render relations?.()}{:else}{@render recommendations?.()}{/if}
				</div>
			{/if}
		</div>
	</div>
</section>

<style>
	:global(html body main:has(.sc)) { padding-top: 0; padding-inline: 0; padding-bottom: 0; }
	.sc { position: relative; isolation: isolate; min-height: 100dvh; color: var(--text); background: #0c0d11; }
	.sc-bg { position: absolute; inset: 0 0 auto; height: min(100dvh, 980px); z-index: -1; overflow: hidden; }
	.sc-bg img { width: 100%; height: 100%; object-fit: cover; filter: blur(2px) saturate(0.9); opacity: .32; transform: scale(1.03); }
	.sc-bg::after { content: ''; position: absolute; inset: 0; background: linear-gradient(180deg, rgb(12 13 17 / 55%), rgb(12 13 17 / 70%) 45%, #0c0d11 100%), linear-gradient(90deg, rgb(12 13 17 / 70%), transparent 60%); }
	.sc-in { display: grid; grid-template-columns: clamp(220px, 19vw, 290px) minmax(0, 1fr); gap: clamp(20px, 2.2vw, 36px); max-width: 1480px; margin: 0 auto; padding: clamp(72px, 8vh, 104px) var(--gutter) 64px; }

	.sc-side { display: flex; flex-direction: column; gap: 14px; min-width: 0; }
	.sc-poster { aspect-ratio: 2 / 3; border-radius: 6px; overflow: hidden; background: #1b1d24; box-shadow: 0 18px 50px rgb(0 0 0 / 50%); display: grid; place-items: center; }
	.sc-poster img { width: 100%; height: 100%; object-fit: cover; }
	.sc-poster span { padding: 16px; font-weight: 700; text-align: center; }
	.sc-trailer { display: inline-flex; align-items: center; justify-content: center; gap: 10px; height: 42px; border-radius: 4px; border: 1px solid rgb(255 255 255 / 14%); background: rgb(38 41 52 / 88%); color: #fff; font: 600 14px/1 var(--font-ui); cursor: pointer; }
	.sc-trailer:hover { background: rgb(52 56 70 / 92%); }
	.sc-next { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 14px 16px; border-radius: 4px; border: 1px solid rgb(52 211 153 / 30%); background: rgb(16 46 40 / 75%); color: #6ee7b7; }
	.sc-next span { font-size: 11.5px; font-weight: 600; letter-spacing: .08em; text-transform: uppercase; }
	.sc-next b { font: 700 14px/1 var(--font-ui); font-variant-numeric: tabular-nums; white-space: nowrap; }
	.sc-info { display: flex; flex-direction: column; margin: 0; padding: 6px 16px 10px; border-radius: 4px; border: 1px solid rgb(255 255 255 / 8%); background: rgb(22 24 31 / 82%); }
	.sc-info > div { padding: 10px 0; }
	.sc-info > .sc-sep { border-top: 1px solid rgb(255 255 255 / 8%); margin-top: 4px; padding-top: 14px; }
	.sc-info dt { font-size: 11.5px; font-weight: 600; letter-spacing: .09em; text-transform: uppercase; color: rgb(255 255 255 / 55%); }
	.sc-info dd { margin: 5px 0 0; font-size: 14px; font-weight: 600; color: #fff; overflow-wrap: anywhere; }

	.sc-chips { display: flex; flex-wrap: wrap; gap: 8px; margin: 0; padding: 0; list-style: none; }
	.sc-chips li { padding: 4px 10px; border-radius: 999px; border: 1.5px solid rgb(255 255 255 / 30%); font-size: 12px; font-weight: 600; }

	.sc-main { display: flex; flex-direction: column; gap: 18px; min-width: 0; }
	.sc-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 24px; min-height: 120px; }
	.sc-names { min-width: 0; padding-top: 40px; }
	.sc-orig { margin: 0 0 6px; font-size: 16px; font-weight: 700; color: rgb(255 255 255 / 88%); }
	h1 { margin: 0; font: 800 clamp(26px, 2.6vw, 38px)/1.1 var(--font-display, var(--font-ui)); text-transform: uppercase; letter-spacing: .01em; }
	.sc-pill { display: inline-block; margin-top: 10px; padding: 3px 9px; border-radius: 999px; background: rgb(255 255 255 / 12%); font-size: 12px; font-weight: 600; }
	.sc-byline { margin-top: 8px; color: rgb(255 255 255 / 70%); font-size: 13px; }
	.sc-logo { max-width: min(300px, 32%); max-height: 150px; object-fit: contain; filter: drop-shadow(0 4px 18px rgb(0 0 0 / 60%)); }
	.sc-bar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 14px 24px; }
	.sc-acts { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
	.sc-scores { display: flex; align-items: center; gap: 22px; }

	.sc-story h2, :global(.sc .tp-h2) { margin: 0 0 10px; font-size: 12px; font-weight: 600; letter-spacing: .1em; text-transform: uppercase; color: rgb(255 255 255 / 60%); }
	.sc-story p { margin: 0; max-width: 110ch; font-size: 14.5px; line-height: 1.6; color: rgb(255 255 255 / 80%); }
	.sc-story p.sc-clamp { display: -webkit-box; -webkit-line-clamp: 3; line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; }
	.sc-more { margin-top: 10px; display: inline-flex; align-items: center; gap: 4px; padding: 0; border: 0; background: none; color: #fff; font: 600 12.5px/1 var(--font-ui); cursor: pointer; }

	.sc-tabs { display: inline-flex; align-self: flex-start; gap: 4px; margin-top: 18px; padding: 5px; border-radius: 6px; border: 1px solid rgb(255 255 255 / 8%); background: rgb(22 24 31 / 82%); }
	.sc-tabs button { padding: 11px 16px; border: 1px solid transparent; border-radius: 4px; background: none; color: rgb(255 255 255 / 62%); font: 600 14px/1 var(--font-ui); cursor: pointer; }
	.sc-tabs button:hover { color: #fff; }
	.sc-tabs button.on { color: #fff; background: rgb(255 255 255 / 6%); border-color: rgb(255 255 255 / 12%); }
	.sc-tabs button:focus-visible { outline: 2px solid #fff; outline-offset: 2px; }
	.sc-panel { display: flex; flex-direction: column; gap: 32px; min-width: 0; }

	@media (max-width: 900px) {
		.sc-in { display: flex; flex-direction: column; gap: 16px; padding-top: 72px; }
		.sc-side { display: contents; }
		.sc-poster { order: 1; width: 150px; }
		.sc-trailer { order: 2; } .sc-next { order: 3; } .sc-main { order: 4; } .sc-info { order: 5; }
		.sc-head { min-height: 0; } .sc-names { padding-top: 0; } .sc-logo { display: none; }
		.sc-tabs { align-self: stretch; overflow-x: auto; scrollbar-width: none; } .sc-tabs button { flex: none; padding: 10px 12px; }
	}
</style>
