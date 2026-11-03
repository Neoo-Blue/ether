package plex

import (
	"encoding/json"
	"encoding/xml"
	"net/url"
	"regexp"
	"strings"

	"github.com/braedonsaunders/ether/internal/diagnostics"
)

// Query values whose syntax and meaning are known not to contain credentials.
// Unknown keys are retained, but their values are masked: clients can send
// arbitrary credentials in extension parameters.
var supportQueryValue = regexp.MustCompile(`^[A-Za-z0-9_,-]{0,256}$`)
var supportDirectoryValue = regexp.MustCompile(`^(?:[0-9]+|playlists)(?:,(?:[0-9]+|playlists))*$`)
var supportNumericValue = regexp.MustCompile(`^[0-9]{1,10}$`)
var supportQueryKeys = map[string]bool{
	"contentdirectoryid": true, "pinnedcontentdirectoryid": true,
	"excludecontinuewatching": true, "count": true, "excludeelements": true,
	"excludefields": true, "excludemusic": true, "excludephotos": true,
	"includerecentchannels": true, "includedetails": true, "includemeta": true,
	"includeondeck": true, "includecollections": true, "includestations": true,
	"includeexternalmetadata": true, "includelibraryplaylists": true,
	"x-plex-container-start": true, "x-plex-container-size": true,
}

func supportQuery(q url.Values) string {
	out := url.Values{}
	for key, values := range q {
		// Keys are untrusted too; do not reflect arbitrary text into the ZIP.
		if !supportQueryValue.MatchString(key) || len(key) > 80 {
			continue
		}
		safe := supportQueryKeys[strings.ToLower(key)]
		if !safe {
			out.Add("[redacted-key]", "[redacted]")
			continue
		}
		for _, v := range values {
			name := strings.ToLower(key)
			valid := supportNumericValue.MatchString(v)
			if name == "contentdirectoryid" || name == "pinnedcontentdirectoryid" {
				valid = supportDirectoryValue.MatchString(v)
			}
			if name == "excludeelements" || name == "excludefields" {
				valid = supportQueryValue.MatchString(v)
			}
			if !safe || !valid {
				v = "[redacted]"
			}
			out.Add(key, v)
		}
	}
	return out.Encode()
}

type journalItem struct {
	Title string `json:"title" xml:"title,attr"`
}
type journalHub struct {
	Identifier string        `json:"hubIdentifier" xml:"hubIdentifier,attr"`
	Title      string        `json:"title" xml:"title,attr"`
	Style      string        `json:"style" xml:"style,attr"`
	Type       string        `json:"type" xml:"type,attr"`
	Size       int           `json:"size" xml:"size,attr"`
	More       flexibleBool  `json:"more" xml:"more,attr"`
	Metadata   []journalItem `json:"Metadata" xml:"Metadata"`
	Directory  []journalItem `json:"Directory" xml:"Directory"`
	Videos     []journalItem `json:"-" xml:"Video"`
	Tracks     []journalItem `json:"-" xml:"Track"`
}
type journalContainer struct {
	Size      int           `json:"size" xml:"size,attr"`
	TotalSize *int          `json:"totalSize" xml:"totalSize,attr"`
	Hubs      []journalHub  `json:"Hub" xml:"Hub"`
	Metadata  []journalItem `json:"Metadata" xml:"Metadata"`
	Directory []journalItem `json:"Directory" xml:"Directory"`
	Videos    []journalItem `json:"-" xml:"Video"`
	Tracks    []journalItem `json:"-" xml:"Track"`
}
type flexibleBool bool

func (b *flexibleBool) UnmarshalJSON(p []byte) error {
	*b = string(p) == "true" || string(p) == "1"
	return nil
}
func (b *flexibleBool) UnmarshalXMLAttr(a xml.Attr) error {
	*b = a.Value == "1" || a.Value == "true"
	return nil
}
func journalTitles(a, b []journalItem) []string {
	out := make([]string, 0, len(a)+len(b))
	for _, x := range append(a, b...) {
		if x.Title != "" {
			out = append(out, x.Title)
		}
	}
	return out
}
func summarizePlexResponse(raw []byte, truncated bool) *diagnostics.ResponseSummary {
	if truncated {
		return &diagnostics.ResponseSummary{Truncated: true}
	}
	var c journalContainer
	var doc struct {
		MediaContainer journalContainer `json:"MediaContainer"`
	}
	if json.Unmarshal(raw, &doc) == nil {
		c = doc.MediaContainer
	} else if xml.Unmarshal(raw, &c) != nil {
		return nil
	}
	c.Metadata = append(c.Metadata, c.Videos...)
	c.Metadata = append(c.Metadata, c.Tracks...)
	out := &diagnostics.ResponseSummary{Size: c.Size, TotalSize: c.TotalSize, Items: journalTitles(c.Metadata, c.Directory)}
	for _, h := range c.Hubs {
		h.Metadata = append(h.Metadata, h.Videos...)
		h.Metadata = append(h.Metadata, h.Tracks...)
		out.Hubs = append(out.Hubs, diagnostics.HubSummary{Identifier: h.Identifier, Title: h.Title, Style: h.Style, Type: h.Type, Size: h.Size, More: bool(h.More), ItemCount: len(h.Metadata) + len(h.Directory), Items: journalTitles(h.Metadata, h.Directory)})
	}
	return out
}
