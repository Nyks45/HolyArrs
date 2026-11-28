import { activeAssCues, assDefaultStyle, type AssCue, type AssDocument, type AssTextStyle } from './ass-parser';
import type { ScopedSubtitleStyle, VideoFit } from './player-prefs';

export interface VideoRect { x: number; y: number; width: number; height: number }

export function videoRect(width: number, height: number, sourceWidth: number, sourceHeight: number, fit: VideoFit): VideoRect {
	if (fit === 'fill' || !sourceWidth || !sourceHeight) return { x: 0, y: 0, width, height };
	const factor = fit === 'cover' ? Math.max(width / sourceWidth, height / sourceHeight) : Math.min(width / sourceWidth, height / sourceHeight);
	const w = sourceWidth * factor, h = sourceHeight * factor;
	return { x: (width - w) / 2, y: (height - h) / 2, width: w, height: h };
}

type Metrics = { text: string; width: number; font: string; size: number; style: AssTextStyle; stretch: number };
type Row = { runs: Metrics[]; width: number; height: number };

function safeFont(name: string): string {
	if (['sans-serif', 'serif', 'monospace', 'system-ui'].includes(name)) return name;
	return `"${name.replace(/["\\\r\n]/g, '')}", sans-serif`;
}

function measureRows(ctx: CanvasRenderingContext2D, cue: AssCue, base: AssTextStyle, scale: number, maxWidth: number, noWrap: boolean): Row[] {
	const rows: Row[] = [];
	let row: Row = { runs: [], width: 0, height: 0 };
	function next() {
		rows.push(row);
		row = { runs: [], width: 0, height: 0 };
	}
	for (const run of cue.runs) {
		const style = { ...base, ...run.style };
		const fontSize = Math.max(1, Math.min(300, (style.size ?? 22) * scale * (style.scaleY ?? 100) / 100));
		const font = `${style.italic ? 'italic ' : ''}${style.bold ? '700 ' : '400 '}${fontSize}px ${safeFont(style.font ?? 'sans-serif')}`;
		const stretch = Math.max(0.01, Math.min(10, (style.scaleX ?? 100) / Math.max(1, style.scaleY ?? 100)));
		ctx.font = font;
		ctx.letterSpacing = `${(style.spacing ?? 0) * scale}px`;
		for (const text of run.text.split(/(\n|[ \t]+)/)) {
			if (text === '\n') { next(); continue; }
			if (!text) continue;
			const width = ctx.measureText(text).width * stretch;
			if (!noWrap && row.runs.length && row.width + width > maxWidth && text.trim()) next();
			if (!row.runs.length && !text.trim()) continue;
			row.runs.push({ text, width, font, size: fontSize, style, stretch });
			row.width += width;
			row.height = Math.max(row.height, fontSize * 1.2);
		}
	}
	if (row.runs.length) next();
	return rows;
}

function drawRun(ctx: CanvasRenderingContext2D, run: Metrics, x: number, y: number, scale: number, opacity: number): void {
	const { style, text, size, font, stretch } = run;
	ctx.save();
	ctx.translate(x, y);
	ctx.scale(stretch, 1);
	ctx.font = font;
	ctx.letterSpacing = `${(style.spacing ?? 0) * scale}px`;
	ctx.textBaseline = 'alphabetic';
	ctx.lineJoin = 'round';
	ctx.globalAlpha = opacity * (style.outlineAlpha ?? 1);
	if (style.blur) ctx.filter = `blur(${Math.min(30, style.blur * scale)}px)`;
	const outline = Math.max(0, Math.min(30, (style.outline ?? 0) * scale));
	const shadow = Math.max(0, Math.min(30, (style.shadow ?? 0) * scale));
	if (style.border === 3) {
		ctx.fillStyle = style.shadowColor ?? '#000';
		ctx.fillRect(-outline, -size, run.width / stretch + outline * 2, size * 1.22 + outline);
	} else {
		if (shadow) {
			ctx.fillStyle = style.shadowColor ?? '#000';
			ctx.fillText(text, shadow, shadow);
		}
		if (outline) {
			ctx.lineWidth = outline * 2;
			ctx.strokeStyle = style.outlineColor ?? '#000';
			ctx.strokeText(text, 0, 0);
		}
	}
	ctx.globalAlpha = opacity * (style.alpha ?? 1);
	ctx.fillStyle = style.primary ?? '#fff';
	ctx.fillText(text, 0, 0);
	if (style.underline) ctx.fillRect(0, size * 0.1, run.width / stretch, Math.max(1, size / 18));
	if (style.strike) ctx.fillRect(0, -size * 0.32, run.width / stretch, Math.max(1, size / 18));
	ctx.restore();
}

export function drawAssTrack(ctx: CanvasRenderingContext2D, doc: AssDocument, time: number, box: VideoRect, scoped: Required<ScopedSubtitleStyle>, bottomLift: number, overlap: boolean, secondary = false): void {
	const sx = box.width / doc.width;
	const sy = box.height / doc.height;
	const fontScale = sy * scoped.scale / 100;
	let occupied = 0;
	for (const cue of activeAssCues(doc, time)) {
		const source = doc.styles.get(cue.style) ?? doc.styles.get('Default') ?? assDefaultStyle;
		const override = scoped.styles[cue.style];
		if (override?.hidden) continue;
		const base = { ...source, ...(override?.font ? { font: override.font } : {}) };
		const scale = fontScale * (override?.scale ?? 100) / 100;
		const left = (cue.marginL || base.marginL) * sx;
		const right = (cue.marginR || base.marginR) * sx;
		const vertical = (cue.marginV || base.marginV) * sy;
		const rows = measureRows(ctx, cue, base, scale, Math.max(1, box.width - left - right), doc.wrapStyle === 2);
		if (!rows.length) continue;
		const totalHeight = rows.reduce((sum, row) => sum + (row.height || base.size * scale), 0);
		const align = Math.max(1, Math.min(9, cue.alignment || base.alignment));
		const horizontal = (align - 1) % 3;
		const v = Math.floor((align - 1) / 3);
		const x = cue.position ? box.x + cue.position[0] * sx : box.x + [left, box.width / 2, box.width - right][horizontal];
		const y = cue.position ? box.y + cue.position[1] * sy : box.y + [box.height - vertical - bottomLift - (overlap ? occupied : 0), box.height / 2, vertical][secondary && v === 0 ? 2 : v];
		let currentY = y - [totalHeight, totalHeight / 2, 0][v] - scoped.offset;
		if (secondary && !cue.position && v === 0) currentY = y - scoped.offset;
		const fadeIn = cue.fade?.[0] ? (time - cue.start) * 1000 / cue.fade[0] : 1;
		const fadeOut = cue.fade?.[1] ? (cue.end - time) * 1000 / cue.fade[1] : 1;
		const opacity = Math.max(0, Math.min(1, fadeIn, fadeOut));
		for (const row of rows) {
			const height = row.height || base.size * scale;
			let cursor = x - [0, row.width / 2, row.width][horizontal];
			for (const run of row.runs) {
				drawRun(ctx, run, cursor, currentY + height * 0.82, doc.scaleBorders ? scale : 1, opacity);
				cursor += run.width;
			}
			currentY += height;
		}
		if (!cue.position && v === 0) occupied += totalHeight;
	}
}
