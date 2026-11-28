<script lang="ts">
	import Shelf from '#lib/ui/Shelf.svelte';
	import { api } from '#lib/core/api.ts';
	import { to } from '#lib/core/paths.ts';
	import { detailPath } from '#lib/metadata/index.ts';
	import { goto } from '$app/navigation';
	import { addonCatalogPage, listAddonCatalogs, type AddonCatalogItem, type AddonCatalogShelf } from './streaming-config-api.ts';
	import { imdbLookupPath, mayPage, newItems, nextHundred, shelfKey } from './addon-pages.ts';

	let { type = 'all' }: { type?: string } = $props();

	let shelves = $state<AddonCatalogShelf[]>([]);
	// Each shelf's later pages, its next skip and whether it has ended.
	let extra = $state<Record<string, AddonCatalogItem[]>>({});
	let ended = $state<Record<string, boolean>>({});
	const skipAt: Record<string, number> = {};
	const busy: Record<string, boolean> = {};
	const gone = new AbortController();
	$effect(() => () => gone.abort());

	const filterKind = $derived(
		type === 'movies' || type === 'movie'
			? 'movie'
			: type === 'shows' || type === 'show'
				? 'show'
				: type === 'anime'
					? 'anime'
					: ''
	);

	function isAnimeShelf(shelf: AddonCatalogShelf): boolean {
		if (shelf.type.trim().toLowerCase() === 'anime') return true;
		return /anime|kitsu|anilist|myanimelist/i.test(`${shelf.addon_id} ${shelf.addon_name} ${shelf.catalog_id} ${shelf.name}`);
	}

	$effect(() => {
		if (type === 'music') {
			shelves = [];
			return;
		}
		const k = filterKind;
		let alive = true;
		listAddonCatalogs(k).then(
			(res) => {
				if (!alive) return;
				const raw = (res.shelves ?? []).map((sh) => ({ ...sh, items: newItems([], sh.items) }));
				shelves = k === 'anime' ? raw.filter(isAnimeShelf) : raw.filter((sh) => !isAnimeShelf(sh));
				extra = {};
				ended = {};
			},
			() => {
				if (alive) shelves = [];
			}
		);
		return () => { alive = false; };
	});

	const itemsOf = (sh: AddonCatalogShelf): AddonCatalogItem[] => {
		const more = extra[shelfKey(sh)];
		return more?.length ? [...sh.items, ...more] : sh.items;
	};

	/** Reads the shelf's next page by skip; a page that brings nothing new jumps to the next hundred once, then the shelf ends. */
	async function more(sh: AddonCatalogShelf) {
		const k = shelfKey(sh);
		if (busy[k] || ended[k]) return;
		busy[k] = true;
		try {
			for (let tries = 0; tries < 2; tries++) {
				const have = itemsOf(sh);
				const skip = skipAt[k] ?? have.length;
				const a = await addonCatalogPage(sh, skip, gone.signal);
				const fresh = newItems(have, a.items);
				if (fresh.length) {
					extra[k] = [...(extra[k] ?? []), ...fresh];
					skipAt[k] = skip + (a.items?.length ?? 0);
					return;
				}
				if (!a.items?.length) break;
				skipAt[k] = nextHundred(skip);
			}
			ended[k] = true;
		} catch {
			ended[k] = true;
		} finally {
			busy[k] = false;
		}
	}

	function itemHref(item: AddonCatalogItem): string {
		if (item.tmdb_id && item.tmdb_id > 0) {
			return to(detailPath(item.kind, String(item.tmdb_id)));
		}
		return to(`/search?q=${encodeURIComponent(item.name)}`);
	}

	/** Calls go once the node comes within a screen and a half of view. */
	function near(node: HTMLElement, go: () => void) {
		const io = new IntersectionObserver(([e]) => {
			if (e.isIntersecting) {
				io.disconnect();
				go();
			}
		}, { rootMargin: '0px 0px 150% 0px' });
		io.observe(node);
		return { destroy: () => io.disconnect() };
	}

	/** An IMDb card finds its TMDB id first and opens the title page; the search stays its link when the lookup finds nothing. */
	async function open(e: MouseEvent, item: AddonCatalogItem) {
		if (!item.imdb_id || (item.tmdb_id ?? 0) > 0 || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
		e.preventDefault();
		try {
			const a = await api.get<{ movies?: { ids?: { tmdb?: string } }[]; shows?: { ids?: { tmdb?: string } }[] }>(imdbLookupPath(item), gone.signal);
			const tmdb = (item.kind === 'show' ? a.shows : a.movies)?.[0]?.ids?.tmdb;
			void goto(tmdb ? to(detailPath(item.kind, tmdb)) : itemHref(item));
		} catch {
			void goto(itemHref(item));
		}
	}
</script>

{#if shelves.length > 0}
	{#each shelves as shelf (shelfKey(shelf))}
		{@const items = itemsOf(shelf)}
		{#if items.length === 0}
			<!-- A shelf listed without items is read when it comes near; one that has none goes. -->
			{#if !ended[shelfKey(shelf)]}<section class="addon-shelf" aria-hidden="true" use:near={() => void more(shelf)}><div class="lazy-sk"></div></section>{/if}
		{:else}
		<section class="addon-shelf" aria-label={shelf.name}>
			<Shelf title={`${shelf.name} · ${shelf.addon_name}`} {items} key={(it) => it.id} item="152px" hasMore={mayPage({ pages: shelf.pages, items }) && !ended[shelfKey(shelf)]} onmore={() => void more(shelf)}>
				{#snippet card(item: AddonCatalogItem)}
					<a class="addon-card" href={itemHref(item)} onclick={(e) => void open(e, item)}>
						<div class="poster-wrap">
							{#if item.poster}
								<img src={item.poster} alt={item.name} loading="lazy" />
							{:else}
								<div class="poster-fallback">{item.name}</div>
							{/if}
						</div>
						<div class="meta">
							<strong>{item.name}</strong>
							{#if item.year}<span>{item.year}</span>{/if}
						</div>
					</a>
				{/snippet}
			</Shelf>
		</section>
		{/if}
	{/each}
{/if}

<style>
	.addon-shelf { margin-top: 4px; }
	.lazy-sk { height: 260px; border-radius: var(--r-card, 10px); background: linear-gradient(90deg, var(--surface-2), transparent); }
	.addon-card {
		display: flex; flex-direction: column; gap: 6px; width: 152px; flex: none; text-decoration: none; color: var(--text);
		transition: transform var(--dur-fast) var(--ease-out);
	}
	.addon-card:hover { transform: translateY(-2px); }
	.poster-wrap {
		width: 152px; aspect-ratio: 2 / 3; border-radius: var(--r-card, 10px); overflow: hidden;
		background: var(--surface-2); border: 1px solid var(--line);
		box-shadow: 0 4px 16px rgb(0 0 0 / 0.25);
		transition: border-color var(--dur-fast) var(--ease-out), box-shadow var(--dur-fast) var(--ease-out);
	}
	.addon-card:hover .poster-wrap {
		border-color: rgb(255 255 255 / 24%);
		box-shadow: 0 8px 24px rgb(0 0 0 / 0.42);
	}
	.poster-wrap img { width: 100%; height: 100%; object-fit: cover; display: block; transition: transform 0.25s var(--ease-out); }
	.addon-card:hover .poster-wrap img { transform: scale(1.03); }
	.poster-fallback { height: 100%; display: grid; place-items: center; padding: 10px; text-align: center; font-size: 12px; color: var(--text-muted); }
	.meta { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
	.meta strong { font-size: 13px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
	.meta span { font-size: 11.5px; color: var(--text-muted); }
</style>
