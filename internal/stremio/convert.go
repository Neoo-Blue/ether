package stremio

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/newznab"
	"github.com/braedonsaunders/ether/internal/releaseparse"
)

// Stream is one stream object an add-on answers. Standard fields keep
// their Stremio meaning; Ether is the optional per-stream `ether` block an
// Ether plugin may add (see internal/plugins/adapters).
type Stream struct {
	Name          string          `json:"name,omitempty"`
	Title         string          `json:"title,omitempty"`
	Description   string          `json:"description,omitempty"`
	InfoHash      string          `json:"infoHash,omitempty"`
	FileIdx       *int            `json:"fileIdx,omitempty"`
	URL           string          `json:"url,omitempty"`
	NZBURL        string          `json:"nzbUrl,omitempty"`
	YTID          string          `json:"ytId,omitempty"`
	ExternalURL   string          `json:"externalUrl,omitempty"`
	Sources       []string        `json:"sources,omitempty"`
	BehaviorHints StreamHints     `json:"behaviorHints,omitzero"`
	Ether         json.RawMessage `json:"ether,omitempty"`
}

// StreamHints is a stream's behaviorHints.
type StreamHints struct {
	Filename     string `json:"filename,omitempty"`
	VideoSize    int64  `json:"videoSize,omitempty"`
	BingeGroup   string `json:"bingeGroup,omitempty"`
	NotWebReady  bool   `json:"notWebReady,omitempty"`
	ProxyHeaders struct {
		Request map[string]string `json:"request,omitempty"`
	} `json:"proxyHeaders,omitzero"`
}

// guidSuffix is the per-file part of a release GUID: the file index when
// the addon gives one, else the requested episode for series.
func guidSuffix(s *Stream, req core.SearchRequest, packs bool) string {
	switch {
	case packs:
		return ""
	case s.FileIdx != nil && *s.FileIdx >= 0:
		return "/" + strconv.Itoa(*s.FileIdx)
	case req.Type != core.TypeMovie:
		return fmt.Sprintf("/s%de%d", req.Season, req.Episode)
	}
	return ""
}

// ConvertOptions carry what conversion needs beyond the streams.
type ConvertOptions struct {
	// Origin is the add-on's own scheme://host[:port], the one private
	// address its stream URLs may reach.
	Origin string
	// Instance identifies the configured add-on in HTTP and NZB GUIDs, so
	// two configurations never share a release identity.
	Instance string
}

// Converted is the outcome of Convert. Source[i] is the index of the
// stream Releases[i] came from.
type Converted struct {
	Releases         []core.Release
	Source           []int
	SkippedNoHash    int
	SkippedAmbiguous int
}

