<script lang="ts">
	import type { ComponentProps } from 'svelte';
	import { t } from '#lib/core/i18n/i18n.svelte.ts';
	import { to } from '#lib/core/paths.ts';
	import type { IconName } from '#lib/ui/Icon.svelte';
	import Popover from '#lib/ui/Popover.svelte';
	import SegmentedControl from '#lib/ui/SegmentedControl.svelte';
	import { MusicGenreTiles } from '#lib/music/index.ts';
	import { MetadataNotice } from '#lib/metadata/index.ts';
	import ExploreHubBar from './ExploreHubBar.svelte';
	import SubcategoryBar from './SubcategoryBar.svelte';
	import type { ExploreHubInfo } from './explore-hub.svelte.ts';
	import type { ExploreType } from './explore-type.ts';
	import type { CategorySummary } from './subcategories.ts';

	interface GenreRef { id: number; name: string }
	type GenreTiles = ComponentProps<typeof MusicGenreTiles>['genres'];

	let {
		type,
		typeOptions,
		onTypeChange,
		musicGenres = [],
		genres = [],
		genreTiles = [],
		activeGenre = null,
		pickW,
		loadBrowseArt,
		onPickGenre,
		hubs = [],
		activeHubId = '',
		activeHubInfo = null,
		onPickHub,
		subcategories = [],
		activeCatId = '',
		onPickCat,
		onOpenAllCats,
		typesStuck = false,
		typesMark = $bindable(),
		typesBox = $bindable(),
		onMeasurePick
	}: {
		type: ExploreType;
		typeOptions: { value: ExploreType; label: string; icon: IconName }[];
		onTypeChange: (next: ExploreType) => void;
		musicGenres: GenreTiles;
		genres: GenreRef[];
		genreTiles: GenreTiles;
		activeGenre: GenreRef | null;
		pickW: number;
		loadBrowseArt: (t: ExploreType) => Promise<void>;
		onPickGenre: (id: number | null, close: () => void) => void;
		hubs: ExploreHubInfo[];
		activeHubId: string;
		activeHubInfo: ExploreHubInfo | null;
		onPickHub: (id: string | null) => void;
		subcategories: CategorySummary[];
		activeCatId: string;
		onPickCat: (id: string | null) => void;
		onOpenAllCats: () => void;
		typesStuck: boolean;
		typesMark?: HTMLElement;
		typesBox?: HTMLElement;
		onMeasurePick: () => void;
	} = $props();
</script>

