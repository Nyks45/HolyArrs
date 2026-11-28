<!-- A Watchlist row's watch actions: Play or Resume, and Mark watched or unwatched. Nothing outside the Watchlist page. -->
<script lang="ts">
	import IconButton from '#lib/ui/IconButton.svelte';
	import type { TitleView } from '../core/types.ts';
	import { t as tr } from '#lib/core/i18n/i18n.svelte.ts';
	import { watchList } from './watch-list.svelte.ts';

	let { t, tabindex = -1 }: { t: TitleView; tabindex?: number } = $props();
	const acts = $derived(t.kind === 'movie' || t.kind === 'show' ? watchList.acts : null);
	const play = $derived(acts?.play(t) ?? null);
	const marked = $derived(acts?.marked(t) ?? false);
	const busy = $derived(watchList.busy.includes(t.id));
</script>

{#if acts}
	{#if play}<IconButton icon="play" size="sm" tipSide="below-end" label={tr(`library.watch.${play.how}Name`, { name: t.name })} href={play.href} {tabindex} data-testid="watch-row-play" />{/if}
	<IconButton icon={marked ? 'eyeOff' : 'check'} size="sm" tipSide="below-end" class={marked ? 'wl-on' : ''} label={tr(marked ? 'library.watch.unmarkName' : 'library.watch.markName', { name: t.name })} disabled={busy} {tabindex} onclick={(e) => { e.stopPropagation(); acts.toggle(t, !marked); }} data-testid="watch-row-mark" />
{/if}