// Convert turns an add-on's streams into releases, best first as the
// add-on ordered them:
//
//   - a stream labelled with an info hash is a torrent Ether streams
//     through its own debrid accounts. Its HTTP link, when it has one, is
//     kept as the release's StreamURL so a server without a debrid account
//     can still play the addon's own resolution of it;
//   - otherwise a stream with an nzbUrl is a Usenet release;
//   - otherwise a stream with an HTTP(S) url is an HTTP stream (an addon's
//     own resolver or CDN link), read with range requests.
//
// External links, YouTube ids and the rest have nothing Ether can stream.
// skippedNoHash counts only streams discarded for having none of those
// (after the pure name/pack filters, so the count does not change which
// releases come out); streams with conflicting hash labels count
// separately as skippedAmbiguous, and duplicate skipped streams (same
// chosen name, URL and GUID suffix) count at most once. The count walks
// every stream, but only the first maxResults valid ones become releases.
func Convert(streams []Stream, req core.SearchRequest, look Lookup, opts ConvertOptions) Converted {
	packs := look.Packs
	var res Converted
	out := make([]core.Release, 0, min(len(streams), maxResults))
	skippedNoHash, skippedAmbiguous := 0, 0
	seen := make(map[string]bool, len(streams))
	seenSkipped := make(map[string]bool, len(streams))
	for i := range streams {
		s := &streams[i]
		text := s.Description
		if text == "" {
			text = s.Title
		}
		torrent, file := names(text, s.BehaviorHints.Filename)
		var name string
		if packs {
			if !isSeasonPack(torrent, req.Season) {
				continue
			}
			name = torrent
		} else if name = pickName(req, file, torrent); name == "" {
			continue
		}
		hash, ambiguous := streamHash(s)
		streamURL, nzbURL := httpLink(s.URL), httpLink(s.NZBURL)
		if ambiguous || (hash == "" && nzbURL == "" && (streamURL == "" || packs)) {
			key := name + "\x00" + s.URL + "\x00" + guidSuffix(s, req, packs)
			if ambiguous {
				key = "ambiguous\x00" + key
			}
			if !seenSkipped[key] {
				seenSkipped[key] = true
				if ambiguous {
					skippedAmbiguous++
				} else {
					skippedNoHash++
				}
			}
			continue
		}
		if len(out) == maxResults {
			continue
		}
		size := s.BehaviorHints.VideoSize
		if size <= 0 {
			size = parseSize(text)
		}
		rel := core.Release{
			Name:     name,
			Size:     size,
			Seeders:  parseSeeders(text),
			Info:     describe(name, file, torrent),
			Category: movieCategory,
		}
		if req.Type != core.TypeMovie {
			rel.Category = tvCategory
		}
		rel.Info.Languages = streamLanguages(rel.Info.Languages, s.Name+"\n"+s.Title+"\n"+s.Description)
		rel.Info = entryEpisodeIdentity(rel.Info, req, look.EntryEpisode)
		rel.DebridCached, rel.DebridUncached = debridHints(s.Name)
		switch {
		case hash != "":
			rel.GUID = "btih:" + hash + guidSuffix(s, req, packs)
			rel.Protocol = core.ProtocolTorrent
			rel.InfoHash = hash
			rel.MagnetURL = newznab.MagnetFor(hash, firstNonEmpty(torrent, name), streamTrackers(s))
			if streamURL != "" && !packs {
				rel.StreamURL, rel.StreamHeaders, rel.StreamOrigin = streamURL, s.RequestHeaders(), opts.Origin
			}
		case nzbURL != "":
			rel.GUID = "nzb:" + opts.Instance + ":" + linkIdentity(name, size, nzbURL, s, req)
			rel.Protocol = core.ProtocolUsenet
			rel.NZBURL = nzbURL
		default:
			rel.GUID = "http:" + opts.Instance + ":" + linkIdentity(name, size, streamURL, s, req)
			rel.Protocol = core.ProtocolHTTP
			rel.StreamURL, rel.StreamHeaders, rel.StreamOrigin = streamURL, s.RequestHeaders(), opts.Origin
		}
		if seen[rel.GUID] {
			continue
		}
		seen[rel.GUID] = true
		out = append(out, rel)
		res.Source = append(res.Source, i)
	}
	res.Releases, res.SkippedNoHash, res.SkippedAmbiguous = out, skippedNoHash, skippedAmbiguous
	return res
}

// streamHash merges the hashes a stream labels explicitly: its infoHash
// field, its URL and its sources. Distinct labels conflict: ambiguous
// reports it and "" wins, rather than a guess — a conflict is not an
// absence, so callers can count it apart from a merely missing hash.
// Uma fonte rotulada internamente em conflito (ambiguous) também impõe
// "" — mesmo com um hash válido em outra fonte.
// Valores inválidos, sim, são ausência e não bloqueiam um hash válido.
// Unlabelled hex in names, titles, descriptions or file names never counts.
func streamHash(s *Stream) (hash string, ambiguous bool) {
	urlHash, urlAmbiguous := hashFromURL(s.URL)
	if urlAmbiguous {
		return "", true
	}
	srcHash, srcAmbiguous := hashFromSources(s.Sources)
	if srcAmbiguous {
		return "", true
	}
	hash = newznab.NormalizeInfoHash(s.InfoHash)
	for _, candidate := range []string{urlHash, srcHash} {
		if candidate == "" {
			continue
		}
		if hash != "" && candidate != hash {
			return "", true
		}
		hash = candidate
	}
	return hash, false
}

