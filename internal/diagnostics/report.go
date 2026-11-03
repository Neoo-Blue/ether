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
	Code      string `json:"code"`
	Text      string `json:"text"`
	Lines     []int  `json:"journalLines"`
	Truncated bool   `json:"truncated,omitempty"`
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
	failureLines := map[int]bool{}
	addFailure := func(line int) {
		if !failureLines[line] {
			failureLines[line] = true
			playbackFailures = append(playbackFailures, line)
		}
	}
	for _, row := range rows {
		e := row.event
		if e.Kind == KindRequest && e.Status >= 400 && (strings.Contains(strings.ToLower(e.Route), "/decision") || strings.Contains(strings.ToLower(e.Route), "playback")) {
			addFailure(row.line)
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
		if e.Response.ErrorCode != "" {
			addFailure(row.line)
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
			directory := q.Get("contentDirectoryID")
			if e.Route == "/hubs/promoted" && directory != "" && directory != "[redacted]" && !strings.Contains(directory, ",") && h.Identifier != "" {
				seen[h.Identifier] = append(seen[h.Identifier], occurrence{directory, row.line})
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
		add("repeated_playback_failures", "Multiple playback decision requests failed.", playbackFailures)
	}
	if len(findings) > 128 {
		findings = append(findings[:128], Finding{Code: "findings_truncated", Text: "Additional findings were omitted to keep the report bounded.", Truncated: true})
	}
	for i := range findings {
		if len(findings[i].Lines) > 64 {
			findings[i].Lines = findings[i].Lines[:64]
			findings[i].Truncated = true
		}
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
		fmt.Fprintf(&b, "- %s (journal lines %v)", f.Text, f.Lines)
		if f.Truncated {
			b.WriteString(" (additional detail omitted)")
		}
		b.WriteByte('\n')
	}
	for _, file := range extra {
		switch file.name {
		case "server.json":
			var server struct {
				Version     string `json:"version"`
				Commit      string `json:"commit"`
				BuildDate   string `json:"buildDate"`
				Platform    string `json:"platform"`
				Container   string `json:"container"`
				InstallType string `json:"installType"`
				Uptime      int64  `json:"uptimeSeconds"`
			}
			if json.Unmarshal(file.body, &server) == nil {
				fmt.Fprintf(&b, "\n## Server\n\nVersion %s, commit %s, build date %s, platform %s, install type %s, container %s, uptime %d seconds. Empty values were not available.\n", strconv.Quote(server.Version), strconv.Quote(server.Commit), strconv.Quote(server.BuildDate), strconv.Quote(server.Platform), strconv.Quote(server.InstallType), strconv.Quote(server.Container), server.Uptime)
			}
		case "storage.json":
			var storage StorageFact
			if json.Unmarshal(file.body, &storage) == nil {
				fmt.Fprintf(&b, "\n## Storage\n\nDatabase %d bytes (known: %t); WAL %d bytes (known: %t); filesystem %s; free space %d bytes (known: %t).\n", storage.DatabaseBytes, storage.DatabaseKnown, storage.WALBytes, storage.WALKnown, strconv.Quote(storage.Filesystem), storage.DiskFreeBytes, storage.DiskFreeKnown)
				fmt.Fprintf(&b, "Cache filesystem %s; free space %d bytes (known: %t); configured cache budget %d bytes.\n", strconv.Quote(storage.CacheFilesystem), storage.CacheFreeBytes, storage.CacheFreeKnown, storage.CacheBudgetBytes)
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
		if e.Protocol != "" || e.Product != "" || e.Platform != "" || e.DeviceType != "" || e.Model != "" || e.AppVersion != "" {
			fmt.Fprintf(&b, "  - Client: protocol %s, product %s, version %s, platform %s, device %s, model %s.\n", strconv.Quote(e.Protocol), strconv.Quote(e.Product), strconv.Quote(e.AppVersion), strconv.Quote(e.Platform), strconv.Quote(e.DeviceType), strconv.Quote(e.Model))
		}
		if e.Response == nil {
			continue
		}
		if e.Response.Decision != "" {
			fmt.Fprintf(&b, "  - Playback decision: %s.\n", strconv.Quote(e.Response.Decision))
		}
		if e.Response.ErrorCode != "" {
			fmt.Fprintf(&b, "  - Error code: %s.\n", strconv.Quote(e.Response.ErrorCode))
		}
		if e.Response.Error != "" {
			fmt.Fprintf(&b, "  - Error: %s.\n", strconv.Quote(e.Response.Error))
		}
		fmt.Fprintf(&b, "  - Container size %d", e.Response.Size)
		if e.Response.ItemCount > 0 || len(e.Response.Hubs) == 0 {
			fmt.Fprintf(&b, ", %d direct items", e.Response.ItemCount)
		}
		if e.Response.TotalSize != nil {
			fmt.Fprintf(&b, ", total %d", *e.Response.TotalSize)
		}
		if e.Response.More != nil {
			fmt.Fprintf(&b, ", more %t", *e.Response.More)
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
	for _, file := range extra {
		if file.name != "logs/capture.log" {
			continue
		}
		printed := 0
		for i, line := range bytes.Split(file.body, []byte("\n")) {
			var entry exportLog
			if json.Unmarshal(line, &entry) != nil || (!strings.EqualFold(entry.Level, "WARN") && !strings.EqualFold(entry.Level, "ERROR")) {
				continue
			}
			if printed == 0 {
				b.WriteString("\n## Server warnings and errors\n\n")
			}
			if printed == 64 {
				b.WriteString("Additional log records are in logs/capture.log.\n")
				break
			}
			fmt.Fprintf(&b, "- %s: %s", entry.At.UTC().Format("15:04:05.000"), strconv.Quote(entry.Msg))
			if entry.Route != "" {
				fmt.Fprintf(&b, " on %s", strconv.Quote(entry.Route))
			}
			if entry.Dur != "" {
				fmt.Fprintf(&b, " (%s)", strconv.Quote(entry.Dur))
			}
			fmt.Fprintf(&b, " (logs/capture.log line %d).\n", i+1)
			printed++
		}
	}
	return b.Bytes()
}
