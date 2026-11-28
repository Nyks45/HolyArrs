<!-- The Watchlist's two switches: which kind (Movies, TV shows, Music) and which watch state, each with its count. -->
<script lang="ts">
	import Icon, { type IconName } from '#lib/ui/Icon.svelte';
	import { t } from '#lib/core/i18n/i18n.svelte.ts';
	import { appLocale } from '#lib/core/i18n/i18n.svelte.ts';
	import { seenFilters, type SeenCounts, type SeenFilter } from './watch-state.ts';

	type KindTab = { key: string; label: string; href: string; icon: IconName; on: boolean; count?: number };
	let { kinds, seen, counts, showSeen = true, loaded = true, onseen }: { kinds: KindTab[]; seen: SeenFilter; counts: SeenCounts; showSeen?: boolean; loaded?: boolean; onseen: (f: SeenFilter) => void } = $props();

	const seenIcon: Record<SeenFilter, IconName> = { all: 'grid', unwatched: 'eye', progress: 'clock', watched: 'check' };
	const num = (n: number | undefined) => (loaded && n !== undefined ? n.toLocaleString(appLocale()) : '');
	let seenEls: HTMLButtonElement[] = $state([]);
	// Arrows move between the watch states, like a radio group.
	function key(e: KeyboardEvent, i: number) {
		const step = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0;
		if (!step) return;
		e.preventDefault();
		const to = (i + step + seenFilters.length) % seenFilters.length;
		onseen(seenFilters[to]);
		seenEls[to]?.focus();
	}
</script>

<div class="wl-tabs">
	{#if kinds.length > 1}
		<nav class="wl-seg wl-kinds" aria-label={t('library.watch.kindsLabel')}>
			{#each kinds as k (k.key)}
				<a href={k.href} class:on={k.on} aria-current={k.on ? 'page' : undefined} data-testid="watchlist-kind-{k.key}"><Icon name={k.icon} size={15} /><span>{k.label}</span>{#if num(k.count)}<em>{num(k.count)}</em>{/if}</a>
			{/each}
		</nav>
	{/if}
	{#if showSeen}
		<div class="wl-seg wl-seen" role="radiogroup" aria-label={t('library.watch.seenLabel')}>
			{#each seenFilters as f, i (f)}
				<button type="button" role="radio" aria-checked={seen === f} tabindex={seen === f ? 0 : -1} class:on={seen === f} bind:this={seenEls[i]} onclick={() => onseen(f)} onkeydown={(e) => key(e, i)} data-testid="watchlist-seen-{f}"><Icon name={seenIcon[f]} size={15} /><span>{t(`library.watch.seen.${f}`)}</span>{#if num(counts[f])}<em>{num(counts[f])}</em>{/if}</button>
			{/each}
		</div>
	{/if}
</div>

<style>
	.wl-tabs { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 10px 16px; min-width: 0; }
	.wl-seg { display: inline-flex; gap: 3px; min-width: 0; padding: 4px; border-radius: 14px; background: rgb(255 255 255 / 7%); box-shadow: inset 0 0 0 1px rgb(255 255 255 / 9%); -webkit-backdrop-filter: blur(18px) saturate(160%); backdrop-filter: blur(18px) saturate(160%); }
	.wl-seg :is(a, button) { position: relative; display: inline-flex; align-items: center; gap: 7px; height: 38px; padding: 0 14px; border: 0; border-radius: 10px; background: none; color: rgb(255 255 255 / 66%); font: 600 13.5px/1 var(--font-ui, inherit); text-decoration: none; white-space: nowrap; cursor: pointer; transition: color var(--dur-fast) var(--ease-out), background var(--dur-fast) var(--ease-out), box-shadow var(--dur-fast) var(--ease-out); }
	.wl-seg :is(a, button):hover { color: #fff; background: rgb(255 255 255 / 6%); }
	.wl-seg :is(a, button).on { color: #fff; background: rgb(255 255 255 / 14%); box-shadow: inset 0 1px 0 rgb(255 255 255 / 12%), 0 4px 14px rgb(0 0 0 / 30%); }
	.wl-seg :is(a, button):focus-visible { outline: none; box-shadow: var(--focus-ring); }
	.wl-seg em { min-width: 20px; padding: 3px 6px; box-sizing: border-box; border-radius: 99px; background: rgb(255 255 255 / 10%); font-style: normal; font-size: 11.5px; font-weight: 700; text-align: center; font-variant-numeric: tabular-nums; color: rgb(255 255 255 / 80%); }
	.wl-seg .on em { background: var(--accent); color: #fff; }
	.wl-seen button[data-testid='watchlist-seen-watched'].on :global(svg) { color: var(--ok); }
	.wl-seen button[data-testid='watchlist-seen-progress'].on :global(svg) { color: var(--info); }

	@media (max-width: 639px) {
		.wl-tabs { flex-direction: column; flex-wrap: nowrap; align-items: stretch; gap: 8px; }
		/* Phones switch the kind from the count menu in the bar below, so only the watch states stay. */
		.wl-kinds { display: none; }
		.wl-seg { overflow-x: auto; scrollbar-width: none; }
		.wl-seg { gap: 2px; padding: 3px; }
		.wl-seg :is(a, button) { flex: 1 0 auto; justify-content: center; gap: 4px; height: 36px; padding: 0 5px; font-size: 12px; }
		.wl-seg :is(a, button) :global(svg) { display: none; }
		.wl-seg em { min-width: 16px; padding: 2px 4px; font-size: 10.5px; }
	}
	@media (prefers-reduced-transparency: reduce) { .wl-seg { backdrop-filter: none; -webkit-backdrop-filter: none; background: rgb(32 32 36 / 96%); } }
</style>
