export interface AssStyle {
	name: string;
	font: string;
	size: number;
	primary: string;
	secondary: string;
	outlineColor: string;
	shadowColor: string;
	bold: boolean;
	italic: boolean;
	underline: boolean;
	strike: boolean;
	scaleX: number;
	scaleY: number;
	spacing: number;
	border: number;
	outline: number;
	shadow: number;
	alignment: number;
	marginL: number;
	marginR: number;
	marginV: number;
}

export type AssTextStyle = Partial<AssStyle> & { blur?: number; alpha?: number; outlineAlpha?: number };
export type AssRun = { text: string; style: AssTextStyle };
export interface AssCue {
	start: number;
	end: number;
	layer: number;
	style: string;
	marginL: number;
	marginR: number;
	marginV: number;
	alignment?: number;
	position?: [number, number];
	fade?: [number, number];
	runs: AssRun[];
}
export interface AssDocument {
	width: number;
	height: number;
	wrapStyle: number;
	scaleBorders: boolean;
	styles: Map<string, AssStyle>;
	cues: AssCue[];
	ends: number[];
}

export const assDefaultStyle: AssStyle = {
	name: 'Default', font: 'sans-serif', size: 22, primary: '#fff', secondary: '#fff',
	outlineColor: '#000', shadowColor: '#000', bold: false, italic: false, underline: false, strike: false,
	scaleX: 100, scaleY: 100, spacing: 0, border: 1, outline: 2, shadow: 2, alignment: 2,
	marginL: 10, marginR: 10, marginV: 10
};

function number(raw: string | undefined, fallback: number): number {
	const value = Number(raw);
	return raw?.trim() && Number.isFinite(value) ? value : fallback;
}

export function parseAssColor(raw: string | undefined, fallback: string): string {
	if (!raw) return fallback;
	const value = raw.trim();
	const hex = /^&?H([\da-f]{1,8})&?$/i.exec(value);
	const color = hex ? Number.parseInt(hex[1], 16) : /^-?\d+$/.test(value) ? Number(value) : NaN;
	if (!Number.isFinite(color)) return fallback;
	return `rgba(${color & 255}, ${(color >>> 8) & 255}, ${(color >>> 16) & 255}, ${1 - ((color >>> 24) & 255) / 255})`;
}

function clock(raw: string | undefined): number {
	const parts = /^(\d+):(\d\d):(\d\d(?:\.\d+)?)$/.exec(raw?.trim() ?? '');
	if (!parts || Number(parts[2]) >= 60 || Number(parts[3]) >= 60) return NaN;
	return Number(parts[1]) * 3600 + Number(parts[2]) * 60 + Number(parts[3]);
}

const v4plus = 'name,fontname,fontsize,primarycolour,secondarycolour,outlinecolour,backcolour,bold,italic,underline,strikeout,scalex,scaley,spacing,angle,borderstyle,outline,shadow,alignment,marginl,marginr,marginv,encoding'.split(',');
const legacyStyles = 'name,fontname,fontsize,primarycolour,secondarycolour,tertiarycolour,backcolour,bold,italic,borderstyle,outline,shadow,alignment,marginl,marginr,marginv,alphalevel,encoding'.split(',');
const v4events = 'layer,start,end,style,name,marginl,marginr,marginv,effect,text'.split(',');

function fields(value: string, format: string[]): Record<string, string> {
	const parts = value.split(',');
	if (parts.length > format.length) parts.splice(format.length - 1, parts.length, parts.slice(format.length - 1).join(','));
	return Object.fromEntries(format.map((name, i) => [name, name === 'text' ? (parts[i] ?? '') : (parts[i] ?? '').trim()]));
}

