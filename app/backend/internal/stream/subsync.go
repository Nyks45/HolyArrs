package stream

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	subSyncSampleRate = 8000
	subSyncFrameMS    = 50
	subSyncFrameSec   = float64(subSyncFrameMS) / 1000.0
	subSyncFrameSamps = subSyncSampleRate * subSyncFrameMS / 1000 // 400 samples per 50ms frame
)

// AutoSyncResult summarizes subtitle-to-audio cross-correlation alignment.
type AutoSyncResult struct {
	OffsetMS    int     `json:"offset_ms"`
	Confidence  float64 `json:"confidence"`
	SampledSec  float64 `json:"sampled_sec"`
	CuesMatched int     `json:"cues_matched"`
}

var reVTTTiming = regexp.MustCompile(`(?m)^[ \t]*((?:\d+:)?\d{1,2}:\d{2}(?:[.,]\d{1,3})?)[ \t]*-->[ \t]*((?:\d+:)?\d{1,2}:\d{2}(?:[.,]\d{1,3})?)`)

// ParseVTTCueIntervals extracts sorted merged [start, end] speech intervals in seconds.
func ParseVTTCueIntervals(vtt string) [][2]float64 {
	matches := reVTTTiming.FindAllStringSubmatch(strings.ReplaceAll(vtt, "\r\n", "\n"), -1)
	if len(matches) == 0 {
		return nil
	}
	raw := make([][2]float64, 0, len(matches))
	for _, m := range matches {
		st, ok1 := parseVTTTimestamp(m[1])
		en, ok2 := parseVTTTimestamp(m[2])
		if !ok1 || !ok2 || en <= st {
			continue
		}
		raw = append(raw, [2]float64{st, en})
	}
	if len(raw) == 0 {
		return nil
	}
	slices.SortFunc(raw, func(a, b [2]float64) int {
		if a[0] < b[0] {
			return -1
		}
		if a[0] > b[0] {
			return 1
		}
		return 0
	})
	merged := make([][2]float64, 0, len(raw))
	cur := raw[0]
	for _, iv := range raw[1:] {
		if iv[0] <= cur[1]+0.08 {
			if iv[1] > cur[1] {
				cur[1] = iv[1]
			}
			continue
		}
		merged = append(merged, cur)
		cur = iv
	}
	merged = append(merged, cur)
	return merged
}

// ExtractAudioSpeechEnvelope decodes a band-passed mono speech activity envelope (0..1 per 50ms frame).
func ExtractAudioSpeechEnvelope(ctx context.Context, mediaURL string, windowStart, durationSec float64) ([]float64, float64, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, 0, errors.New("ffmpeg is required for audio subtitle sync")
	}
	if durationSec <= 0 {
		durationSec = 150
	}
	args := []string{"-hide_banner", "-loglevel", "error"}
	if strings.HasPrefix(mediaURL, "http://") || strings.HasPrefix(mediaURL, "https://") {
		args = append(args, "-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "2")
	}
	if windowStart > 0.25 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", windowStart))
	}
	args = append(args,
		"-t", fmt.Sprintf("%.1f", durationSec),
		"-i", mediaURL,
		"-vn",
		"-af", "highpass=f=300,lowpass=f=3000",
		"-ac", "1",
		"-ar", strconv.Itoa(subSyncSampleRate),
		"-f", "s16le",
		"pipe:1",
	)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if out.Len() < subSyncFrameSamps*2*40 {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			return nil, 0, fmt.Errorf("audio extraction failed: %s", msg)
		}
	}

	raw := out.Bytes()
	nFrames := len(raw) / (subSyncFrameSamps * 2)
	if nFrames < 20 {
		return nil, float64(nFrames) * subSyncFrameSec, errors.New("insufficient audio frames for subtitle sync")
	}
	energies := make([]float64, nFrames)
	for f := 0; f < nFrames; f++ {
		base := f * subSyncFrameSamps * 2
		var sumSq float64
		for s := 0; s < subSyncFrameSamps; s++ {
			v := int16(binary.LittleEndian.Uint16(raw[base+s*2:]))
			nf := float64(v) / 32768.0
			sumSq += nf * nf
		}
		rms := math.Sqrt(sumSq / float64(subSyncFrameSamps))
		energies[f] = math.Log1p(rms * 32.0)
	}
	sorted := slices.Clone(energies)
	slices.Sort(sorted)
	floor := sorted[len(sorted)*3/10]
	ceil := sorted[len(sorted)*95/100]
	span := ceil - floor
	if span < 0.05 {
		span = 0.05
	}
	env := make([]float64, nFrames)
	for i, e := range energies {
		v := (e - floor) / span
		if v < 0 {
			v = 0
		} else if v > 1 {
			v = 1
		}
		env[i] = v
	}
	return env, float64(nFrames) * subSyncFrameSec, nil
}