// magnetValues parses a magnet URI's query, tolerating unescaped junk in
// other parameters (same tolerance as newznab.InfoHashFromMagnet).
func magnetValues(magnet string) url.Values {
	q := magnet[strings.IndexByte(magnet, ':')+1:]
	q = strings.TrimPrefix(q, "?")
	values, err := url.ParseQuery(q)
	if err != nil {
		values = url.Values{}
		for _, part := range strings.Split(q, "&") {
			if k, v, ok := strings.Cut(part, "="); ok {
				if uv, uerr := url.QueryUnescape(v); uerr == nil {
					v = uv
				}
				values.Add(k, v)
			}
		}
	}
	return values
}

// hashFromMagnet extracts the v1 info hash from a magnet URI's
// xt=urn:btih: parameters. ambiguous reports two or more distinct valid
// hashes inside the same magnet; invalid xt values are absence.
func hashFromMagnet(magnet string) (hash string, ambiguous bool) {
	if !strings.HasPrefix(strings.ToLower(magnet), "magnet:") {
		return "", false
	}
	for _, xt := range magnetValues(magnet)["xt"] {
		if lower := strings.ToLower(xt); strings.HasPrefix(lower, "urn:btih:") {
			candidate := newznab.NormalizeInfoHash(xt[len("urn:btih:"):])
			if candidate == "" {
				continue
			}
			if hash != "" && candidate != hash {
				return "", true
			}
			hash = candidate
		}
	}
	return hash, false
}

// magnetTrackers extracts a magnet URI's tr= announce URLs.
func magnetTrackers(magnet string) []string {
	if !strings.HasPrefix(strings.ToLower(magnet), "magnet:") {
		return nil
	}
	var out []string
	for key, values := range magnetValues(magnet) {
		if !strings.EqualFold(key, "tr") {
			continue
		}
		for _, tr := range values {
			if tr = strings.TrimSpace(tr); tr != "" {
				out = append(out, tr)
			}
		}
	}
	return out
}

// streamMagnets collects every original magnet a stream carries: its URL
// itself, a ?magnet= query value, or a magnet among its sources.
func streamMagnets(s *Stream) []string {
	var magnets []string
	raw := strings.TrimSpace(s.URL)
	if strings.HasPrefix(strings.ToLower(raw), "magnet:") {
		magnets = append(magnets, raw)
	} else if u, err := url.Parse(raw); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		for key, values := range u.Query() {
			if strings.EqualFold(key, "magnet") {
				magnets = append(magnets, values...)
			}
		}
	}
	for _, source := range s.Sources {
		if source = strings.TrimSpace(source); strings.HasPrefix(strings.ToLower(source), "magnet:") {
			magnets = append(magnets, source)
		}
	}
	return magnets
}

// hashFromSources reads "dht:<hash>" sources and magnet sources.
// ambiguous reports an internal conflict — between sources, or inside a
// single magnet source — distinguishing it from absence ("", false):
// invalid values are absence and never make a source ambiguous.
func hashFromSources(sources []string) (hash string, ambiguous bool) {
	for _, source := range sources {
		source = strings.TrimSpace(source)
		var candidate string
		if strings.HasPrefix(strings.ToLower(source), "dht:") {
			candidate = newznab.NormalizeInfoHash(source[len("dht:"):])
		} else {
			var magnetAmbiguous bool
			candidate, magnetAmbiguous = hashFromMagnet(source)
			if magnetAmbiguous {
				return "", true
			}
		}
		if candidate == "" {
			continue
		}
		if hash != "" && candidate != hash {
			return "", true
		}
		hash = candidate
	}
	return hash, false
}