function readStyle(values: Record<string, string>, legacy: boolean): AssStyle {
	const d = assDefaultStyle;
	const n = (key: string, fallback: number) => number(values[key], fallback);
	const oldAlignment: Record<number, number> = { 1: 1, 2: 2, 3: 3, 5: 7, 6: 8, 7: 9, 9: 4, 10: 5, 11: 6 };
	const alignment = n('alignment', d.alignment);
	return {
		name: values.name || d.name, font: values.fontname || d.font, size: Math.max(1, n('fontsize', d.size)),
		primary: parseAssColor(values.primarycolour, d.primary), secondary: parseAssColor(values.secondarycolour, d.secondary),
		outlineColor: parseAssColor(values.outlinecolour || values.tertiarycolour, d.outlineColor), shadowColor: parseAssColor(values.backcolour, d.shadowColor),
		bold: n('bold', 0) !== 0, italic: n('italic', 0) !== 0, underline: n('underline', 0) !== 0, strike: n('strikeout', 0) !== 0,
		scaleX: n('scalex', d.scaleX), scaleY: n('scaley', d.scaleY), spacing: n('spacing', d.spacing), border: n('borderstyle', d.border),
		outline: n('outline', d.outline), shadow: n('shadow', d.shadow), alignment: legacy ? oldAlignment[alignment] ?? 2 : alignment,
		marginL: n('marginl', d.marginL), marginR: n('marginr', d.marginR), marginV: n('marginv', d.marginV)
	};
}

function coord(raw: string, min: number, max?: number): [number, number] | undefined {
	const match = /^\(\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*\)$/.exec(raw);
	if (!match) return;
	const x = Number(match[1]), y = Number(match[2]);
	if (x < min || y < min || (max !== undefined && (x > max || y > max))) return;
	return [x, y];
}

function alpha(value: string): number | undefined {
	const match = /^&H([\da-f]{1,2})&?$/i.exec(value.trim());
	return match ? 1 - Number.parseInt(match[1], 16) / 255 : undefined;
}

function readRuns(text: string, cue: AssCue, styles: Map<string, AssStyle>, wrapStyle: number): AssRun[] {
	let style: AssTextStyle = {};
	let drawing = false;
	const runs: AssRun[] = [];
	for (const part of text.split(/(\{[^}]*\})/)) {
		if (part.startsWith('{')) {
			for (const match of part.slice(1, -1).matchAll(/\\(alpha|bord|blur|shad|pos|fad|fscx|fscy|fsp|fn|fs|an|[13]a|[13]c|[bcisurp])([^\\]*)/gi)) {
				const key = match[1].toLowerCase(), value = match[2].trim();
				if (/^[bcisup]$/.test(key) && /^[a-z]/i.test(value)) continue;
				switch (key) {
					case 'r': style = { ...styles.get(value) }; drawing = false; break;
					case 'p': drawing = number(value, 0) > 0; break;
					case 'fn': style.font = value || undefined; break;
					case 'fs': style.size = Math.max(1, number(value, assDefaultStyle.size)); break;
					case 'fscx': style.scaleX = number(value, 100); break;
					case 'fscy': style.scaleY = number(value, 100); break;
					case 'fsp': style.spacing = number(value, 0); break;
					case 'b': style.bold = number(value, 0) !== 0; break;
					case 'i': style.italic = number(value, 0) !== 0; break;
					case 'u': style.underline = number(value, 0) !== 0; break;
					case 's': style.strike = number(value, 0) !== 0; break;
					case 'c': case '1c': style.primary = parseAssColor(value, styles.get(cue.style)?.primary ?? assDefaultStyle.primary); break;
					case '3c': style.outlineColor = parseAssColor(value, styles.get(cue.style)?.outlineColor ?? assDefaultStyle.outlineColor); break;
					case 'alpha': style.alpha = alpha(value); style.outlineAlpha = style.alpha; break;
					case '1a': style.alpha = alpha(value); break;
					case '3a': style.outlineAlpha = alpha(value); break;
					case 'bord': style.outline = Math.max(0, number(value, 0)); break;
					case 'shad': style.shadow = Math.max(0, number(value, 0)); break;
					case 'blur': style.blur = Math.max(0, number(value, 0)); break;
					case 'an': if (/^[1-9]$/.test(value)) cue.alignment ??= Number(value); break;
					case 'pos': cue.position ??= coord(value, -100000, 100000); break;
					case 'fad': cue.fade ??= coord(value, 0, 1_000_000); break;
				}
			}
		} else if (part && !drawing) {
			runs.push({ text: part.replaceAll('\\N', '\n').replaceAll('\\n', wrapStyle === 2 ? '\n' : ' ').replaceAll('\\h', '\u00a0'), style: { ...style } });
		}
	}
	return runs;
}

