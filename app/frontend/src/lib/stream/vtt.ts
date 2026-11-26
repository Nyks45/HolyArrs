// Lightweight WebVTT parser and cue matcher for custom Netflix-style captions.

export interface VTTCue {
	start: number;
	end: number;
	lines: string[];
}

function parseTimestamp(raw: string): number {
	const s = raw.trim().replace(',', '.');
	const parts = s.split(':');
	if (parts.length === 2) {
		const m = Number(parts[0]);
		const sec = Number(parts[1]);
		return Number.isFinite(m) && Number.isFinite(sec) ? m * 60 + sec : NaN;
	}
	if (parts.length === 3) {
		const h = Number(parts[0]);
		const m = Number(parts[1]);
		const sec = Number(parts[2]);
		return Number.isFinite(h) && Number.isFinite(m) && Number.isFinite(sec) ? h * 3600 + m * 60 + sec : NaN;
	}
	return NaN;
}

function cleanCueLine(line: string): string {
	return line
		.replace(/<\/?(?:b|i|u|c(?:\.[^>\s]+)?|ruby|rt|lang)[^>]*>/gi, '')
		.replace(/<[^>]+>/g, '')
		.trim();
}

export function parseWebVTT(raw: string): VTTCue[] {
	if (!raw) return [];
	const blocks = raw.replace(/\r\n?/g, '\n').split(/\n{2,}/);
	const cues: VTTCue[] = [];

	for (const block of blocks) {
		const rawLines = block.split('\n');
		const timingIdx = rawLines.findIndex((l) => l.includes('-->'));
		if (timingIdx < 0) continue;

		const timingLine = rawLines[timingIdx];
		const arrowIdx = timingLine.indexOf('-->');
		const start = parseTimestamp(timingLine.slice(0, arrowIdx));
		const endToken = timingLine.slice(arrowIdx + 3).trim().split(/\s+/)[0] ?? '';
		const end = parseTimestamp(endToken);
		if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) continue;

		const lines = rawLines
			.slice(timingIdx + 1)
			.map(cleanCueLine)
			.filter(Boolean);
		if (lines.length === 0) continue;

		cues.push({ start, end, lines });
	}

	cues.sort((a, b) => a.start - b.start);
	return cues;
}

export function activeCuesAt(cues: VTTCue[], timeSec: number, delayMS = 0): VTTCue[] {
	if (!cues.length || !Number.isFinite(timeSec)) return [];
	// Positive delayMS pushes subtitles later in playback (match earlier cue time).
	const adjusted = timeSec - delayMS / 1000;
	if (adjusted < 0) return [];
	return cues.filter((c) => adjusted >= c.start && adjusted <= c.end);
}