// hashFromURL reads a magnet URL, explicit hash query fields (including a
// magnet carried in ?magnet=) or the known Torrentio-style
// /resolve/{provider}/{credential}/{hash}/... route. ambiguous reports an
// internal conflict within the URL itself (e.g. ?info_hash=A&info_hash=B,
// or a magnet with two distinct xt hashes), distinguishing it from
// absence ("", false): invalid values are absence. Arbitrary hexadecimal
// URL values may be credentials or opaque IDs, not torrent identities.
func hashFromURL(raw string) (hash string, ambiguous bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(raw), "magnet:") {
		return hashFromMagnet(raw)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	for key, values := range u.Query() {
		switch strings.ToLower(key) {
		case "infohash", "info_hash", "hash", "btih":
			for _, value := range values {
				if candidate := newznab.NormalizeInfoHash(value); candidate != "" {
					if hash != "" && candidate != hash {
						return "", true
					}
					hash = candidate
				}
			}
		case "magnet":
			for _, value := range values {
				candidate, magnetAmbiguous := hashFromMagnet(value)
				if magnetAmbiguous {
					return "", true
				}
				if candidate != "" {
					if hash != "" && candidate != hash {
						return "", true
					}
					hash = candidate
				}
			}
		}
	}
	if hash != "" {
		return hash, false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i+3 < len(parts); i++ {
		if parts[i] != "resolve" {
			continue
		}
		switch strings.ToLower(parts[i+1]) {
		case config.DebridRealDebrid, config.DebridAllDebrid, config.DebridPremiumize,
			config.DebridTorBox, config.DebridDebridLink, config.DebridOffcloud:
			return newznab.NormalizeInfoHash(parts[i+3]), false
		}
	}
	return "", false
}

// videoExt reports whether a file name ends in a video container.
func videoExt(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".mkv", ".mp4", ".m4v", ".avi", ".ts", ".m2ts", ".mov", ".wmv", ".webm", ".mpg", ".mpeg":
		return true
	}
	return false
}

// names splits a stream's text into the torrent name and the video file's
// name (without directories or extension). Addons put the torrent name on
// the first line and, for a file inside a pack, the file's path on the
// next; statistics lines (seeders, size, source, languages) follow.
func names(text, filename string) (torrent, file string) {
	for _, line := range strings.Split(text, "\n") {
		if isStatsLine(line) {
			continue
		}
		if line = trimDecoration(line); line == "" {
			continue
		}
		if torrent == "" {
			torrent = line
			continue
		}
		if file == "" && videoExt(line) {
			file = line
		}
	}
	if filename != "" {
		file = filename
	}
	if file != "" {
		file = path.Base(strings.ReplaceAll(file, "\\", "/"))
		if videoExt(file) {
			file = strings.TrimSuffix(file, path.Ext(file))
		}
	}
	if torrent != "" && videoExt(torrent) {
		if file == "" {
			file = strings.TrimSuffix(path.Base(torrent), path.Ext(torrent))
		}
		torrent = strings.TrimSuffix(torrent, path.Ext(torrent))
	}
	return torrent, file
}

// trimDecoration removes the emoji and symbols addons lead lines with
// ("📂 Name", "📄 file.mkv").
func trimDecoration(line string) string {
	line = strings.TrimSpace(line)
	return strings.TrimSpace(strings.TrimLeftFunc(line, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '[' && r != '('
	}))
}

// isStatsLine reports an addon's statistics or flags line.
func isStatsLine(line string) bool {
	for _, marker := range []string{"👤", "💾", "⚙️", "⚙", "🔎", "🌐", "🌎", "🔗", "📦", "🇬🇧"} {
		if strings.Contains(line, marker) {
			return true
		}
	}
	// A line of only flags or separators.
	return strings.IndexFunc(line, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) < 0
}

// pickName chooses the name that describes the video best: the file's own
// name when it identifies the title (and episode), else the torrent's. A
// file in a pack names its episode and its own quality; a foreign-language
// torrent name often names neither.
func pickName(req core.SearchRequest, file, torrent string) string {
	score := func(name string) int {
		if name == "" {
			return -1
		}
		info := releaseparse.Parse(name)
		n := 0
		if releaseparse.TitleMatches(info, req.Title) || (req.OriginalTitle != "" && releaseparse.TitleMatches(info, req.OriginalTitle)) {
			n += 4
		} else {
			for _, alt := range req.AlternativeTitles {
				if releaseparse.TitleMatches(info, alt) {
					n += 4
					break
				}
			}
		}
		if req.Type != core.TypeMovie && req.Episode > 0 && releaseparse.EpisodeMatches(info, req.Season, req.Episode) {
			n += 2
		}
		if info.Height > 0 {
			n++
		}
		return n
	}
	fs, ts := score(file), score(torrent)
	switch {
	case fs < 0 && ts < 0:
		return ""
	case fs >= ts:
		return file
	default:
		return torrent
	}
}

