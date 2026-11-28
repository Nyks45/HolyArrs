<!-- Compact audio popover above the HUD: track choice and volume boost. -->
<script lang="ts">
	import Icon from '#lib/ui/Icon.svelte';
	import PlayerIcons from './PlayerIcons.svelte';
	import type { AudioTrack } from './stream-api.ts';

	let { tracks, audioIdx, canSwitch, boost, onChangeAudio, onBoost, onClose }: {
		tracks: AudioTrack[]; audioIdx: number; canSwitch: boolean; boost: number;
		onChangeAudio: (idx: number) => void; onBoost: (percent: number) => void; onClose: () => void;
	} = $props();

	const label = (tr: AudioTrack) => [tr.title || (tr.language ? new Intl.DisplayNames(['en'], { type: 'language' }).of(tr.language) ?? tr.language.toUpperCase() : ''), tr.channels && tr.channels > 2 ? `${tr.channels}ch` : ''].filter(Boolean).join(' · ') || `Track ${tr.index + 1}`;
	const safeLabel = (tr: AudioTrack) => { try { return label(tr); } catch { return tr.language?.toUpperCase() || `Track ${tr.index + 1}`; } };
</script>

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') onClose(); }} />
<div class="pop" role="dialog" aria-label="Audio">
	<div class="head">Audio Track</div>
	<div class="list">
		{#each tracks.length ? tracks : [{ index: 0, stream_index: 0, title: 'Default' }] as tr (tr.index)}
			<button type="button" class="row" class:on={audioIdx === tr.index} disabled={!canSwitch && audioIdx !== tr.index} onclick={() => onChangeAudio(tr.index)}>
				<span class="tick">{#if audioIdx === tr.index}<Icon name="check" size={14} />{/if}</span>{safeLabel(tr)}
			</button>
		{/each}
	</div>
	<div class="boost">
		<div class="boost-head"><span>Volume Boost</span><span>{boost > 100 ? `${boost}%` : 'Off'}</span></div>
		<div class="boost-row">
			<PlayerIcons name="mute" size={15} />
			<input type="range" min="100" max="300" step="5" value={boost} aria-label="Volume boost" oninput={(e) => onBoost(Number(e.currentTarget.value))} />
			<PlayerIcons name="volume" size={15} />
		</div>
	</div>
</div>

<style>
	.pop { position: absolute; z-index: 20; right: 142px; bottom: 122px; width: 240px; padding-bottom: 6px; background: rgb(24 24 27 / 97%); border: 1px solid rgb(255 255 255 / 10%); border-radius: 12px; box-shadow: 0 18px 50px rgb(0 0 0 / 55%); backdrop-filter: blur(20px); color: #fff; font-size: 13px; }
	.head { padding: 14px 14px 12px; font-weight: 700; border-bottom: 1px solid rgb(255 255 255 / 8%); }
	.list { padding: 4px 0; max-height: 220px; overflow: auto; border-bottom: 1px solid rgb(255 255 255 / 8%); }
	.row { display: flex; align-items: center; gap: 8px; width: 100%; padding: 9px 14px; border: 0; background: transparent; color: rgb(255 255 255 / 85%); font: inherit; text-align: left; cursor: pointer; }
	.row:hover:not(:disabled) { background: rgb(255 255 255 / 6%); }
	.row.on { color: #fff; }
	.row:disabled { opacity: .45; cursor: default; }
	.tick { width: 14px; display: grid; place-items: center; }
	.boost { padding: 12px 14px 6px; }
	.boost-head { display: flex; justify-content: space-between; font-size: 12px; color: rgb(255 255 255 / 60%); }
	.boost-row { display: flex; align-items: center; gap: 10px; margin-top: 10px; color: rgb(255 255 255 / 55%); }
	.boost-row input { flex: 1; accent-color: #fff; }
	@media (max-width: 720px) { .pop { right: 8px; bottom: 96px; } }
</style>
