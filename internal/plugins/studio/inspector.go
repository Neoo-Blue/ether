package studio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/wasm"
	"github.com/braedonsaunders/ether/internal/viewspec"
)

// ValidationHint is a plain-language inspector diagnostic on a plugin call,
// paired with a concrete suggested fix (SPEC WS11b).
type ValidationHint struct {
	CallID       int64  `json:"callId,omitempty"`
	Route        string `json:"route"`
	Severity     string `json:"severity"` // "warning" | "error" | "info"
	Message      string `json:"message"`
	SuggestedFix string `json:"suggestedFix"`
	Field        string `json:"field,omitempty"`
}

// InspectCall analyzes one recorded plugin call and returns plain-language
// validation hints with suggested fixes (SPEC WS11b).
func InspectCall(call plugins.CallRecord) []ValidationHint {
	var hints []ValidationHint
	add := func(severity, field, msg, fix string) {
		hints = append(hints, ValidationHint{
			CallID:       call.ID,
			Route:        call.Route,
			Severity:     severity,
			Field:        field,
			Message:      msg,
			SuggestedFix: fix,
		})
	}

	if call.Error != "" {
		add(
			"error",
			"runtime",
			fmt.Sprintf("Call to %s failed: %s.", call.Route, call.Error),
			"Check that your handle(req) function returns { status: 200, body: ... } before the route deadline.",
		)
		return hints
	}

	if call.Status >= 400 {
		add(
			"warning",
			"status",
			fmt.Sprintf("Route %s returned HTTP %d.", call.Route, call.Status),
			"Add a branch in handle(req) for this route path or return status 200 with a JSON body.",
		)
	}

	deadline := plugins.DeadlineFor(call.Route).Milliseconds()
	if deadline > 0 && call.MS > deadline/2 && call.MS > 500 {
		add(
			"warning",
			"timing",
			fmt.Sprintf("Call to %s took %d ms, which is over half of its %d ms budget.", call.Route, call.MS, deadline),
			"Cache upstream responses in ether.kv or move heavy work into a scheduled job.",
		)
	}

	if strings.TrimSpace(call.Response) == "" {
		return hints
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(call.Response), &body); err != nil {
		return hints
	}

	switch {
	case strings.HasPrefix(call.Route, "/catalog/"):
		metas, _ := body["metas"].([]any)
		for i, raw := range metas {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id, _ := item["id"].(string)
			name, _ := item["name"].(string)
			if name == "" {
				name = fmt.Sprintf("item %d", i+1)
			}
			if !strings.HasPrefix(id, "tt") && !strings.HasPrefix(id, "tmdb:") {
				add(
					"warning",
					fmt.Sprintf("metas[%d].id", i),
					fmt.Sprintf("Item %d (%s) has no IMDb or TMDB id.", i+1, name),
					"Set id to an IMDb id starting with tt (such as tt0133093) or a TMDB id starting with tmdb: so Ether can match it to your catalogue.",
				)
			}
		}

	case call.Route == "/ether/channels.json":
		channels, _ := body["channels"].([]any)
		if len(channels) == 0 {
			add(
				"warning",
				"channels",
				"The channel list is empty.",
				"Return a non-empty channels array from /ether/channels.json so Live TV can populate the guide.",
			)
		}
		for i, raw := range channels {
			ch, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name, _ := ch["name"].(string)
			if name == "" {
				name = fmt.Sprintf("channel %d", i+1)
			}
			numStr := fmt.Sprint(ch["number"])
			if ch["number"] == nil || numStr == "" {
				add(
					"info",
					fmt.Sprintf("channels[%d].number", i),
					fmt.Sprintf("Channel %d (%s) has no channel number.", i+1, name),
					"Add a number field (such as \"101\") so the channel keeps a fixed position in the guide.",
				)
			}
			logo, _ := ch["logo"].(string)
			if strings.TrimSpace(logo) == "" {
				add(
					"info",
					fmt.Sprintf("channels[%d].logo", i),
					fmt.Sprintf("Channel %d (%s) has no logo link.", i+1, name),
					"Add an SVG or PNG logo URL so the guide renders channel art instead of a text badge.",
				)
			}
			guideID, _ := ch["guideId"].(string)
			if strings.TrimSpace(guideID) == "" {
				add(
					"info",
					fmt.Sprintf("channels[%d].guideId", i),
					fmt.Sprintf("Channel %d (%s) has no guideId.", i+1, name),
					"Set guideId so programme schedules attach to this channel without fuzzy name matching.",
				)
			}
		}

	case strings.HasPrefix(call.Route, "/ether/view/"):
		specRaw, hasSpec := body["spec"]
		if !hasSpec {
			add(
				"error",
				"spec",
				"View response is missing the spec field.",
				"Return { spec: { specVersion: 1, layout: \"detail\", header: [...], body: [...] }, data: {...} }.",
			)
		} else if specBytes, err := json.Marshal(specRaw); err == nil {
			if res := viewspec.ValidatePlugin(specBytes); !res.OK && len(res.Errors) > 0 {
				add(
					"error",
					"spec",
					fmt.Sprintf("ViewSpec validation failed: %s.", res.Errors[0]),
					"Use only closed ViewSpec v1 blocks and Ether widgets, and remove any className fields.",
				)
			}
		}

	case strings.HasPrefix(call.Route, "/stream/") || strings.HasPrefix(call.Route, "/ether/streams"):
		streams, _ := body["streams"].([]any)
		for i, raw := range streams {
			st, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			urlStr, _ := st["url"].(string)
			infoHash, _ := st["infoHash"].(string)
			if strings.TrimSpace(urlStr) == "" && strings.TrimSpace(infoHash) == "" {
				add(
					"error",
					fmt.Sprintf("streams[%d].url", i),
					fmt.Sprintf("Stream %d is missing both url and infoHash.", i+1),
					"Provide a direct HTTPS stream url or an infoHash so Ether can play this stream.",
				)
			}
			title, _ := st["title"].(string)
			name, _ := st["name"].(string)
			etherObj, _ := st["ether"].(map[string]any)
			relName := ""
			if etherObj != nil {
				relName, _ = etherObj["releaseName"].(string)
			}
			if strings.TrimSpace(title) == "" && strings.TrimSpace(relName) == "" && strings.TrimSpace(name) == "" {
				add(
					"warning",
					fmt.Sprintf("streams[%d].ether.releaseName", i),
					fmt.Sprintf("Stream %d has no release name for Ether's parser.", i+1),
					"Provide ether.releaseName or title so Ether can extract resolution, HDR and codec facts.",
				)
			}
		}
	}

	return hints
}