// describe parses name and fills the quality facts it lacks from the other
// of the stream's names: a pack's episode file often carries only the
// episode, its torrent name the resolution, source, codecs and group.
func describe(name, file, torrent string) core.ReleaseInfo {
	info := releaseparse.Parse(name)
	other := torrent
	if name == torrent {
		other = file
	}
	if other == "" || other == name {
		return info
	}
	o := releaseparse.Parse(other)
	if info.Height == 0 {
		info.Height = o.Height
	}
	if info.Source == core.SourceUnknown {
		info.Source = o.Source
	}
	if info.VideoCodec == "" {
		info.VideoCodec = o.VideoCodec
	}
	if info.AudioCodec == "" {
		info.AudioCodec, info.Channels = o.AudioCodec, o.Channels
	}
	if info.Channels == "" {
		info.Channels = o.Channels
	}
	if info.Year == 0 {
		info.Year = o.Year
	}
	if info.Group == "" {
		info.Group = o.Group
	}
	if info.Edition == "" {
		info.Edition = o.Edition
	}
	if len(info.Languages) == 0 {
		info.Languages = o.Languages
	}
	info.HDR10 = info.HDR10 || o.HDR10
	info.DolbyVision = info.DolbyVision || o.DolbyVision
	info.Atmos = info.Atmos || o.Atmos
	info.Proper = info.Proper || o.Proper
	info.Repack = info.Repack || o.Repack
	return info
}

// isSeasonPack reports a torrent name covering the whole of season.
func isSeasonPack(name string, season int) bool {
	if name == "" {
		return false
	}
	info := releaseparse.Parse(name)
	if len(info.Episodes) > 0 {
		return false
	}
	if info.FullSeason {
		return true
	}
	for _, s := range info.Seasons {
		if s == season {
			return true
		}
	}
	return false
}

var (
	seedersRe = regexp.MustCompile(`👤\s*([\d,.]+)`)
	sizeRe    = regexp.MustCompile(`(?i)💾\s*([\d.,]+)\s*([KMGT]?)i?B\b`)
)

func parseSeeders(text string) int {
	m := seedersRe.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.NewReplacer(",", "", ".", "").Replace(m[1]))
	return n
}

func parseSize(text string) int64 {
	m := sizeRe.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
	if err != nil || v <= 0 {
		return 0
	}
	shift := map[string]uint{"": 0, "K": 10, "M": 20, "G": 30, "T": 40}[strings.ToUpper(m[2])]
	return int64(v * float64(uint64(1)<<shift))
}

// streamTrackers returns the announce URLs carried into the generated
// magnet: the tr= of every original magnet (URL, ?magnet= or sources)
// plus the stream's "tracker:" sources, deduplicated and capped at
// maxTrackers. Without an original magnet it is exactly trackers(sources).
func streamTrackers(s *Stream) []string {
	var out []string
	add := func(tr string) {
		tr = strings.TrimSpace(tr)
		if tr == "" || len(out) == maxTrackers {
			return
		}
		for _, x := range out {
			if x == tr {
				return
			}
		}
		out = append(out, tr)
	}
	for _, magnet := range streamMagnets(s) {
		for _, tr := range magnetTrackers(magnet) {
			add(tr)
		}
	}
	for _, tr := range trackers(s.Sources) {
		add(tr)
	}
	return out
}

