package core

import "time"

// Cache cause codes explain why a release's bytes were fetched or retained.
// The vocabulary is closed: readers ignore unknown codes and never guess one.
const (
	CacheCausePlaying      = "playing"
	CacheCausePaused       = "paused"
	CacheCauseUnfinished   = "unfinished"
	CacheCauseOwnerPin     = "owner-pin"
	CacheCauseShelf        = "shelf"
	CacheCauseNextEpisode  = "next-episode"
	CacheCausePostPlay     = "post-play"
	CacheCauseResumeWarm   = "resume-warm"
	CacheCauseDetailWarm   = "detail-warm"
	CacheCauseTrailerWarm  = "trailer-warm"
	CacheCauseBrowseWarm   = "browse-warm"
	CacheCauseWatchlist    = "watchlist"
	CacheCauseQualityCheck = "quality-check"
	CacheCauseFailover     = "failover"
	CacheCauseRepair       = "repair"
	CacheCauseUnrecorded   = "unrecorded"
)

// CacheShelfDetail explains a preload-shelf selection for one release.
type CacheShelfDetail struct {
	ReasonCodes []string `json:"reasonCodes,omitempty"`
	Score       float64  `json:"score,omitempty"`
	State       string   `json:"state,omitempty"` // fetched | reserved
}

// CacheCause is one observed reason for a release, with the first time it was seen.
type CacheCause struct {
	Code      string            `json:"code"`
	FirstSeen time.Time         `json:"firstSeen,omitzero"`
	Shelf     *CacheShelfDetail `json:"shelf,omitempty"`
}

// CacheProvenance is what a caller knows about a release when bytes are written
// or a cause is established. Empty fields leave the existing metadata alone.
type CacheProvenance struct {
	ReleaseID string
	File      string
	Size      int64
	Source    string // release name, else indexer name
	Title     string // cache-facing display title
	Cause     string
	Play      bool
	Shelf     *CacheShelfDetail
}

// CacheProvenanceRecorder is implemented by caches that keep per-release
// provenance. Recording must never fail or block byte admission.
type CacheProvenanceRecorder interface {
	RecordProvenance(CacheProvenance)
}