// InspectCalls aggregates plain-language hints across a list of plugin calls.
func InspectCalls(calls []plugins.CallRecord) []ValidationHint {
	var out []ValidationHint
	for _, c := range calls {
		out = append(out, InspectCall(c)...)
	}
	return out
}

// RedactJSON masks values under secret-looking keys before storing in the
// Studio inspector ring.
func RedactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	redactAny(v)
	out, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(out)
}

func redactAny(v any) {
	switch node := v.(type) {
	case map[string]any:
		for k, val := range node {
			lk := strings.ToLower(k)
			if strings.Contains(lk, "secret") ||
				strings.Contains(lk, "token") ||
				strings.Contains(lk, "password") ||
				strings.Contains(lk, "apikey") ||
				strings.Contains(lk, "api_key") ||
				strings.Contains(lk, "authorization") {
				node[k] = "[redacted]"
				continue
			}
			redactAny(val)
		}
	case []any:
		for _, item := range node {
			redactAny(item)
		}
	}
}

// locateJSONField finds the 1-based (line, column) of a JSON field path in raw JSON text.
func locateJSONField(text, fieldPath string) (int, int) {
	parts := strings.Split(fieldPath, ".")
	for i := len(parts); i > 0; i-- {
		seg := parts[i-1]
		if idx := strings.IndexByte(seg, '['); idx > 0 {
			seg = seg[:idx]
		}
		if seg == "" {
			continue
		}
		needle := `"` + seg + `"`
		if pos := strings.Index(text, needle); pos >= 0 {
			return byteOffsetToLineCol(text, pos)
		}
	}
	return 1, 1
}

func jsonSyntaxDiagnostic(file, text string, err error) wasm.Diagnostic {
	line, col := 1, 1
	var syn *json.SyntaxError
	if ok := asSyntaxError(err, &syn); ok && syn.Offset > 0 {
		line, col = byteOffsetToLineCol(text, int(syn.Offset)-1)
	}
	return wasm.Diagnostic{
		File:     file,
		Line:     line,
		Column:   col,
		Text:     err.Error(),
		Severity: "error",
	}
}

func asSyntaxError(err error, target **json.SyntaxError) bool {
	if s, ok := err.(*json.SyntaxError); ok {
		*target = s
		return true
	}
	return false
}

func byteOffsetToLineCol(text string, offset int) (int, int) {
	if offset < 0 {
		return 1, 1
	}
	if offset > len(text) {
		offset = len(text)
	}
	prefix := text[:offset]
	line := bytes.Count([]byte(prefix), []byte("\n")) + 1
	lastNL := strings.LastIndexByte(prefix, '\n')
	col := offset - lastNL
	if col < 1 {
		col = 1
	}
	return line, col
}
