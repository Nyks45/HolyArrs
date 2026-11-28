<!-- Thin stroke player icons in the HUD style. -->
<script lang="ts" module>
	export type PlayerIconName =
		| 'back' | 'replay10' | 'forward10' | 'play' | 'pause' | 'next' | 'volume' | 'volumeLow' | 'mute'
		| 'captions' | 'audio' | 'settings' | 'more' | 'theater' | 'fullscreen' | 'exitFullscreen' | 'episodes';
</script>

<script lang="ts">
	let { name, size = 24 }: { name: PlayerIconName; size?: number } = $props();
</script>

<svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
	{#if name === 'back'}
		<path d="m12 19-7-7 7-7" /><path d="M19 12H5" />
	{:else if name === 'replay10'}
		<path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" /><path d="M3 3v5h5" />
	{:else if name === 'forward10'}
		<path d="M21 12a9 9 0 1 1-9-9c2.52 0 4.93 1 6.74 2.74L21 8" /><path d="M21 3v5h-5" />
	{:else if name === 'play'}
		<path d="M6 4.8a1 1 0 0 1 1.5-.86l11.1 6.4a1 1 0 0 1 0 1.72l-11.1 6.4A1 1 0 0 1 6 17.6z" />
	{:else if name === 'pause'}
		<rect x="6" y="4.5" width="3.5" height="15" rx="1" /><rect x="14.5" y="4.5" width="3.5" height="15" rx="1" />
	{:else if name === 'next'}
		<path d="M5 4.8a1 1 0 0 1 1.5-.86l9.5 6.4a1 1 0 0 1 0 1.72l-9.5 6.4A1 1 0 0 1 5 17.6z" /><path d="M19.5 4.5v15" />
	{:else if name === 'volume' || name === 'volumeLow' || name === 'mute'}
		<path d="M11 4.7a.7.7 0 0 0-1.2-.5L6.4 7.6A1.4 1.4 0 0 1 5.4 8H3a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2.4a1.4 1.4 0 0 1 1 .4l3.4 3.4a.7.7 0 0 0 1.2-.5z" />
		{#if name === 'mute'}<path d="m22 9-6 6" /><path d="m16 9 6 6" />{:else}<path d="M16 9a5 5 0 0 1 0 6" />{/if}
		{#if name === 'volume'}<path d="M19.4 18.4a9 9 0 0 0 0-12.8" />{/if}
	{:else if name === 'captions'}
		<rect x="3" y="5" width="18" height="14" rx="2" /><path d="M7 15h4" /><path d="M15 15h2" /><path d="M7 11h2" /><path d="M13 11h4" />
	{:else if name === 'audio'}
		<path d="M2 10v3" /><path d="M6 6v11" /><path d="M10 3v18" /><path d="M14 8v7" /><path d="M18 5v13" /><path d="M22 10v3" />
	{:else if name === 'settings'}
		<path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" /><circle cx="12" cy="12" r="3" />
	{:else if name === 'more'}
		<circle cx="5" cy="12" r="1.4" fill="currentColor" /><circle cx="12" cy="12" r="1.4" fill="currentColor" /><circle cx="19" cy="12" r="1.4" fill="currentColor" />
	{:else if name === 'theater'}
		<rect x="3" y="3" width="18" height="18" rx="2" /><path d="M15 3v18" /><path d="m8 9 3 3-3 3" />
	{:else if name === 'fullscreen'}
		<path d="M8 3H5a2 2 0 0 0-2 2v3" /><path d="M21 8V5a2 2 0 0 0-2-2h-3" /><path d="M3 16v3a2 2 0 0 0 2 2h3" /><path d="M16 21h3a2 2 0 0 0 2-2v-3" />
	{:else if name === 'exitFullscreen'}
		<path d="M8 3v3a2 2 0 0 1-2 2H3" /><path d="M21 8h-3a2 2 0 0 1-2-2V3" /><path d="M3 16h3a2 2 0 0 1 2 2v3" /><path d="M16 21v-3a2 2 0 0 1 2-2h3" />
	{:else if name === 'episodes'}
		<path d="M12 12H3" /><path d="M16 6H3" /><path d="M12 18H3" /><path d="m16 12 5 3-5 3z" />
	{/if}
</svg>
