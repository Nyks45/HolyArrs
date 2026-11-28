export type PlayerAction = 'togglePlay' | 'back5' | 'fwd5' | 'back10' | 'fwd10' | 'volumeUp' | 'volumeDown' | 'start' | 'end' | 'mute' | 'fullscreen' | 'immersive' | 'next' | 'previous' | 'subtitles' | 'stats' | 'hide' | 'pip' | 'shortcuts' | 'episodes' | 'sources' | 'closeDrawer' | 'screenshot' | 'screenshotSubs' | 'screenshotRaw' | 'screenshotRawSubs' | `jump${number}`;

const shortcuts: Record<string, PlayerAction> = {
	' ': 'togglePlay', k: 'togglePlay', arrowleft: 'back5', arrowright: 'fwd5', j: 'back10', l: 'fwd10',
	arrowup: 'volumeUp', arrowdown: 'volumeDown', home: 'start', end: 'end', m: 'mute', f: 'fullscreen',
	t: 'immersive', n: 'next', b: 'previous', c: 'subtitles', d: 'stats', h: 'hide', i: 'pip', p: 'pip',
	'/': 'shortcuts', e: 'episodes', o: 'sources', q: 'sources'
};

export const shortcutHelp = [
	['Space / K', 'Play or pause'], ['← / →', 'Seek 5 seconds'], ['J / L', 'Seek 10 seconds'],
	['↑ / ↓', 'Adjust volume'], ['0–9', 'Jump by percentage'], ['Home / End', 'Start or near end'],
	['M', 'Mute'], ['F', 'Fullscreen'], ['T', 'Immersive mode'], ['N / B', 'Next or previous episode'],
	['C', 'Captions'], ['D', 'Video stats'], ['H', 'Hide controls'], ['I / P', 'Picture in picture'],
	['E', 'Episodes'], ['O / Q', 'Sources'], ['/', 'Keyboard shortcuts'], ['Esc', 'Close panel'],
	['S', 'Screenshot'], ['Shift + S', 'Screenshot with subtitles'], ['Alt + S', 'Raw screenshot'],
	['Alt + Shift + S', 'Raw screenshot with subtitles']
];

export function readPlayerShortcut(event: KeyboardEvent, isShow: boolean): PlayerAction | null {
	if (event.defaultPrevented || event.ctrlKey || event.metaKey || event.isComposing) return null;
	const key = event.key.toLowerCase();
	if (key === 'escape') return 'closeDrawer';
	const target = event.target;
	if (typeof Element !== 'undefined' && target instanceof Element) {
		if (target.closest('input, textarea, select, [contenteditable]:not([contenteditable="false"]), [role="slider"], [role="spinbutton"]')) return null;
		if (key === ' ' && target.closest('button, a')) return null;
	}
	if (key === 's') {
		if (event.repeat) return null;
		if (event.altKey) return event.shiftKey ? 'screenshotRawSubs' : 'screenshotRaw';
		return event.shiftKey ? 'screenshotSubs' : 'screenshot';
	}
	if (event.altKey || event.shiftKey) return null;
	if (/^[0-9]$/.test(key)) return `jump${Number(key)}`;
	const action = shortcuts[key];
	if (!action || (!isShow && (action === 'episodes' || action === 'next' || action === 'previous'))) return null;
	return action;
}
