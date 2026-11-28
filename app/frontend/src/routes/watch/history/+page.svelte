<script lang="ts">
	import { onMount } from 'svelte';
	import { SvelteSet } from 'svelte/reactivity';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import PageLoader from '#lib/ui/PageLoader.svelte';
	import LoadError from '#lib/ui/LoadError.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import { message } from '#lib/core/api.ts';
	import { ago, dateTime } from '#lib/core/format.ts';
	import { to } from '#lib/core/paths.ts';
	import { t } from '#lib/core/i18n/i18n.svelte.ts';
	import { session } from '#lib/core/session.svelte.ts';
	import { toasts } from '#lib/core/toasts.svelte.ts';
	import { sized } from '#lib/core/tmdb-image.ts';
	import { detailPath } from '#lib/metadata/index.ts';
	import {
		deleteWatchHistory,
		formatDuration,
		formatTimeLeft,
		listWatchHistory,
		type WatchEntry
	} from '#lib/stream/index.ts';

	let loading = $state(true);
	let error = $state('');
	let items = $state<WatchEntry[]>([]);
	let filterKind = $state<'all' | 'in_progress' | 'finished' | 'movie' | 'show'>('all');
	let search = $state('');
	const removing = new SvelteSet<string>();
	const removalErrors = new Map<string, number>();

	const filtered = $derived.by(() => {
		const q = search.trim().toLowerCase();
		return items.filter((e) => {
			if (filterKind === 'in_progress' && e.completed) return false;
			if (filterKind === 'finished' && !e.completed) return false;
			if (filterKind === 'movie' && e.kind !== 'movie') return false;
			if (filterKind === 'show' && e.kind !== 'show') return false;
			if (q) {
				const hay = `${e.title} ${e.episode_name ?? ''} ${e.release_title ?? ''} ${e.source_name ?? ''}`.toLowerCase();
				if (!hay.includes(q)) return false;
			}
			return true;
		});
	});

	function watchHref(e: WatchEntry, resume: boolean): string {
		const base = `/watch/${e.kind}/${encodeURIComponent(e.tmdb)}`;
		const qs = new URLSearchParams();
		if (e.kind === 'show') {
			if (e.season) qs.set('s', String(e.season));
			if (e.episode) qs.set('e', String(e.episode));
		}
		qs.set('resume', resume ? '1' : '0');
		qs.set('from', to('/watch/history'));
		return to(`${base}?${qs.toString()}`);
	}

	async function load() {
		loading = true;
		error = '';
		try {
			const res = await listWatchHistory(200);
			items = res.items ?? [];
		} catch (e) {
			error = message(e);
		} finally {
			loading = false;
		}
	}

	async function removeEntry(e: WatchEntry) {
		const key = e.key;
		if (!key || removing.has(key)) return;
		const previousError = removalErrors.get(key);
		if (previousError !== undefined) {
			toasts.dismiss(previousError);
			removalErrors.delete(key);
		}
		removing.add(key);
		try {
			await deleteWatchHistory(key);
			items = items.filter((x) => x.key !== key);
		} catch (err) {
			removalErrors.set(key, toasts.push(t('plugins.removeFailed', { name: e.title }), 'fail', { body: message(err) }));
		} finally {
			removing.delete(key);
		}
	}

	onMount(() => {
		if (!session.kid) void load();
	});
</script>

<svelte:head>
	<title>Watch History · HolyArrs</title>
</svelte:head>

