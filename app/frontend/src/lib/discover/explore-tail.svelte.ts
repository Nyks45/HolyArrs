// The rows under the head: tail pages read as the page scrolls down, past the pool into the endless tail. Explore and Home share it.

import { api } from '#lib/core/api.ts';
import { cardIdentity, type ExploreCard, type ExploreRow } from '#lib/ui/title-cards.ts';
import { serverRow, tailPath, type HeadQuery, type TailAnswer } from './explore-server.ts';

export type TailRow = ExploreRow & { wide: boolean; type: string };

/** What a left page keeps of its tail, to put back on Back. */
export interface TailKept {
	rows: TailRow[];
	next: number;
	more: boolean;
}

/** A row already on the page (by id) is never added again: a page laid out again between two asks may repeat one. */
export function addTailRows<R extends { id: string; cards?: readonly ExploreCard[] }>(have: readonly { id: string; cards?: readonly ExploreCard[] }[], rows: readonly R[], got: readonly R[]): R[] {
	const ids = new Set([...have, ...rows].map((r) => r.id));
	const out = [...rows];
	const recent = () => [...have, ...out].slice(-NEAR_ROWS);
	for (const r of got) {
		if (ids.has(r.id)) continue;
		ids.add(r.id);
		out.push(leaveNear(r, recent()));
	}
	return out;
}

/** How many rows above a new tail row its cards are not repeated from, and the fewest cards that leaves it. */
const NEAR_ROWS = 3;
const NEAR_MIN = 8;

/** Leaves out of a new row the titles the rows right above it show, while it keeps NEAR_MIN cards. */
function leaveNear<R extends { cards?: readonly ExploreCard[] }>(r: R, above: readonly { cards?: readonly ExploreCard[] }[]): R {
	if (!r.cards) return r;
	const shown = new Set(above.flatMap((a) => (a.cards ?? []).map(cardIdentity)));
	const kept = r.cards.filter((c) => !shown.has(cardIdentity(c)));
	return kept.length === r.cards.length || kept.length < NEAR_MIN ? r : { ...r, cards: kept };
}

export function createExploreTail(ctx: {
	query: () => HeadQuery;
	signal: AbortSignal;
	/** The head rows, which the tail never repeats. */
	head: () => readonly { id: string }[];
	/** The end of the page is still near: read the next page at once. */
	near?: () => boolean;
}) {
	let rows = $state<TailRow[]>([]);
	let more = $state(false);
	let next = 0;
	let busy = false;
	let gen = 0;

	function reset(on = true) {
		gen++;
		rows = [];
		next = 0;
		more = on;
		busy = false;
	}

	async function load() {
		if (busy || !more) return;
		busy = true;
		const g = gen;
		try {
			const a = await api.get<TailAnswer>(tailPath(ctx.query(), next), ctx.signal);
			if (g !== gen) return;
			rows = addTailRows(ctx.head(), rows, a.rows.map(serverRow));
			next++;
			more = a.more;
		} catch {
			if (g === gen) more = false;
		} finally {
			if (g === gen) busy = false;
		}
		// Still within reach of the end (a tall screen, or a page of thin rows left out): the next page too.
		requestAnimationFrame(() => {
			if (g === gen && more && ctx.near?.()) void load();
		});
	}

	return {
		get rows() {
			return rows;
		},
		get more() {
			return more;
		},
		reset,
		load,
		keep: (): TailKept => ({ rows: $state.snapshot(rows) as TailRow[], next, more }),
		restore(k: TailKept) {
			gen++;
			busy = false;
			rows = k.rows;
			next = k.next;
			more = k.more;
		}
	};
}

/** Calls load when the node comes within 1.5 screens of view. */
export function tailSentinel(node: HTMLElement, load: () => void) {
	let go = load;
	const io = new IntersectionObserver(([e]) => e.isIntersecting && go(), { rootMargin: '150% 0px' });
	io.observe(node);
	return {
		update(next: () => void) {
			go = next;
		},
		destroy: () => io.disconnect()
	};
}