export function parseAssDocument(raw: string): AssDocument | null {
	const doc: AssDocument = { width: 0, height: 0, wrapStyle: 0, scaleBorders: true, styles: new Map(), cues: [], ends: [] };
	let section = '';
	let styleFormat = v4plus;
	let eventFormat = v4events;
	for (const line of raw.replace(/^\ufeff/, '').split(/\r?\n/)) {
		const header = /^\s*\[([^\]]+)\]/.exec(line);
		if (header) {
			section = header[1].toLowerCase();
			if (section === 'v4 styles') styleFormat = legacyStyles;
			continue;
		}
		const field = /^\s*([\w ]+):\s*(.*)$/.exec(line);
		if (!field) continue;
		const key = field[1].trim().toLowerCase(), value = field[2];
		if (section === 'script info') {
			if (key === 'playresx') doc.width = number(value, 0);
			if (key === 'playresy') doc.height = number(value, 0);
			if (key === 'wrapstyle') doc.wrapStyle = number(value, 0);
			if (key === 'scaledborderandshadow') doc.scaleBorders = value.trim().toLowerCase() !== 'no';
		} else if (section === 'v4+ styles' || section === 'v4 styles') {
			if (key === 'format') styleFormat = value.toLowerCase().split(',').map((f) => f.trim());
			else if (key === 'style') {
				const style = readStyle(fields(value, styleFormat), section === 'v4 styles');
				doc.styles.set(style.name, style);
			}
		} else if (section === 'events') {
			if (key === 'format') eventFormat = value.toLowerCase().split(',').map((f) => f.trim());
			else if (key === 'dialogue') {
				const f = fields(value, eventFormat), start = clock(f.start), end = clock(f.end);
				if (!Number.isFinite(start) || !(end > start)) continue;
				const cue: AssCue = { start, end, layer: number(f.layer || f.marked?.replace(/^[^=]*=/, ''), 0), style: f.style || 'Default', marginL: number(f.marginl, 0), marginR: number(f.marginr, 0), marginV: number(f.marginv, 0), runs: [] };
				cue.runs = readRuns(f.text, cue, doc.styles, doc.wrapStyle);
				if (cue.runs.length) doc.cues.push(cue);
			}
		}
	}
	if (!doc.cues.length) return null;
	doc.height = doc.height > 0 ? doc.height : doc.width > 0 ? doc.width * 0.75 : 288;
	doc.width = doc.width > 0 ? doc.width : doc.height * 4 / 3;
	doc.cues.sort((a, b) => a.start - b.start);
	if (!doc.styles.has('Default')) doc.styles.set('Default', assDefaultStyle);
	for (const cue of doc.cues) doc.ends.push(Math.max(doc.ends.at(-1) ?? 0, cue.end));
	return doc;
}

export function activeAssCues(doc: AssDocument, time: number): AssCue[] {
	let low = 0, high = doc.cues.length;
	while (low < high) {
		const mid = (low + high) >>> 1;
		if (doc.cues[mid].start <= time) low = mid + 1;
		else high = mid;
	}
	const active: AssCue[] = [];
	for (let i = low - 1; i >= 0 && doc.ends[i] > time; i--) {
		if (doc.cues[i].end > time) active.push(doc.cues[i]);
	}
	return active.sort((a, b) => a.layer - b.layer || a.start - b.start);
}
