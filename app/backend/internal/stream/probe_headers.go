package stream

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"math"
	"strconv"
	"strings"
)

// PreflightResult captures container, HLS status, duration, and total size
// discovered during stream preflight without downloading the whole asset.
type PreflightResult struct {
	ContentType string
	IsHLS       bool
	DurationSec float64
	TotalSize   int64
}

func parseHLSPlaylistDuration(body string, truncated bool) float64 {
	if truncated {
		return 0
	}
	var total float64
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 0, 16*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "#EXTINF:") {
			continue
		}
		rest := strings.TrimPrefix(line, "#EXTINF:")
		if comma := strings.IndexByte(rest, ','); comma >= 0 {
			rest = rest[:comma]
		}
		if sec, err := strconv.ParseFloat(strings.TrimSpace(rest), 64); err == nil && sec > 0 && !math.IsNaN(sec) && !math.IsInf(sec, 0) {
			total += sec
		}
	}
	if total < 10 || total >= 86400 {
		return 0
	}
	return total
}

func parseContentRangeTotal(statusCode int, contentRange, contentLength string) int64 {
	if cr := strings.TrimSpace(contentRange); cr != "" {
		if slash := strings.LastIndexByte(cr, '/'); slash >= 0 {
			tail := strings.TrimSpace(cr[slash+1:])
			if tail != "" && tail != "*" {
				if n, err := strconv.ParseInt(tail, 10, 64); err == nil && n > 0 {
					return n
				}
			}
		}
	}
	if statusCode == 200 {
		if n, err := strconv.ParseInt(strings.TrimSpace(contentLength), 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func detectDisguisedContainer(ct string, peek []byte) string {
	if len(peek) >= 4 && peek[0] == 0x1a && peek[1] == 0x45 && peek[2] == 0xdf && peek[3] == 0xa3 {
		head := peek[:min(128, len(peek))]
		if bytes.Contains(bytes.ToLower(head), []byte("webm")) {
			if ct == "" || strings.Contains(ct, "octet-stream") {
				return "video/webm"
			}
			return ct
		}
		return "video/x-matroska"
	}
	if len(peek) >= 8 && string(peek[4:8]) == "ftyp" {
		if ct == "" || strings.Contains(ct, "octet-stream") {
			return "video/mp4"
		}
	}
	return ct
}

func parseMP4Duration(peek []byte) float64 {
	off := 0
	for off+24 <= len(peek) {
		rel := bytes.Index(peek[off:], []byte("mvhd"))
		if rel < 0 {
			return 0
		}
		idx := off + rel
		off = idx + 4
		if idx < 4 || idx+24 > len(peek) {
			continue
		}
		if peek[idx+5] != 0 || peek[idx+6] != 0 || peek[idx+7] != 0 {
			continue
		}
		ver := peek[idx+4]
		var durSec float64
		switch ver {
		case 0:
			ts := binary.BigEndian.Uint32(peek[idx+16 : idx+20])
			dur := binary.BigEndian.Uint32(peek[idx+20 : idx+24])
			if ts > 0 && dur > 0 {
				durSec = float64(dur) / float64(ts)
			}
		case 1:
			if idx+36 > len(peek) {
				continue
			}
			ts := binary.BigEndian.Uint32(peek[idx+24 : idx+28])
			dur := binary.BigEndian.Uint64(peek[idx+28 : idx+36])
			if ts > 0 && dur > 0 {
				durSec = float64(dur) / float64(ts)
			}
		default:
			continue
		}
		if durSec > 0 && durSec < 86400 && !math.IsNaN(durSec) && !math.IsInf(durSec, 0) {
			return durSec
		}
	}
	return 0
}

func parseEBMLDuration(peek []byte) float64 {
	segInfoID := []byte{0x15, 0x49, 0xa9, 0x66}
	off := 0
	for off < len(peek) {
		rel := bytes.Index(peek[off:], segInfoID)
		if rel < 0 {
			return 0
		}
		idx := off + rel
		off = idx + 4
		if idx+6 <= len(peek) && peek[idx+4] == 0x53 && peek[idx+5] == 0xac {
			continue
		}
		chunk := peek[idx:min(len(peek), idx+512)]
		var scale uint64 = 1_000_000
		if si := bytes.Index(chunk, []byte{0x2a, 0xd7, 0xb1}); si >= 0 && si+4 < len(chunk) && (chunk[si+3]&0x80) != 0 {
			sz := int(chunk[si+3] & 0x7f)
			if sz >= 1 && sz <= 8 && si+4+sz <= len(chunk) {
				var v uint64
				for _, b := range chunk[si+4 : si+4+sz] {
					v = (v << 8) | uint64(b)
				}
				if v > 0 {
					scale = v
				}
			}
		}
		if di := bytes.Index(chunk, []byte{0x44, 0x89}); di >= 0 && di+3 < len(chunk) && (chunk[di+2]&0x80) != 0 {
			sz := int(chunk[di+2] & 0x7f)
			var rawDur float64
			switch sz {
			case 4:
				if di+7 <= len(chunk) {
					rawDur = float64(math.Float32frombits(binary.BigEndian.Uint32(chunk[di+3 : di+7])))
				}
			case 8:
				if di+11 <= len(chunk) {
					rawDur = math.Float64frombits(binary.BigEndian.Uint64(chunk[di+3 : di+11]))
				}
			}
			if rawDur > 0 {
				durSec := rawDur * float64(scale) / 1e9
				if durSec > 0 && durSec < 86400 && !math.IsNaN(durSec) && !math.IsInf(durSec, 0) {
					return durSec
				}
			}
		}
	}
	return 0
}
