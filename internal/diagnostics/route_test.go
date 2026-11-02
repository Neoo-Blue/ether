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
