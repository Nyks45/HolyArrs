<script lang="ts">
	import Icon from '#lib/ui/Icon.svelte';
	import type { CategorySummary } from './subcategories.ts';

	let {
		categories = [],
		activeId = '',
		onpick,
		onopenall
	}: {
		categories: CategorySummary[];
		activeId?: string;
		onpick: (id: string | null) => void;
		onopenall: () => void;
	} = $props();

	const visible = $derived(categories.slice(0, 18));
</script>

{#if visible.length > 0}
	<div class="subcat-bar" data-testid="explore-subcat-bar" role="navigation" aria-label="Subcategories">
		<div class="subcat-scroll">
			{#if activeId}
				<button type="button" class="subcat-pill clear" onclick={() => onpick(null)}>
					<Icon name="x" size={12} />
					<span>All subcategories</span>
				</button>
			{/if}
			{#each visible as c (c.id)}
				<button
					type="button"
					class="subcat-pill"
					class:active={activeId === c.id}
					aria-pressed={activeId === c.id}
					onclick={() => onpick(activeId === c.id ? null : c.id)}
				>
					<span>{c.name}</span>
				</button>
			{/each}
		</div>
		<button type="button" class="subcat-pill more-btn" data-testid="explore-all-subcategories" onclick={onopenall}>
			<Icon name="sliders" size={13} />
			<span>Browse all</span>
		</button>
	</div>
{/if}

<style>
	.subcat-bar { display: flex; align-items: center; gap: 8px; min-width: 0; }
	:global(.hub-strip) + .subcat-bar { margin-top: -18px; }
	.subcat-scroll {
		display: flex; flex-wrap: nowrap; align-items: center; gap: 7px; overflow-x: auto;
		padding: 2px 2px 4px; min-width: 0; flex: 1; scrollbar-width: none; overscroll-behavior-x: contain;
	}
	.subcat-scroll::-webkit-scrollbar { display: none; }
	.subcat-pill {
		flex: none; display: inline-flex; align-items: center; gap: 6px; min-height: 30px; padding: 0 12px;
		border-radius: 999px; border: 1px solid var(--line);
		background: color-mix(in srgb, var(--surface-1) 82%, transparent);
		color: var(--text-muted); font: 500 12.5px/1 var(--font-ui); cursor: pointer; white-space: nowrap;
		transition: background-color var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out), border-color var(--dur-fast) var(--ease-out);
	}
	.subcat-pill:hover { color: var(--text); border-color: var(--text-faint); background: var(--surface-2); }
	.subcat-pill.active { background: var(--text); color: var(--bg); border-color: var(--text); font-weight: 600; }
	.subcat-pill.clear { color: var(--text); border-color: color-mix(in srgb, var(--text) 30%, var(--line)); }
	.subcat-pill.more-btn { color: var(--text); border-style: dashed; }
	@media (pointer: coarse) { .subcat-pill { min-height: 38px; } }
</style>
