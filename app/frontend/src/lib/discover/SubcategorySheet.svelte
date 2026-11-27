<script lang="ts">
	import Icon from '#lib/ui/Icon.svelte';
	import type { CategorySummary } from './subcategories.ts';

	let {
		open = false,
		categories = [],
		activeId = '',
		onclose,
		onselect
	}: {
		open: boolean;
		categories: CategorySummary[];
		activeId?: string;
		onclose: () => void;
		onselect: (id: string) => void;
	} = $props();

	let filter = $state('');
	let dialog = $state<HTMLDialogElement>();

	$effect(() => {
		if (open && dialog && !dialog.open) dialog.showModal();
	});

	const filtered = $derived.by(() => {
		const q = filter.trim().toLowerCase();
		if (!q) return categories;
		return categories.filter((c) => {
			if (c.name.toLowerCase().includes(q) || c.id.toLowerCase().includes(q) || c.section_title.toLowerCase().includes(q)) {
				return true;
			}
			return (c.aliases ?? []).some((a) => a.toLowerCase().includes(q)) || (c.parent_genres ?? []).some((g) => g.toLowerCase().includes(q));
		});
	});

	const grouped = $derived.by(() => {
		const map = new Map<string, { title: string; items: CategorySummary[] }>();
		for (const c of filtered) {
			const key = c.section || 'other';
			let bucket = map.get(key);
			if (!bucket) {
				bucket = { title: c.section_title || 'More', items: [] };
				map.set(key, bucket);
			}
			bucket.items.push(c);
		}
		return Array.from(map.values());
	});

	function dismiss() {
		dialog?.close();
		onclose();
	}

	function cancel(e: Event) {
		e.preventDefault();
		dismiss();
	}

	function backdrop(e: MouseEvent) {
		if (e.target !== dialog || !dialog) return;
		const { left, right, top, bottom } = dialog.getBoundingClientRect();
		if (e.clientX < left || e.clientX > right || e.clientY < top || e.clientY > bottom) dismiss();
	}
</script>

{#if open}
	<dialog bind:this={dialog} class="subcat-sheet" aria-label="Browse all subcategories" data-testid="subcategories-sheet" oncancel={cancel} onclick={backdrop}>
		<header class="sheet-head">
			<div>
				<h2>Browse subcategories</h2>
				<p>{categories.length} curated themes, niches and subgenres</p>
			</div>
			<button type="button" class="close-btn" aria-label="Close subcategories" onclick={dismiss}>
				<Icon name="x" size={18} />
			</button>
		</header>

		<div class="search-wrap">
			<Icon name="search" size={16} />
			<input
				type="search"
				bind:value={filter}
				onkeydown={(e) => { if (e.key === 'Escape') cancel(e); }}
				placeholder="Search subcategories (e.g. legal, courtroom, medical, heist, isekai)..."
				aria-label="Filter subcategories"
			/>
		</div>

		<div class="sheet-body">
			{#if grouped.length === 0}
				<p class="empty-msg">No subcategories match “{filter.trim()}”.</p>
			{:else}
				{#each grouped as sec (sec.title)}
					<div class="sec-group">
						<h3>{sec.title}</h3>
						<div class="pill-grid">
							{#each sec.items as c (c.id)}
								<button
									type="button"
									class="sheet-pill"
									class:active={c.id === activeId}
									onclick={() => {
										dismiss();
										onselect(c.id);
									}}
								>
									{c.name}
								</button>
							{/each}
						</div>
					</div>
				{/each}
			{/if}
		</div>
	</dialog>
{/if}

<style>
	.subcat-sheet::backdrop {
		background: rgb(0 0 0 / 68%); backdrop-filter: blur(8px);
	}
	.subcat-sheet {
		width: min(760px, calc(100% - 40px)); max-height: min(82dvh, 720px);
		margin: auto; padding: 20px 22px; border-radius: 20px;
		background: var(--surface-1); color: var(--text);
		border: 1px solid var(--line); box-shadow: 0 24px 64px rgb(0 0 0 / 55%);
	}
	.subcat-sheet[open] { display: flex; flex-direction: column; gap: 14px; }
	.sheet-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
	.sheet-head h2 { margin: 0; font: var(--t-title2); }
	.sheet-head p { margin: 4px 0 0; font: var(--t-footnote); color: var(--text-muted); }
	.close-btn {
		flex: none; width: 34px; height: 34px; border: 1px solid var(--line); border-radius: 999px;
		background: var(--surface-2); color: var(--text); display: grid; place-items: center; cursor: pointer;
	}
	.close-btn:hover { background: var(--surface-3); }
	.search-wrap {
		display: flex; align-items: center; gap: 10px; padding: 0 14px; min-height: 42px;
		border-radius: 12px; border: 1px solid var(--line); background: var(--surface-2); color: var(--text-muted);
	}
	.search-wrap input {
		flex: 1; min-width: 0; border: 0; outline: 0; background: transparent;
		color: var(--text); font: var(--t-body);
	}
	.sheet-body {
		overflow-y: auto; display: flex; flex-direction: column; gap: 18px; padding-right: 4px;
		scrollbar-width: thin; scrollbar-color: rgb(255 255 255 / 22%) transparent;
	}
	.sec-group h3 { margin: 0 0 8px; font: 600 13px/1.2 var(--font-ui); text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); }
	.pill-grid { display: flex; flex-wrap: wrap; gap: 8px; }
	.sheet-pill {
		min-height: 34px; padding: 0 14px; border-radius: 999px;
		border: 1px solid var(--line); background: var(--surface-2); color: var(--text);
		font: 500 13px/1 var(--font-ui); cursor: pointer;
		transition: background-color var(--dur-fast) var(--ease-out), border-color var(--dur-fast) var(--ease-out);
	}
	.sheet-pill:hover { border-color: var(--text-faint); background: var(--surface-3); }
	.sheet-pill.active { background: var(--text); color: var(--bg); border-color: var(--text); font-weight: 600; }
	.empty-msg { color: var(--text-muted); text-align: center; padding: 28px 0; font: var(--t-body); }
</style>