{#if session.kid}
	<EmptyState icon="alert" title="Watch History" text={t('server.accounts.your-account-may-not-do-this')} action={{ label: t('home.home'), href: to('/') }} />
{:else}
<div class="wh-page" data-testid="watch-history-page">
	<PageHeader title="Watch History" subtitle="Resume any title right where you left off with your remembered source and subtitles." />

	<div class="wh-bar">
		<div class="wh-pills" role="group" aria-label="Filter watch history">
			{#each [
				{ v: 'all', l: 'All' },
				{ v: 'in_progress', l: 'In Progress' },
				{ v: 'finished', l: 'Finished' },
				{ v: 'movie', l: 'Movies' },
				{ v: 'show', l: 'TV Shows' }
			] as opt (opt.v)}
				<button
					type="button"
					class="wh-pill"
					class:active={filterKind === opt.v}
					onclick={() => (filterKind = opt.v as typeof filterKind)}
				>
					{opt.l}
				</button>
			{/each}
		</div>

		<input
			type="search"
			class="wh-search"
			placeholder="Filter titles, episodes, or releases…"
			bind:value={search}
			aria-label="Filter watch history"
		/>
	</div>

	{#if error && !loading}
		<LoadError hasData={items.length > 0} message={error} onretry={() => void load()} />
	{:else}
		<PageLoader {loading} label="Loading watch history" onretry={() => void load()}>
			{#if items.length === 0}
				<EmptyState
					icon="clock"
					title="Nothing watched yet"
					text="Start watching any movie or TV episode from its page and your timestamp, source, quality, and subtitles will appear here."
					action={{ label: 'Browse Explore', href: to('/explore') }}
				/>
			{:else if filtered.length === 0}
				<p class="wh-empty">No entries matched the current filter.</p>
			{:else}
				<ul class="wh-list">
					{#each filtered as entry (entry.key ?? `${entry.kind}:${entry.tmdb}:${entry.season}:${entry.episode}`)}
						{@const art = entry.backdrop || entry.poster}
						<li class="wh-row">
							<a class="wh-thumb" href={watchHref(entry, !entry.completed)}>
								{#if art}
									<img src={sized(art, 'backdrop', 340)} alt="" loading="lazy" decoding="async" />
								{:else}
									<span class="wh-ph"><Icon name={entry.kind === 'show' ? 'tv' : 'film'} size={24} /></span>
								{/if}
								{#if (entry.progress_pct ?? 0) > 0}
									<span class="wh-prog"><i style="width: {Math.min(100, entry.progress_pct ?? 0)}%"></i></span>
								{/if}
							</a>

							<div class="wh-info">
								<div class="wh-head">
									<a class="wh-title" href={watchHref(entry, !entry.completed)} dir="auto">{entry.title}</a>
									{#if entry.kind === 'show' && entry.season}
										<span class="wh-ep">
											S{String(entry.season).padStart(2, '0')} E{String(entry.episode ?? 1).padStart(2, '0')}
											{#if entry.episode_name}· {entry.episode_name}{/if}
										</span>
									{/if}
								</div>

								<div class="wh-stats">
									<span class="wh-status" class:done={entry.completed}>
										{#if entry.completed}
											Finished
										{:else}
											At {formatDuration(entry.position_sec)}
											{#if entry.duration_sec > 0}
												/ {formatDuration(entry.duration_sec)} ({formatTimeLeft(entry.position_sec, entry.duration_sec)})
											{/if}
										{/if}
									</span>
									{#if entry.updated_at}
										<span>·</span>
										<span title={dateTime(entry.updated_at)}>Watched {ago(entry.updated_at)}</span>
									{/if}
								</div>

								<div class="wh-tags">
									{#if entry.source_name}<span class="wh-tag">{entry.source_name}</span>{/if}
									{#if entry.resolution}<span class="wh-tag">{entry.resolution}</span>{/if}
									{#if entry.quality && entry.quality !== 'original'}<span class="wh-tag">{entry.quality} stream</span>{/if}
									{#if entry.subtitle && entry.subtitle.mode !== 'off'}
										<span class="wh-tag">Subs: {entry.subtitle.language?.toUpperCase() || entry.subtitle.label || entry.subtitle.mode}</span>
									{/if}
									{#if entry.release_title}
										<span class="wh-rel" title={entry.release_title}>{entry.release_title}</span>
									{/if}
								</div>
							</div>

							<div class="wh-acts">
								<a class="wh-btn wh-btn-primary" href={watchHref(entry, !entry.completed)}>
									<Icon name="play" size={14} />
									<span>{entry.completed ? 'Watch Again' : 'Resume'}</span>
								</a>
								{#if !entry.completed && entry.position_sec > 10}
									<a class="wh-btn" href={watchHref(entry, false)} title="Start from beginning">
										<Icon name="replay" size={14} />
										<span>Start Over</span>
									</a>
								{/if}
								<a class="wh-btn" href={to(detailPath(entry.kind, entry.tmdb))}>
									<Icon name="info" size={14} />
									<span>Details</span>
								</a>
								<button type="button" class="wh-btn wh-btn-danger" onclick={() => void removeEntry(entry)} disabled={removing.has(entry.key ?? '')} aria-label="Remove" title="Remove from history">
									<Icon name="trash" size={14} />
								</button>
							</div>
						</li>
					{/each}
				</ul>
			{/if}
		</PageLoader>
	{/if}
</div>
{/if}

<style>
	.wh-page { display: flex; flex-direction: column; gap: 20px; max-width: 1100px; }
	.wh-bar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; }
	.wh-pills { display: flex; flex-wrap: wrap; gap: 6px; }
	.wh-pill {
		min-height: 34px; padding: 0 14px; border: 1px solid var(--line); border-radius: 999px;
		background: transparent; color: var(--text-muted); font: 600 13px/1 var(--font-ui); cursor: pointer;
	}
	.wh-pill.active { background: var(--text); color: var(--bg); border-color: var(--text); }
	.wh-search {
		height: 38px; min-width: min(280px, 100%); padding: 0 12px; border: 1px solid var(--line);
		border-radius: 10px; background: var(--surface-1); color: var(--text); font: 500 14px/1 var(--font-ui);
	}
	.wh-empty { padding: 28px 0; color: var(--text-muted); font: 500 14px/1.4 var(--font-ui); }
	.wh-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 12px; }
	.wh-row {
		display: flex; align-items: center; gap: 16px; padding: 12px; border: 1px solid var(--line);
		border-radius: 16px; background: var(--surface-1);
	}
	.wh-thumb {
		position: relative; width: 176px; aspect-ratio: 16 / 9; flex: none; border-radius: 10px;
		overflow: hidden; background: var(--surface-2); color: var(--text-muted);
	}
	.wh-thumb img { width: 100%; height: 100%; object-fit: cover; }
	.wh-ph { position: absolute; inset: 0; display: grid; place-items: center; }
	.wh-prog { position: absolute; left: 0; right: 0; bottom: 0; height: 4px; background: rgb(255 255 255 / 26%); }
	.wh-prog i { display: block; height: 100%; background: #e50914; }

	.wh-info { min-width: 0; flex: 1; display: flex; flex-direction: column; gap: 6px; }
	.wh-head { display: flex; flex-wrap: wrap; align-items: baseline; gap: 8px; }
	.wh-title { font: 700 16px/1.25 var(--font-ui); color: var(--text); text-decoration: none; }
	.wh-title:hover { text-decoration: underline; }
	.wh-ep { font: 600 13.5px/1.2 var(--font-ui); color: var(--text-muted); }
	.wh-stats { display: flex; flex-wrap: wrap; gap: 6px; font: 500 13px/1.2 var(--font-ui); color: var(--text-muted); font-variant-numeric: tabular-nums; }
	.wh-status { color: var(--text); font-weight: 600; }
	.wh-status.done { color: #4ade80; }
	.wh-tags { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; }
	.wh-tag { padding: 2px 8px; border-radius: 6px; background: var(--surface-2); font: 600 11.5px/1.3 var(--font-ui); color: var(--text); }
	.wh-rel { font: 500 12px/1.2 var(--font-ui); color: var(--text-faint); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 320px; }

	.wh-acts { display: flex; flex-wrap: wrap; gap: 6px; flex: none; }
	.wh-btn {
		display: inline-flex; align-items: center; gap: 6px; height: 34px; padding: 0 12px;
		border: 1px solid var(--line); border-radius: 9px; background: transparent; color: var(--text);
		text-decoration: none; font: 600 12.5px/1 var(--font-ui); cursor: pointer; white-space: nowrap;
	}
	.wh-btn:hover { background: var(--surface-2); border-color: var(--line-strong); }
	.wh-btn-primary { background: var(--text); color: var(--bg); border-color: var(--text); }
	.wh-btn-primary:hover { opacity: 0.9; background: var(--text); }
	.wh-btn-danger { color: var(--fail); }

	@media (max-width: 820px) {
		.wh-row { flex-direction: column; align-items: stretch; }
		.wh-thumb { width: 100%; }
	}
</style>