// trackers returns the announce URLs of a stream's "tracker:" sources.
func trackers(sources []string) []string {
	var out []string
	for _, s := range sources {
		if tr, ok := strings.CutPrefix(s, "tracker:"); ok && tr != "" {
			out = append(out, tr)
			if len(out) == maxTrackers {
				break
			}
		}
	}
	return out
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// hintRe matches a debrid service tag with its availability marker:
// "[RD+]" and "[RD download]", "[TB⚡]" and "[TB⬇️]", "RD ⏳". Torrentio and
// Comet tag Debrid-Link as DL and Offcloud as OC.
var hintRe = regexp.MustCompile(`(?i)\b(RD|TB|AD|PM|DL|OC)\s*(\+|⚡|download|⬇|⏳)`)

var hintKinds = map[string]string{
	"RD": config.DebridRealDebrid,
	"TB": config.DebridTorBox,
	"AD": config.DebridAllDebrid,
	"PM": config.DebridPremiumize,
	"DL": config.DebridDebridLink,
	"OC": config.DebridOffcloud,
}

// debridHints reads the cached markers an addon configured with a debrid
// key puts in a stream's name.
func debridHints(text string) (cached, uncached []string) {
	for _, idx := range hintRe.FindAllStringSubmatchIndex(text, -1) {
		tag, marker := strings.ToUpper(text[idx[2]:idx[3]]), text[idx[4]:idx[5]]
		if tag == "DL" && idx[2] > 0 && (text[idx[2]-1] == '-' || text[idx[2]-1] == '.') {
			continue // "WEB-DL+", "WEB.DL ⬇": a source tag, not Debrid-Link
		}
		kind := hintKinds[tag]
		switch marker {
		case "+", "⚡":
			if !contains(cached, kind) {
				cached = append(cached, kind)
			}
		default:
			if !contains(uncached, kind) {
				uncached = append(uncached, kind)
			}
		}
	}
	return cached, uncached
}

// httpLink returns raw when it is an absolute http(s) URL, else "".
func httpLink(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return raw
}

// requestHeaders are the headers the addon says its stream URL needs
// (behaviorHints.proxyHeaders.request), bounded and stripped of headers
// Ether controls itself.
func (s *Stream) RequestHeaders() map[string]string {
	var out map[string]string
	for k, v := range s.BehaviorHints.ProxyHeaders.Request {
		k = http.CanonicalHeaderKey(strings.TrimSpace(k))
		switch k {
		case "", "Range", "Host", "Content-Length", "Connection", "Transfer-Encoding", "Te", "Upgrade", "Proxy-Authorization", "Proxy-Connection":
			continue
		}
		if strings.ContainsAny(k+v, "\r\n\x00") || len(out) == maxStreamHeaders {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[k] = v
	}
	return out
}

// linkIdentity identifies an HTTP or NZB stream within one configured
// addon without its URL, which can carry credentials and often changes
// between answers: the file's name and size, the debrid service the addon
// labels it with, its file index or requested episode, and the link's host.
func linkIdentity(name string, size int64, link string, s *Stream, req core.SearchRequest) string {
	host := ""
	if u, err := url.Parse(link); err == nil {
		host = strings.ToLower(u.Host)
	}
	cached, uncached := debridHints(s.Name)
	h := sha1.Sum([]byte(strings.ToLower(name) + "|" + strconv.FormatInt(size, 10) + "|" + host + "|" +
		strings.Join(cached, ",") + "|" + strings.Join(uncached, ",") + "|" + guidSuffix(s, req, false)))
	return hex.EncodeToString(h[:10])
}

// streamLanguages adds the audio languages a stream's text declares
// outside its release name: flag emoji (as Torrentio prints them) and
// "Multi Audio" / "Dual Audio".
func streamLanguages(langs []string, text string) []string {
	add := func(l string) {
		for _, x := range langs {
			if x == l {
				return
			}
		}
		langs = append(langs, l)
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "multi audio") || strings.Contains(lower, "dual audio") {
		add(releaseparse.LanguageMulti)
	}
	for _, l := range releaseparse.FlagLanguages(text) {
		add(l)
	}
	return langs
}

// entryEpisodeIdentity reads an anime release numbered inside the
// database entry the addon was asked for ("Show Final Season Part 2 - 04"
// for that entry's fourth episode) as the requested episode. Only a
// season-less release whose own number is that entry episode is read so;
// every other name keeps its parsed identity.
func entryEpisodeIdentity(info core.ReleaseInfo, req core.SearchRequest, entryEpisode int) core.ReleaseInfo {
	if entryEpisode <= 0 || req.Type != core.TypeEpisode || len(info.Seasons) > 0 || info.FullSeason {
		return info
	}
	if !releaseparse.EpisodeMatchesAbsolute(info, entryEpisode) || entryEpisode == req.AbsoluteEpisode {
		return info
	}
	info.Seasons, info.Episodes = []int{req.Season}, []int{req.Episode}
	return info
}


const (
	movieCategory = 2000
	tvCategory    = 5000
	// maxResults bounds the streams converted from one answer; add-ons
	// answer popular titles with a few hundred, best first.
	maxResults = 250
	// maxTrackers bounds the trackers carried into a magnet. Debrid
	// services resolve cached torrents by hash; trackers only help the
	// rare uncached add.
	maxTrackers = 8
	// maxStreamHeaders bounds the request headers an add-on may ask for.
	maxStreamHeaders = 16
)
