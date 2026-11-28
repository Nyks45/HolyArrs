import { describe, expect, test } from 'vitest';
import { activeAssCues, parseAssColor, parseAssDocument } from './ass-parser';

const example = String.raw`[Script Info]
PlayResX: 1280
PlayResY: 720
WrapStyle: 2
[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, OutlineColour, Bold, Alignment
Style: Signs, Sans, 30, &H800000FF, &H0000FF00, -1, 8
[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 2,0:00:02.20,0:00:10.00,Signs,,0,0,0,,{\an7\pos(30,100)\fad(150,200)\b1\c&HFF0000&}Left, sign\N{\i1\clip(0,0,100,100)}Second\hline
Dialogue: 0,0:00:05.00,0:00:08.00,Default,,0,0,0,,Line
Dialogue: 3,0:00:06.00,0:00:07.00,Signs,,0,0,0,,{\p1}m 0 0 l 10 10
Dialogue: 4,0:00:08.00,0:00:07.00,Signs,,0,0,0,,Invalid span
`;

describe('ASS captions', () => {
	test('reads styled, positioned runs with text punctuation intact', () => {
		const parsed = parseAssDocument(example)!;
		expect(parsed).toMatchObject({ width: 1280, height: 720, wrapStyle: 2 });
		expect(parsed.styles.get('Signs')).toMatchObject({ font: 'Sans', size: 30, primary: parseAssColor('&H800000FF', ''), bold: true, alignment: 8 });
		expect(parsed.cues).toHaveLength(2);
		const cue = parsed.cues[0];
		expect(cue).toMatchObject({ start: 2.2, end: 10, layer: 2, alignment: 7, position: [30, 100], fade: [150, 200] });
		expect(cue.runs.map((run) => run.text)).toEqual(['Left, sign\n', 'Second\u00a0line']);
		expect(cue.runs[1].style).toMatchObject({ primary: 'rgba(0, 0, 255, 1)', italic: true });
	});
	test('applies layer order and excludes cues at their end time', () => {
		const parsed = parseAssDocument(example)!;
		expect(activeAssCues(parsed, 1)).toEqual([]);
		expect(activeAssCues(parsed, 5).map((cue) => cue.layer)).toEqual([0, 2]);
		expect(activeAssCues(parsed, 8).map((cue) => cue.layer)).toEqual([2]);
		expect(activeAssCues(parsed, 10)).toEqual([]);
	});
});
