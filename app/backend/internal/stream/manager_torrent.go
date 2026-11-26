package stream

import (
	"context"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
)

func (m *Manager) resolveAndWarm(sess *Session) {
	t := sess.torrent
	select {
	case <-t.GotInfo():
	case <-sess.ctx.Done():
		return
	case <-time.After(60 * time.Second):
		sess.mu.Lock()
		if sess.state == "resolving" {
			sess.state = "error"
			sess.errMsg = "timed out waiting for torrent metadata from peers"
		}
		sess.mu.Unlock()
		return
	}

	vfiles := filterVideoFiles(t.Files())
	if len(vfiles) == 0 {
		sess.mu.Lock()
		sess.state = "error"
		sess.errMsg = "no playable video files found in this torrent"
		sess.mu.Unlock()
		return
	}

	sel := pickBestVideoFile(vfiles, sess.kind, sess.season, sess.episode)
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return
	}
	sess.files = vfiles
	sess.selected = sel
	sess.state = "buffering"
	sess.warmupAt = time.Now()
	sess.mu.Unlock()

	prioritizeFile(t, vfiles, sel)
	m.monitorWarmup(sess)
}

func (m *Manager) monitorWarmup(sess *Session) {
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-sess.ctx.Done():
			return
		case <-tick.C:
			sess.mu.Lock()
			if sess.closed || sess.selected < 0 || sess.selected >= len(sess.files) {
				sess.mu.Unlock()
				return
			}
			f := sess.files[sess.selected]
			comp := f.BytesCompleted()
			fLen := f.Length()
			needProbe := sess.probe == nil && !sess.probing && time.Since(sess.probeFailedAt) > 4*time.Second
			readyThreshold := minInt64(2<<20, fLen)
			canProbe := needProbe && comp >= minInt64(1500*1024, fLen)
			if canProbe {
				sess.probing = true
			}
			if comp >= readyThreshold && sess.state == "buffering" && time.Since(sess.warmupAt) >= 8*time.Second {
				sess.state = "ready"
			}
			sess.mu.Unlock()

			if canProbe {
				m.runProbeAsync(sess)
			}
		}
	}
}

func (m *Manager) runProbeAsync(sess *Session) {
	sess.mu.Lock()
	mediaURL, fileExt, _, ok := sess.localOrMediaTargetLocked(m.localBaseURL())
	sess.mu.Unlock()
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(sess.ctx, 18*time.Second)
	defer cancel()
	p, err := probeMedia(ctx, mediaURL, fileExt)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.probing = false
	if err != nil {
		sess.probeFailedAt = time.Now()
		return
	}
	sess.probe = p
	if p != nil && len(p.Chapters) > 0 {
		sess.chapters = p.Chapters
	}
	if sess.state == "buffering" {
		sess.state = "ready"
	}
}

func prioritizeFile(t *torrent.Torrent, files []*torrent.File, selectedIdx int) {
	for _, f := range t.Files() {
		f.SetPriority(torrent.PiecePriorityNone)
	}
	if selectedIdx < 0 || selectedIdx >= len(files) {
		return
	}
	f := files[selectedIdx]
	f.SetPriority(torrent.PiecePriorityNormal)
	info := t.Info()
	if info == nil || info.PieceLength <= 0 {
		f.Download()
		return
	}
	beginIdx := f.BeginPieceIndex()
	endIdx := f.EndPieceIndex()
	headPieces := int((6<<20)/info.PieceLength) + 1
	for i := beginIdx; i < endIdx && i < beginIdx+headPieces; i++ {
		if i == beginIdx {
			t.Piece(i).SetPriority(torrent.PiecePriorityNow)
		} else {
			t.Piece(i).SetPriority(torrent.PiecePriorityHigh)
		}
	}
	if endIdx-1 > beginIdx {
		t.Piece(endIdx - 1).SetPriority(torrent.PiecePriorityHigh)
	}
}

func filterVideoFiles(all []*torrent.File) []*torrent.File {
	var vids []*torrent.File
	for _, f := range all {
		ext := strings.ToLower(filepath.Ext(f.DisplayPath()))
		if videoExts[ext] {
			vids = append(vids, f)
		}
	}
	if len(vids) <= 1 {
		return vids
	}
	var nonSample []*torrent.File
	for _, f := range vids {
		low := strings.ToLower(f.DisplayPath())
		if strings.Contains(low, "sample") && f.Length() < 80<<20 {
			continue
		}
		nonSample = append(nonSample, f)
	}
	if len(nonSample) > 0 {
		vids = nonSample
	}
	sort.Slice(vids, func(i, j int) bool {
		return vids[i].DisplayPath() < vids[j].DisplayPath()
	})
	return vids
}

var (
	reSeasonEp = regexp.MustCompile(`(?i)S(\d{1,2})\s*[.\-_ ]*E(\d{1,3})`)
	reAltEp    = regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{1,3})\b`)
)

func pickBestVideoFile(files []*torrent.File, kind string, season, episode int) int {
	if len(files) == 0 {
		return -1
	}
	if kind == "show" && season > 0 && episode > 0 {
		best := -1
		var bestSize int64
		for i, f := range files {
			name := filepath.Base(f.DisplayPath())
			s, e, ok := extractEpisodeNums(name)
			if ok && s == season && e == episode && f.Length() > bestSize {
				best = i
				bestSize = f.Length()
			}
		}
		if best >= 0 {
			return best
		}
	}
	best := 0
	for i := 1; i < len(files); i++ {
		if files[i].Length() > files[best].Length() {
			best = i
		}
	}
	return best
}

func extractEpisodeNums(name string) (season, episode int, ok bool) {
	if m := reSeasonEp.FindStringSubmatch(name); len(m) == 3 {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		return s, e, true
	}
	if m := reAltEp.FindStringSubmatch(name); len(m) == 3 {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		return s, e, true
	}
	return 0, 0, false
}

func slicesClone(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
