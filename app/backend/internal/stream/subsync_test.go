package stream

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestParseVTTCueIntervalsAndAutoSyncOffsets(t *testing.T) {
	// Build a sparse speech pattern across 120s so false lag aliases cannot dominate.
	speechIntervals := [][2]float64{
		{8.0, 10.5},
		{15.2, 17.8},
		{23.0, 25.0},
		{34.5, 37.2},
		{48.0, 51.0},
		{63.4, 65.8},
		{79.0, 82.0},
		{96.5, 99.0},
	}

	const totalSec = 120.0
	nFrames := int(totalSec / subSyncFrameSec)
	audioEnv := make([]float64, nFrames)
	for i := range audioEnv {
		audioEnv[i] = 0.02 // low background noise
		sec := float64(i) * subSyncFrameSec
		for _, iv := range speechIntervals {
			if sec >= iv[0] && sec < iv[1] {
				audioEnv[i] = 0.92
				break
			}
		}
	}

	cases := []struct {
		name      string
		shiftSec  float64
		wantOffMS int
	}{
		{"in_sync", 0, 0},
		// Audio speaks at iv[0], cue is placed 2.4s EARLIER -> must delay subtitle by +2400ms.
		{"positive_delay_2400ms", 2.40, 2400},
		// Audio speaks at iv[0], cue is placed 1.85s LATER -> must advance subtitle by -1850ms.
		{"negative_delay_1850ms", -1.85, -1850},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sb strings.Builder
			sb.WriteString("WEBVTT\n\n")
			for idx, iv := range speechIntervals {
				st := iv[0] - tc.shiftSec
				en := iv[1] - tc.shiftSec
				if st < 0 || en <= st {
					continue
				}
				fmt.Fprintf(&sb, "%d\n%s --> %s\nLine %d\n\n", idx+1, fmtVTT(st), fmtVTT(en), idx+1)
			}
			cues := ParseVTTCueIntervals(sb.String())
			if len(cues) != len(speechIntervals) {
				t.Fatalf("ParseVTTCueIntervals = %d, want %d", len(cues), len(speechIntervals))
			}
			offMS, conf, matched := EstimateSubtitleOffsetMS(audioEnv, subSyncFrameSec, 0, cues, 20)
			if matched != len(speechIntervals) {
				t.Fatalf("matched = %d, want %d", matched, len(speechIntervals))
			}
			if conf < 0.7 {
				t.Fatalf("expected strong confidence, got %v", conf)
			}
			if math.Abs(float64(offMS-tc.wantOffMS)) > 100 {
				t.Fatalf("offsetMS = %d, want %d (+/-100ms)", offMS, tc.wantOffMS)
			}
		})
	}
}

func fmtVTT(sec float64) string {
	ms := int(math.Round(sec * 1000))
	s := ms / 1000
	ms %= 1000
	m := s / 60
	s %= 60
	return fmt.Sprintf("00:%02d:%02d.%03d", m, s, ms)
}
