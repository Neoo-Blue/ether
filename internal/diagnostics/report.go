package diagnostics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

type Finding struct {
	Code  string `json:"code"`
	Text  string `json:"text"`
	Lines []int  `json:"journalLines"`
}
type reportRow struct {
	line  int
	event exportEvent
}

// analyzeJournal consumes the same already-redacted JSONL that is written to
// the ZIP. References therefore point to actual lines, even when a record was
// dropped by the redaction check.
func analyzeJournal(body string) ([]reportRow, []Finding) {
	var rows []reportRow
	for i, line := range strings.Split(strings.TrimSuffix(body, "\n"), "\n") {
		if line == "" {
			continue
		}
		var e exportEvent
		if json.Unmarshal([]byte(line), &e) == nil {
			rows = append(rows, reportRow{i + 1, e})
		}
	}
	var findings []Finding
	type occurrence struct {
		directory string
		line      int
	}
	seen := map[string][]occurrence{}
	var errors, slow, empty, hero, cancelled, playbackFailures []int
	for _, row := range rows {
		e := row.event
		if e.Kind == KindSourceRejected || e.Kind == KindEncoderFailed || (e.Kind == KindRequest && strings.Contains(e.Route, "/decision") && e.Status >= 400) {
			playbackFailures = append(playbackFailures, row.line)
		}
		if e.Kind != KindRequest {
			continue
		}
		if e.Status >= 400 {
			errors = append(errors, row.line)
		}
		if e.DurationMS >= 2000 || e.DBTimeMS >= 500 || e.DBQueries > 300 {
			slow = append(slow, row.line)
		}
		if e.Cancelled {
			cancelled = append(cancelled, row.line)
		}
		if e.Response == nil {
			continue
		}
		q, _ := url.ParseQuery(e.Query)
		for _, h := range e.Response.Hubs {
			id := strings.ToLower(h.Identifier)
			if strings.Contains(id, "continue") && h.ItemCount == 0 && !e.Response.Truncated {
				empty = append(empty, row.line)
			}
			if strings.EqualFold(h.Style, "hero") && !strings.Contains(id, "continue") {
				hero = append(hero, row.line)
			}
			if e.Route == "/hubs/promoted" && q.Get("contentDirectoryID") != "" && h.Identifier != "" {
				seen[h.Identifier] = append(seen[h.Identifier], occurrence{q.Get("contentDirectoryID"), row.line})
			}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		occ := seen[id]
		dirs := map[string]bool{}
		lines := map[int]bool{}
		for _, o := range occ {
			dirs[o.directory] = true
			lines[o.line] = true
		}
		if len(dirs) < 2 {
			continue
		}
		nums := make([]int, 0, len(lines))
		for n := range lines {
			nums = append(nums, n)
		}
		sort.Ints(nums)
		findings = append(findings, Finding{Code: "duplicate_hub_across_directories", Text: fmt.Sprintf("Hub %q appeared in promoted responses for multiple content directories.", id), Lines: nums})
	}
	add := func(code, text string, lines []int) {
		if len(lines) > 0 {
			findings = append(findings, Finding{Code: code, Text: text, Lines: lines})
		}
	}
	add("empty_continue_watching", "A Continue Watching hub contained no items.", empty)
	add("other_hero", "A hero row other than Continue Watching was returned.", hero)
	add("slow_request", "A request exceeded a wall-time or database budget.", slow)
	add("http_error", "A request returned an HTTP error.", errors)
	add("cancelled_request", "A request context was cancelled when the handler returned.", cancelled)
	if len(playbackFailures) >= 2 {
		add("repeated_playback_failures", "Multiple playback decision or encoder failures were observed.", playbackFailures)
	}
	return rows, findings
}

func reportMarkdown(rows []reportRow, findings []Finding, extra []archived) []byte {
	var b bytes.Buffer
	b.WriteString("# Recording report\n\nThis report describes server observations. A cancelled request does not by itself prove a client timeout. Times are UTC. Line references point to events.jsonl.\n\n## Findings\n\n")
	if len(findings) == 0 {
		b.WriteString("No automated rule matched the retained requests.\n")
	}
	for _, f := range findings {
		fmt.Fprintf(&b, "- %s (journal lines %v)\n", f.Text, f.Lines)
	}
	for _, file := range extra {
		switch file.name {
		case "server.json":
			var server struct {
				Version   string `json:"version"`
				Commit    string `json:"commit"`
				BuildDate string `json:"buildDate"`
				Platform  string `json:"platform"`
				Container string `json:"container"`
				Uptime    int64  `json:"uptimeSeconds"`
			}
			if json.Unmarshal(file.body, &server) == nil {
				fmt.Fprintf(&b, "\n## Server\n\nVersion %s, commit %s, build date %s, platform %s, container %s, uptime %d seconds. Empty values were not available.\n", strconv.Quote(server.Version), strconv.Quote(server.Commit), strconv.Quote(server.BuildDate), strconv.Quote(server.Platform), strconv.Quote(server.Container), server.Uptime)
			}
		case "storage.json":
			var storage StorageFact
			if json.Unmarshal(file.body, &storage) == nil {
				fmt.Fprintf(&b, "\n## Storage\n\nDatabase %d bytes; WAL %d bytes; filesystem %s; free space %d bytes (known: %t).\n", storage.DatabaseBytes, storage.WALBytes, strconv.Quote(storage.Filesystem), storage.DiskFreeBytes, storage.DiskFreeKnown)
			}
		}
	}
	for _, file := range extra {
		if file.name != "viewer.json" {
			continue
		}
		var v ViewerFacts
		if json.Unmarshal(file.body, &v) != nil {
			continue
		}
		fmt.Fprintf(&b, "\n## Viewer snapshot\n\nSampled %d watch states: %d in progress and %d watched; sample truncated: %t. Candidate list truncated: %t.\n", v.WatchSample, v.InProgress, v.Watched, v.WatchSampleTruncated, v.CandidatesTruncated)
		for _, c := range v.Candidates {
			fmt.Fprintf(&b, "- %s (key %d, offset %d ms, included: %t)\n", c.Reason, c.Key, c.OffsetMS, c.Included)
			if c.Title != "" {
				fmt.Fprintf(&b, "  - item: %s\n", strconv.Quote(c.Title))
			}
		}
	}
	b.WriteString("\n## Recorded requests in order\n\n")
	for _, row := range rows {
		if b.Len() > 400<<10 {
			b.WriteString("\nReport shortened; see events.jsonl for the remaining records.\n")
			break
		}
		e := row.event
		if e.Kind != KindRequest {
			continue
		}
		fmt.Fprintf(&b, "- Line %d, %s: %s %s", row.line, e.At.UTC().Format("15:04:05.000"), e.Method, e.Route)
		if e.Query != "" {
			fmt.Fprintf(&b, "?%s", e.Query)
		}
		fmt.Fprintf(&b, " → HTTP %d, %d ms, %d bytes, %d database statements in %d ms.\n", e.Status, e.DurationMS, e.Bytes, e.DBQueries, e.DBTimeMS)
		if e.Product != "" || e.Platform != "" || e.DeviceType != "" || e.Model != "" || e.AppVersion != "" {
			fmt.Fprintf(&b, "  - Client: product %s, version %s, platform %s, device %s, model %s.\n", strconv.Quote(e.Product), strconv.Quote(e.AppVersion), strconv.Quote(e.Platform), strconv.Quote(e.DeviceType), strconv.Quote(e.Model))
		}
		if e.Response == nil {
			continue
		}
		fmt.Fprintf(&b, "  - Container size %d", e.Response.Size)
		if e.Response.TotalSize != nil {
			fmt.Fprintf(&b, ", total %d", *e.Response.TotalSize)
		}
		if e.Response.Truncated {
			b.WriteString(" (summary truncated)")
		}
		b.WriteString(".\n")
		for _, h := range e.Response.Hubs {
			fmt.Fprintf(&b, "  - Hub %s: style %s, type %s, size %d, %d items, more %t.\n", strconv.Quote(h.Identifier), strconv.Quote(h.Style), strconv.Quote(h.Type), h.Size, h.ItemCount, h.More)
			for _, title := range h.Items {
				fmt.Fprintf(&b, "  - item: %s\n", strconv.Quote(title))
			}
		}
		for _, title := range e.Response.Items {
			fmt.Fprintf(&b, "  - item: %s\n", strconv.Quote(title))
		}
	}
	return b.Bytes()
}
