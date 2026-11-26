<script lang="ts">
	import Shelf from '#lib/ui/Shelf.svelte';
	import { to } from '#lib/core/paths.ts';
	import { detailPath } from '#lib/metadata/index.ts';
	import { listAddonCatalogs, type AddonCatalogItem, type AddonCatalogShelf } from './streaming-config-api.ts';

	let { type = 'all' }: { type?: string } = $props();

	let shelves = $state<AddonCatalogShelf[]>([]);

	const filterKind = $derived(type === 'movie' ? 'movie' : type === 'show' ? 'show' : '');

	$effect(() => {
		const k = filterKind;
		let alive = true;
		listAddonCatalogs(k).then(
			(res) => {
				if (alive) shelves = res.shelves ?? [];
			},
			() => {
				if (alive) shelves = [];
			}
		);
		return () => { alive = false; };
	});

	function itemHref(item: AddonCatalogItem): string {
		if (item.tmdb_id && item.tmdb_id > 0) {
			return to(detailPath(item.kind, String(item.tmdb_id)));
		}
		return to(`/search?q=${encodeURIComponent(item.name)}`);
	}
</script>

{#if shelves.length > 0}
	{#each shelves as shelf, sidx (`${shelf.addon_id}:${shelf.type}:${shelf.catalog_id}:${sidx}`)}
		<section class="addon-shelf" aria-label={shelf.name}>
			<Shelf title={`${shelf.name} · ${shelf.addon_name}`}>
				{#each shelf.items as item, idx (`${item.id}:${idx}`)}
					<a class="addon-card" href={itemHref(item)}>
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
				{/each}
			</Shelf>
		</section>
	{/each}
{/if}

<style>
	.addon-card {
		display: flex; flex-direction: column; gap: 6px; width: 146px; flex: none; text-decoration: none; color: var(--text);
	}
	.poster-wrap {
		width: 146px; aspect-ratio: 2 / 3; border-radius: 10px; overflow: hidden; background: var(--surface-2); border: 1px solid var(--border);
	}
	.poster-wrap img { width: 100%; height: 100%; object-fit: cover; display: block; }
	.poster-fallback { height: 100%; display: grid; place-items: center; padding: 10px; text-align: center; font-size: 12px; color: var(--muted); }
	.meta { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
	.meta strong { font-size: 13px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
	.meta span { font-size: 11.5px; color: var(--muted); }
</style>
