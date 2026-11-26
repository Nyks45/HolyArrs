import type { SubStylePrefs } from './streaming-config-api.ts';

export interface ResolvedSubtitleStyle {
	color: string;
	background: string;
	fontSizeRem: number;
	bottomPct: number;
	fontFamilyCss: string;
}

const RE_BRACKET_SDH = /\[[^\]]*\]/g;
const RE_PAREN_SDH = /\([^)]*\)/g;
const RE_NOTE_SPAN = /[♪♫][^♪♫\n]*[♪♫]?/g;
const RE_SPEAKER_LABEL = /^(\s*-\s*)?[A-Z][A-Z0-9 .'-]{1,24}:\s*/;

export function stripSdhCueLines(lines: string[]): string[] {
	const out: string[] = [];
	for (const raw of lines) {
		let line = raw
			.replace(RE_BRACKET_SDH, ' ')
			.replace(RE_PAREN_SDH, ' ')
			.replace(RE_NOTE_SPAN, ' ')
			.replace(RE_SPEAKER_LABEL, (_, dash: string | undefined) => (dash ? '- ' : ''))
			.replace(/\s{2,}/g, ' ')
			.trim();
		if (!line || line === '-' || line === '--') continue;
		out.push(line);
	}
	return out;
}

function parseHexRGB(hex: string): [number, number, number] {
	let clean = hex.trim().replace(/^#/, '');
	if (clean.length === 3) {
		clean = clean
			.split('')
			.map((ch) => ch + ch)
			.join('');
	}
	if (!/^[0-9a-fA-F]{6}$/.test(clean)) return [255, 255, 255];
	return [
		parseInt(clean.slice(0, 2), 16),
		parseInt(clean.slice(2, 4), 16),
		parseInt(clean.slice(4, 6), 16)
	];
}

export function scaleHexBrightness(hex: string, brightnessPct: number): string {
	const clamped = Math.min(100, Math.max(10, Number.isFinite(brightnessPct) ? brightnessPct : 40));
	const factor = (0.7 * clamped + 30) / 100;
	const [r, g, b] = parseHexRGB(hex);
	const toHex = (v: number) => Math.min(255, Math.max(0, Math.round(v * factor))).toString(16).padStart(2, '0');
	return `#${toHex(r)}${toHex(g)}${toHex(b)}`;
}

export function hexToRgba(hex: string, alpha: number): string {
	const a = Math.min(1, Math.max(0, Number.isFinite(alpha) ? alpha : 0.65));
	const [r, g, b] = parseHexRGB(hex || '#000000');
	return `rgba(${r}, ${g}, ${b}, ${a.toFixed(2)})`;
}

export function resolveSubtitleStyle(prefs: SubStylePrefs): ResolvedSubtitleStyle {
	const rawColor = prefs.color || '#ffffff';
	const color = prefs.hdr_dimmed ? scaleHexBrightness(rawColor, prefs.hdr_brightness) : rawColor;
	const background = hexToRgba(prefs.bg_color || '#000000', prefs.bg_opacity);
	const fontSizeRem = Math.min(2.5, Math.max(0.5, prefs.size || 1)) * 1.12;
	const bottomPct = Math.min(35, Math.max(0, prefs.offset_pct ?? 6));
	let fontFamilyCss = 'var(--font-ui), system-ui, sans-serif';
	if (prefs.font_family === 'serif') fontFamilyCss = 'Georgia, Cambria, "Times New Roman", serif';
	else if (prefs.font_family === 'mono') fontFamilyCss = 'ui-monospace, SFMono-Regular, Menlo, monospace';
	return { color, background, fontSizeRem, bottomPct, fontFamilyCss };
}
