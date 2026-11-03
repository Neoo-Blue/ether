package playback

import (
	"io"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
)

type bufferedSource struct {
	core.MediaSource
	bytes int64
}

func (b bufferedSource) BufferedAhead() int64 { return b.bytes }

type bufferedHLS struct {
	core.HLSSession
	buffer core.RecoveryBuffer
}

func (b bufferedHLS) RecoveryBuffer() core.RecoveryBuffer { return b.buffer }

type warmSource struct{}

func (warmSource) ReadAt([]byte, int64) (int, error) { return 0, io.EOF }
func (warmSource) Size() int64                       { return 10 }
func (warmSource) Name() string                      { return "file.mkv" }
func (warmSource) ReleaseID() string                 { return "A" }
func (warmSource) Close() error                      { return nil }

func TestPrepareRecordsCacheProvenance(t *testing.T) {
	h := newHarness(t, []*fakeRelease{mkv("A", 100_000, time.Minute)}, []string{"A"})
	h.prepare(owner, movieKey, "client")
	got := h.cache.provenanceFor("A")
	if len(got) != 1 {
		t.Fatalf("provenance = %+v", got)
	}
	p := got[0]
	if p.Cause != core.CacheCausePlaying || !p.Play || p.File != "A.mkv" || p.Size != 100_000 || p.Source != "Release.A" || p.Title != "The Signal" {
		t.Fatalf("playing provenance = %+v", p)
	}

	h.m.PlaybackProgress(owner.ID, "client", movieKey, time.Minute, "paused")
	if !h.cache.hasCause("A", core.CacheCausePaused) {
		t.Fatal("paused cause missing")
	}

	h.m.applyPins(nil, map[string]bool{"A": true}, nil)
	if !h.cache.hasCause("A", core.CacheCauseUnfinished) {
		t.Fatal("unfinished cause missing")
	}
}

func TestAddBufferedReportsSourceAndHLSStages(t *testing.T) {
	h := newHarness(t, []*fakeRelease{mkv("A", 100_000, time.Minute)}, []string{"A"})
	offset := int64(30_000)
	direct := &core.PlaySession{ID: "direct", Source: bufferedSource{bytes: 100}, Info: core.MediaInfo{Size: 1000, Duration: 100 * time.Second}, ViewOffset: &offset}
	h.m.addBuffered(direct)
	if direct.BufferedBytes != 100 || direct.BufferedUntil != 40*time.Second {
		t.Fatalf("direct buffered = %d %v", direct.BufferedBytes, direct.BufferedUntil)
	}
	h.m.deps.HLSLookup = func(string) (core.HLSSession, bool) {
		return bufferedHLS{buffer: core.RecoveryBuffer{Position: 10 * time.Second, ReadyUntil: 40 * time.Second}}, true
	}
	hls := &core.PlaySession{ID: "hls", Info: core.MediaInfo{Size: 1000, Duration: 100 * time.Second}}
	h.m.addBuffered(hls)
	if hls.BufferedBytes != 300 || hls.BufferedUntil != 40*time.Second {
		t.Fatalf("hls buffered = %d %v", hls.BufferedBytes, hls.BufferedUntil)
	}
}

func TestWarmCauseMapping(t *testing.T) {
	h := newHarness(t, []*fakeRelease{mkv("A", 100_000, time.Minute)}, []string{"A"})
	res := &core.Resolution{Release: core.Release{ID: "A", Name: "Release.A"}}
	for reason, want := range map[string]string{
		core.PrewarmDetail:      core.CacheCauseDetailWarm,
		core.PrewarmTrailer:     core.CacheCauseTrailerWarm,
		core.PrewarmPostPlay:    core.CacheCausePostPlay,
		core.PrewarmBrowse:      core.CacheCauseBrowseWarm,
		core.PrewarmNextEpisode: core.CacheCauseNextEpisode,
	} {
		h.m.noteWarmCause(res, warmSource{}, reason, "The Signal")
		if !h.cache.hasCause("A", want) {
			t.Fatalf("reason %q did not record %q: %+v", reason, want, h.cache.provenanceFor("A"))
		}
	}
}

func TestPrepareRecordsEpisodeTitleWithSeasonAndEpisode(t *testing.T) {
	h := newHarness(t, []*fakeRelease{mkv("A", 100_000, time.Minute)}, []string{"A"})
	h.prepare(owner, epKey, "client")
	got := h.cache.provenanceFor("A")
	if len(got) != 1 || got[0].Title != "Boundary Bay · S01E01 · Pilot" {
		t.Fatalf("episode title = %+v", got)
	}
}
