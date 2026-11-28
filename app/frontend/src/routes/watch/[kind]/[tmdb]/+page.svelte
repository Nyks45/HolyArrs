<script lang="ts">
	import { page } from '$app/state';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import { to } from '#lib/core/paths.ts';
	import { session } from '#lib/core/session.svelte.ts';
	import { t } from '#lib/core/i18n/i18n.svelte.ts';
	import { WatchPlayer } from '#lib/stream/index.ts';

	const kind = $derived(page.params.kind ?? '');
	const tmdb = $derived(page.params.tmdb ?? '');
	const validKind = $derived(kind === 'movie' || kind === 'show');
	const season = $derived(Number(page.url.searchParams.get('s')) || 0);
	const episode = $derived(Number(page.url.searchParams.get('e')) || 0);
	const autoResume = $derived(page.url.searchParams.get('resume') === '1');
	const initialDrawer = $derived(page.url.searchParams.get('drawer') === 'sources' ? 'sources' : 'none');
	const backHref = $derived(page.url.searchParams.get('from') ?? '');
</script>

{#if session.kid}
	<EmptyState icon="alert" title={t('discover.titlePage.thisTitleIsNot')} action={{ label: t('home.home'), href: to('/') }} />
{:else if !validKind || !tmdb}
	<EmptyState
		icon="alert"
		title="Stream not found"
		text="Expected a movie or TV show TMDB id."
		action={{ label: 'Back Home', href: to('/') }}
	/>
{:else}
	{#key `${kind}/${tmdb}`}
		<WatchPlayer
			kind={kind === 'show' ? 'show' : 'movie'}
			{tmdb}
			initialSeason={season}
			initialEpisode={episode}
			{autoResume}
			{initialDrawer}
			{backHref}
		/>
	{/key}
{/if}
