<script lang="ts">
	import { page } from '$app/state';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import { to } from '#lib/core/paths.ts';
	import { WatchPlayer } from '#lib/stream/index.ts';

	const kind = $derived(page.params.kind ?? '');
	const tmdb = $derived(page.params.tmdb ?? '');
	const validKind = $derived(kind === 'movie' || kind === 'show');
	const season = $derived(Number(page.url.searchParams.get('s')) || 0);
	const episode = $derived(Number(page.url.searchParams.get('e')) || 0);
	const autoResume = $derived(page.url.searchParams.get('resume') === '1');
	const backHref = $derived(page.url.searchParams.get('from') ?? '');
</script>

{#if !validKind || !tmdb}
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
			{backHref}
		/>
	{/key}
{/if}
