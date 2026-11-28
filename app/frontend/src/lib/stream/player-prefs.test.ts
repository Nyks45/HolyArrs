import { expect, test } from 'vitest';
import { resolveScopedSubtitleStyle, subtitleScopeKey } from './player-prefs';
import { readPlayerShortcut } from './player-shortcuts';
import { screenshotName } from './screenshot';

test('title and episode subtitle preferences retain inherited named-style settings', () => {
	const ctx = { kind: 'show' as const, tmdb: '10', season: 2, episode: 4 };
	const settings = {
		global: { scale: 130, offset: 25, styles: { Signs: { hidden: false, font: 'Custom', scale: 80 } } },
		[subtitleScopeKey('title', ctx)]: { offset: 40, styles: { Signs: { scale: 50 } } },
		[subtitleScopeKey('episode', ctx)]: { scale: 90, styles: { Signs: { hidden: true } } }
	};
	expect(resolveScopedSubtitleStyle(settings, ctx)).toEqual({ scale: 90, offset: 40, styles: { Signs: { font: 'Custom', scale: 50, hidden: true } } });
	expect(resolveScopedSubtitleStyle(settings, { ...ctx, episode: 5 }).scale).toBe(130);
	expect(resolveScopedSubtitleStyle(settings, { ...ctx, kind: 'movie' }).offset).toBe(25);
});

test('keyboard modifiers select the four screenshot modes', () => {
	const event = (fields: Partial<KeyboardEvent>) => ({ key: 's', ...fields } as KeyboardEvent);
	expect(readPlayerShortcut(event({}), true)).toBe('screenshot');
	expect(readPlayerShortcut(event({ shiftKey: true }), true)).toBe('screenshotSubs');
	expect(readPlayerShortcut(event({ altKey: true }), true)).toBe('screenshotRaw');
	expect(readPlayerShortcut(event({ altKey: true, shiftKey: true }), true)).toBe('screenshotRawSubs');
	expect(readPlayerShortcut(event({ ctrlKey: true }), true)).toBeNull();
	expect(readPlayerShortcut(event({ key: 'e' }), false)).toBeNull();
	expect(readPlayerShortcut(event({ key: '7' }), true)).toBe('jump7');
	expect(screenshotName('Good: Title', 75.8, 2, 3)).toBe('Good__Title_S02E03_01m15s.png');
});
