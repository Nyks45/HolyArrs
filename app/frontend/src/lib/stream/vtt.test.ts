import { describe, expect, it } from 'vitest';
import { activeCuesAt, parseWebVTT } from './vtt.ts';

describe('WebVTT cue engine', () => {
	const sample = `WEBVTT

1
00:00:05.000 --> 00:00:08.500 align:middle
<i>First line</i>
Second <script>bad()</script>line

00:12.000 --> 00:15.250
MM:SS timing cue`;

	it('parses HH:MM:SS and MM:SS cues and strips tags', () => {
		const cues = parseWebVTT(sample);
		expect(cues).toHaveLength(2);
		expect(cues[0]).toEqual({
			start: 5,
			end: 8.5,
			lines: ['First line', 'Second bad()line']
		});
		expect(cues[1]).toEqual({
			start: 12,
			end: 15.25,
			lines: ['MM:SS timing cue']
		});
	});

	it('matches active cues and shifts timing by delayMS', () => {
		const cues = parseWebVTT(sample);
		expect(activeCuesAt(cues, 6, 0).map((c) => c.lines.join('\n'))).toEqual(['First line\nSecond bad()line']);
		expect(activeCuesAt(cues, 9.2, 0)).toEqual([]);
		// +1000ms delay means the 5s-8.5s subtitle renders between 6s and 9.5s.
		expect(activeCuesAt(cues, 9.2, 1000)).toHaveLength(1);
		// -1000ms delay means the 5s-8.5s subtitle renders earlier (4s-7.5s).
		expect(activeCuesAt(cues, 4.2, -1000)).toHaveLength(1);
	});
});