<div class="types-mark" bind:this={typesMark} aria-hidden="true"></div>
<div class="types" class:stuck={typesStuck} data-pin-pills bind:this={typesBox} onpointerdowncapture={onMeasurePick} onkeydowncapture={onMeasurePick}>
	<SegmentedControl
		options={typeOptions}
		value={type}
		label={t('discover.explore.show')}
		testid="explore-type"
		announce={(l) => t('discover.explore.showing', { label: l })}
		onchange={onTypeChange}
	/>
	{#if type === 'music' && musicGenres.length}
		<div class="genre-pick" data-testid="genre-picker">
			<Popover label={t('discover.explore.genres')} text={t('discover.explore.genres')} variant="button" align="end" width={pickW}>
				{#snippet children(close)}
					<div class="genre-tiles" role="group" aria-label={t('discover.explore.genres')}>
						<MusicGenreTiles genres={musicGenres} allHref={to('/search?kind=music')} compact all={false} onopen={close} />
					</div>
				{/snippet}
			</Popover>
		</div>
	{:else if genres.length && type !== 'anime'}
		<div class="genre-pick" data-testid="genre-picker">
			<Popover label={t('discover.explore.genres')} text={activeGenre ? activeGenre.name : t('discover.explore.genresWord')} variant="button" align="end" width={pickW} onopen={() => void loadBrowseArt(type)}>
				{#snippet children(close)}
					<div class="genre-tiles" role="group" aria-label={t('discover.explore.genres')}>
						<MusicGenreTiles genres={genreTiles} allHref="" compact all={false} selected={activeGenre ? String(activeGenre.id) : ''} onpick={(k) => onPickGenre(k === null ? null : Number(k), close)} />
					</div>
				{/snippet}
			</Popover>
		</div>
	{/if}
</div>
<MetadataNotice />
{#if type !== 'music' && type !== 'anime'}
	<ExploreHubBar {hubs} activeId={activeHubId} activeInfo={activeHubInfo} onpick={onPickHub} />
{/if}
{#if type !== 'music'}
	<SubcategoryBar categories={subcategories} activeId={activeCatId} onpick={onPickCat} onopenall={onOpenAllCats} />
{/if}

<style>
	.types-mark { height: 0; margin-top: -32px; }
	.types { position: sticky; top: calc(var(--topbar) + 10px); z-index: 20; display: flex; justify-content: center; margin-top: -12px; pointer-events: none; }
	.types :global(.ui-seg) { pointer-events: auto; transition: transform var(--bar-glide), box-shadow var(--bar-glide), background-color var(--bar-glide); transform-origin: top center; }
	.types.stuck :global(.ui-seg) { transform: scale(0.9); background: color-mix(in srgb, var(--surface-2) 70%, transparent); -webkit-backdrop-filter: saturate(180%) blur(20px); backdrop-filter: saturate(180%) blur(20px); box-shadow: 0 10px 30px rgb(0 0 0 / 0.28), 0 0 0 0.5px color-mix(in srgb, var(--text) 14%, transparent); }
	.types.stuck { --seg-hit: 49px; }
	@media (prefers-reduced-motion: reduce) { .types :global(.ui-seg), .types :global(.ui-seg .thumb), .genre-pick :global(.trigger) { transition: none; } }
	@media (max-width: 767px) { .types { top: max(16px, env(safe-area-inset-top)); max-width: 100%; } }
	@media (max-width: 767px) { .types :global(.ui-seg) { flex: 1 1 auto; display: grid; width: auto; min-width: 0; } .types :global(.ui-seg button) { padding: 0 8px; } .genre-pick { flex: none; min-width: 0; } .genre-pick :global(.trigger) { max-width: 36vw; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } }
	@media (max-width: 479px) { .types :global(.ui-seg button) { padding: 0 6px; font-size: 13px; min-width: max-content; white-space: nowrap; } .genre-pick { flex: 0 1 auto; margin-left: 6px; } .genre-pick :global(.trigger) { padding: 0 12px; font-size: 13px; } }
	/* Small phones: each pill is as wide as its name so none breaks onto two lines; the picked one carries the fill, as an equal-width thumb would miss. */
	@media (max-width: 479px) {
		.types :global(.ui-seg) { grid-auto-columns: auto; }
		.types :global(.ui-seg .thumb) { display: none; }
		.types :global(.ui-seg button[aria-checked='true']) { background: var(--seg-thumb); box-shadow: 0 1px 2px rgb(0 0 0 / 12%), 0 0 0 .5px rgb(0 0 0 / 4%); }
	}
	@media (min-width: 360px) and (max-width: 479px) { .types :global(.ui-seg button svg) { display: none; } }
	@media (max-width: 359px) {
		.types :global(.ui-seg button span) { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
	}
	.genre-pick { pointer-events: auto; margin-left: 8px; display: flex; align-items: center; }
	.genre-pick :global(.trigger) { min-height: 36px; border-radius: 10px; padding: 0 14px; font: 600 14px/1 var(--font-ui); background: var(--surface-2); color: var(--text); transition: transform var(--bar-glide); transform-origin: top center; }
	@media (pointer: coarse) { .genre-pick :global(.trigger) { min-height: 44px; } }
	.types.stuck .genre-pick :global(.trigger) { transform: scale(0.9); background: color-mix(in srgb, var(--surface-2) 70%, transparent); -webkit-backdrop-filter: saturate(180%) blur(20px); backdrop-filter: saturate(180%) blur(20px); box-shadow: 0 10px 30px rgb(0 0 0 / 0.28), 0 0 0 0.5px color-mix(in srgb, var(--text) 14%, transparent); }
	:global(html[data-look='minimal']) .types.stuck :global(.ui-seg), :global(html[data-look='minimal']) .types.stuck .genre-pick :global(.trigger) { background: var(--surface-2); -webkit-backdrop-filter: none; backdrop-filter: none; box-shadow: 0 0 0 0.5px color-mix(in srgb, var(--text) 14%, transparent); }
	.genre-pick :global(.panel) { max-height: min(calc(100dvh - var(--topbar, 64px) - 96px), 560px); padding: 0; overscroll-behavior: contain; scrollbar-width: thin; scrollbar-color: color-mix(in srgb, var(--text) 28%, transparent) transparent; }
	.genre-pick :global(.panel::-webkit-scrollbar) { width: 8px; }
	.genre-pick :global(.panel::-webkit-scrollbar-thumb) { border-radius: 8px; background: color-mix(in srgb, var(--text) 28%, transparent); }
	.genre-tiles { padding: 14px; }
	@media (max-width: 767px) { .genre-pick :global(.panel) { max-width: calc(100vw - 16px); } .genre-tiles { padding: 10px; } }
</style>