// EstimateSubtitleOffsetMS aligns speech activity frames with VTT cues via zero-mean cross-correlation.
// Positive offsetMS means audio speaks after the cue timestamp (delay cues forward by offsetMS).
func EstimateSubtitleOffsetMS(audioEnv []float64, frameSec, windowStart float64, cues [][2]float64, maxOffsetSec float64) (offsetMS int, confidence float64, matched int) {
	n := len(audioEnv)
	if n < 20 || frameSec <= 0 || len(cues) == 0 {
		return 0, 0, 0
	}
	if maxOffsetSec <= 0 {
		maxOffsetSec = 30
	}
	padFrames := int(math.Ceil(maxOffsetSec / frameSec))
	timeline := make([]float64, n+2*padFrames)

	winEnd := windowStart + float64(n+padFrames)*frameSec
	winMin := windowStart - float64(padFrames)*frameSec
	for _, iv := range cues {
		if iv[1] < winMin || iv[0] > winEnd {
			continue
		}
		matched++
		jStart := int(math.Floor((iv[0]-windowStart)/frameSec)) + padFrames
		jEnd := int(math.Ceil((iv[1]-windowStart)/frameSec)) + padFrames
		if jStart < 0 {
			jStart = 0
		}
		if jEnd > len(timeline) {
			jEnd = len(timeline)
		}
		for j := jStart; j < jEnd; j++ {
			timeline[j] = 1.0
		}
	}
	if matched == 0 {
		return 0, 0, 0
	}

	var aMean, aVar float64
	for _, v := range audioEnv {
		aMean += v
	}
	aMean /= float64(n)
	aCentered := make([]float64, n)
	for i, v := range audioEnv {
		d := v - aMean
		aCentered[i] = d
		aVar += d * d
	}
	if aVar < 1e-6 {
		return 0, 0, matched
	}

	bestCorr := -1.0
	bestLag := 0
	for lag := -padFrames; lag <= padFrames; lag++ {
		var tSum, tSq, dot float64
		for i := 0; i < n; i++ {
			s := timeline[i+padFrames-lag]
			tSum += s
			tSq += s * s
			dot += aCentered[i] * s
		}
		tMean := tSum / float64(n)
		tVar := tSq - float64(n)*tMean*tMean
		if tVar < 1e-6 {
			continue
		}
		corr := dot / math.Sqrt(aVar*tVar)
		if corr > bestCorr {
			bestCorr = corr
			bestLag = lag
		}
	}
	if bestCorr <= 0 {
		return 0, 0, matched
	}
	if bestCorr > 1 {
		bestCorr = 1
	}
	return int(math.Round(float64(bestLag) * frameSec * 1000)), math.Round(bestCorr*1000) / 1000, matched
}

// AutoSyncSubtitle samples session audio around startSec and aligns the provided WebVTT cues.
func (m *Manager) AutoSyncSubtitle(ctx context.Context, sessionID, vtt string, startSec, maxOffsetSec float64) (AutoSyncResult, error) {
	cues := ParseVTTCueIntervals(vtt)
	if len(cues) == 0 {
		return AutoSyncResult{}, errors.New("subtitle contains no timed cues")
	}
	sess, ok := m.getSession(sessionID)
	if !ok {
		return AutoSyncResult{}, errors.New("stream session not found")
	}
	sess.mu.Lock()
	target, _, _, targetOK := sess.localOrMediaTargetLocked(m.localBaseURL())
	var knownDur float64
	if sess.probe != nil {
		knownDur = sess.probe.DurationSec
	}
	sess.mu.Unlock()
	if !targetOK {
		return AutoSyncResult{}, errors.New("stream target is not ready yet")
	}

	if maxOffsetSec <= 0 || maxOffsetSec > 60 {
		maxOffsetSec = 30
	}
	winStart := startSec - 15
	if startSec < 15 && (knownDur == 0 || knownDur > 210) {
		// Skip silent studio logos when starting at 0:00 on long titles.
		winStart = 45
	}
	if winStart < 0 {
		winStart = 0
	}

	const sampleWindow = 150.0
	env, sampled, err := ExtractAudioSpeechEnvelope(ctx, target, winStart, sampleWindow)
	if err != nil && winStart > 0 {
		winStart = 0
		env, sampled, err = ExtractAudioSpeechEnvelope(ctx, target, 0, sampleWindow)
	}
	if err != nil {
		return AutoSyncResult{}, err
	}

	off, conf, matched := EstimateSubtitleOffsetMS(env, subSyncFrameSec, winStart, cues, maxOffsetSec)
	return AutoSyncResult{
		OffsetMS:    off,
		Confidence:  conf,
		SampledSec:  math.Round(sampled*10) / 10,
		CuesMatched: matched,
	}, nil
}
