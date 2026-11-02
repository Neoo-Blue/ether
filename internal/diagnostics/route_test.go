package diagnostics

import (
	"strings"
	"testing"
	"time"
)

func TestRequestRouteMasksIdentifyingSegments(t *testing.T) {
	for in, want := range map[string]string{
		"/hubs/promoted": "/hubs/promoted",
		"/hubs/items/home.because.2000000000000688":          "/hubs/items/home.because.2000000000000688",
		"/library/metadata/1000000001571955/children":        "/library/metadata/1000000001571955/children",
		"/tv.plex.providers.epg.xmltv:1/watchnow/all":        "/tv.plex.providers.epg.xmltv:1/watchnow/all",
		"/Items/4f1c2a9be0d34c5a8e1f2b3c4d5e6f70/Images":     "/Items/*/Images",
		"/eyJkZWJyaWQiOiJhYmMxMjMifQ/stream/tt1234.json":     "/*/stream/*",
		"/library/parts/12/1700000000/Some Movie (2024).mkv": "/library/parts/12/1700000000/*",
		"relative/path": "",
	} {
		if got := requestRoute(in); got != want {
			t.Errorf("requestRoute(%q) = %q, want %q", in, got, want)
		}
	}
}

// A Home problem is a run of requests: the capture must say which routes the
// device asked for and how long each took, not only that they returned 200
// (#238).
func TestCaptureKeepsRequestRoutes(t *testing.T) {
	now := time.Date(2026, 11, 2, 16, 10, 0, 0, time.UTC)
	q := LogQuery{From: now.Add(-time.Minute), Until: now.Add(time.Minute), DeviceKey: "dev-1", MaxLines: 10}
	rec, ok := ParseLogLine(`time=2026-11-02T16:10:00Z level=DEBUG msg=request method=GET path=/hubs/promoted status=200 dur=84ms pageStart=0 pageSize=12 client=dev-1 device=iPhone peer=192.0.2.1`)
	if !ok {
		t.Fatal("parse")
	}
	other := LogRecord{Time: now, Level: "INFO", Message: "playback", Attrs: map[string]string{"client": "dev-1", "path": "/tmp/library/movie.mkv", "method": "GET"}}
	body, _, _, _ := projectLogs([]LogRecord{rec, other}, q, [3]bool{}, nil)
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines %q", body)
	}
	for _, want := range []string{`"method":"GET"`, `"route":"/hubs/promoted"`, `"dur":"84ms"`, `"pageStart":"0"`, `"pageSize":"12"`} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("request line %s lacks %s", lines[0], want)
		}
	}
	if strings.Contains(string(body), "/tmp/library") || strings.Contains(lines[1], "route") {
		t.Fatalf("a non-request path was exported: %s", body)
	}
}

// A capture is useful without the owner changing the log level: each line
// keeps its message, and with titles included its error, minus URL queries
// where addon and debrid keys live (#238, #262).
func TestCaptureKeepsMessagesAndErrors(t *testing.T) {
	now := time.Date(2026, 11, 2, 17, 38, 22, 0, time.UTC)
	q := LogQuery{From: now.Add(-time.Minute), Until: now.Add(time.Minute), DeviceKey: "dev-1", MaxLines: 10}
	rec := LogRecord{Time: now, Level: "WARN", Message: "release failed to open", Attrs: map[string]string{
		"client": "dev-1", "release": "Sleepers.S01E01",
		"err": "open Sleepers.S01E01: stream from https://comet.example/abc/playback?token=s3cret is 433923 bytes",
	}}
	body, _, _, _ := projectLogs([]LogRecord{rec}, q, [3]bool{true, false, false}, nil)
	line := string(body)
	for _, want := range []string{`"msg":"release failed to open"`, `"err":"open Sleepers.S01E01: stream from https://comet.example/abc/playback is 433923 bytes"`} {
		if !strings.Contains(line, want) {
			t.Errorf("titles on: %s lacks %s", line, want)
		}
	}
	if strings.Contains(line, "s3cret") {
		t.Fatalf("URL query leaked: %s", line)
	}
	body, _, _, _ = projectLogs([]LogRecord{rec}, q, [3]bool{}, nil)
	if line = string(body); !strings.Contains(line, `"msg":"release failed to open"`) || strings.Contains(line, "Sleepers") {
		t.Fatalf("titles off: %s", line)
	}
}
