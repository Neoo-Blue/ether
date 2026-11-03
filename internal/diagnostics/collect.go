package diagnostics

// collect runs archive collectors outside the recorder lock. A failed
// collector becomes a gap and does not stop the ZIP.
func (r *Recorder) collect(snap finishSnap, uses []secretUse) ([]archived, []Gap) {
	var files []archived
	var gaps []Gap
	put := func(name string, v any) {
		body, err := marshalChecked(v, uses)
		if err != nil {
			gaps = append(gaps, Gap{Collector: name, Status: "omitted", Detail: "Omitted because the redaction check did not accept the file."})
			return
		}
		files = append(files, archived{name: name, body: body})
	}
	server := map[string]any{
		"version":     redactString(r.deps.Version, uses),
		"commit":      redactString(r.deps.Commit, uses),
		"buildDate":   redactString(r.deps.BuildDate, uses),
		"platform":    redactString(r.deps.Platform, uses),
		"container":   redactString(r.deps.Container, uses),
		"installType": redactString(r.deps.InstallType, uses),
	}
	if r.deps.Uptime != nil {
		server["uptimeSeconds"] = int64(r.deps.Uptime().Seconds())
	}
	put("server.json", server)
	if snap.viewerAmbiguous {
		gaps = append(gaps, Gap{Collector: "viewer", Status: "unavailable", Detail: "Multiple viewers used this device during the recording; viewer state was not selected."})
	} else if snap.viewerID == 0 || r.deps.Viewer == nil {
		gaps = append(gaps, Gap{Collector: "viewer", Status: "unavailable", Detail: "No authenticated viewer was observed for this device."})
	} else if v, err := r.deps.Viewer(snap.viewerID); err != nil {
		gaps = append(gaps, Gap{Collector: "viewer", Status: "failed", Detail: "Viewer state could not be collected."})
	} else {
		for i := range v.Sections {
			if !snap.flags[0] {
				v.Sections[i].Title = ""
			} else {
				v.Sections[i].Title = clipNetwork(v.Sections[i].Title, maxString, uses)
			}
			v.Sections[i].Type = clipFree(v.Sections[i].Type, 40, uses)
		}
		for i := range v.Candidates {
			if !snap.flags[0] {
				v.Candidates[i].Title = ""
			} else {
				v.Candidates[i].Title = clipNetwork(v.Candidates[i].Title, maxString, uses)
			}
			v.Candidates[i].Reason = clipFree(v.Candidates[i].Reason, maxString, uses)
		}
		for i := range v.Layout {
			v.Layout[i].Layer = clipFree(v.Layout[i].Layer, 40, uses)
			v.Layout[i].Section = clipFree(v.Layout[i].Section, 40, uses)
			v.Layout[i].Row = clipNetwork(v.Layout[i].Row, maxString, uses)
			v.Layout[i].Status = clipFree(v.Layout[i].Status, 40, uses)
		}
		put("viewer.json", v)
	}
	if r.deps.Settings == nil {
		gaps = append(gaps, Gap{Collector: "settings", Status: "unavailable", Detail: "Settings were not collected."})
	} else if s, err := r.deps.Settings(); err != nil {
		gaps = append(gaps, Gap{Collector: "settings", Status: "failed", Detail: "Settings could not be collected."})
	} else {
		put("settings.json", redactSettings(s, snap.flags[0], uses))
	}
	if r.deps.Sessions == nil {
		gaps = append(gaps, Gap{Collector: "sessions", Status: "unavailable", Detail: "Sessions were not collected."})
	} else if rows, omitted, err := r.deps.Sessions(snap.deviceKey); err != nil {
		gaps = append(gaps, Gap{Collector: "sessions", Status: "failed", Detail: "Sessions could not be collected."})
	} else {
		put("sessions.json", sessionDoc(rows, omitted, snap.flags[0], uses))
	}
	if r.deps.Hardware == nil {
		gaps = append(gaps, Gap{Collector: "hardware", Status: "unavailable", Detail: "Hardware was not collected."})
	} else if h, err := r.deps.Hardware(); err != nil {
		gaps = append(gaps, Gap{Collector: "hardware", Status: "failed", Detail: "Hardware could not be collected."})
	} else {
		h.Name = redactString(h.Name, uses)
		h.FFmpeg = redactString(h.FFmpeg, uses)
		put("hardware.json", h)
	}
	if r.deps.Storage == nil {
		gaps = append(gaps, Gap{Collector: "storage", Status: "unavailable", Detail: "Storage was not collected."})
	} else if s, err := r.deps.Storage(); err != nil {
		gaps = append(gaps, Gap{Collector: "storage", Status: "failed", Detail: "Storage could not be collected."})
	} else {
		s.Filesystem = clipFree(s.Filesystem, 40, uses)
		s.CacheFilesystem = clipFree(s.CacheFilesystem, 40, uses)
		put("storage.json", s)
	}
	if r.deps.Connectivity == nil {
		gaps = append(gaps, Gap{Collector: "connectivity", Status: "unavailable", Detail: "Connectivity was not collected."})
	} else if c, err := r.deps.Connectivity(); err != nil {
		gaps = append(gaps, Gap{Collector: "connectivity", Status: "failed", Detail: "Connectivity could not be collected."})
	} else {
		c.SecureConnections = redactString(c.SecureConnections, uses)
		put("connectivity.json", c)
	}
	gaps = append(gaps, Gap{Collector: "logs", Status: "unavailable", Detail: "Rotated logs were not read."})
	q := logQuery(snap)
	if (q.DeviceKey == "" && len(q.SessionKeys) == 0 && len(q.AttemptKeys) == 0) || r.deps.LogExtract == nil {
		gaps = append(gaps, Gap{Collector: "logs", Status: "unavailable", Detail: "No correlated log extract was available."})
		return files, gaps
	}
	records, _, err := r.deps.LogExtract(q)
	if err != nil {
		gaps = append(gaps, Gap{Collector: "logs", Status: "failed", Detail: "Logs could not be collected."})
		return files, gaps
	}
	body, _, _, logGaps := projectLogs(records, q, snap.flags, uses)
	gaps = append(gaps, logGaps...)
	if leaks(body, uses) {
		gaps = append(gaps, Gap{Collector: "logs/capture.log", Status: "omitted", Detail: "Omitted because the redaction check did not accept the file."})
		return files, gaps
	}
	if len(body) > 0 {
		files = append(files, archived{name: "logs/capture.log", body: body})
	}
	return files, gaps
}

func redactSettings(s Settings, titles bool, uses []secretUse) Settings {
	s.LogLevel = redactString(s.LogLevel, uses)
	s.FriendlyName = clipNetwork(s.FriendlyName, maxString, uses)
	s.QualityDefault = redactString(s.QualityDefault, uses)
	s.QualityRemote = redactString(s.QualityRemote, uses)
	s.ProfileName = clipNetwork(s.ProfileName, maxString, uses)
	s.HWAccel = redactString(s.HWAccel, uses)
	s.SecureConnections = redactString(s.SecureConnections, uses)
	s.ReleaseMode = clipFree(s.ReleaseMode, 40, uses)
	s.VisibilityDefault = clipFree(s.VisibilityDefault, 40, uses)
	for i := range s.CatalogRules {
		r := &s.CatalogRules[i]
		r.Action = clipNetwork(r.Action, 40, uses)
		r.MediaType = clipNetwork(r.MediaType, 40, uses)
		r.Match = clipNetwork(r.Match, 40, uses)
		r.Scope = clipNetwork(r.Scope, 40, uses)
		for j := range r.Conditions {
			c := &r.Conditions[j]
			c.Field = clipNetwork(c.Field, 80, uses)
			c.Op = clipNetwork(c.Op, 40, uses)
			if titles {
				if len(c.Value) > maxString {
					s.CatalogRulesTruncated = true
				}
				c.Value = clipNetwork(c.Value, maxString, uses)
			} else {
				c.Value = ""
			}
		}
	}
	return s
}

type sessionRow struct {
	State    string `json:"state,omitempty"`
	Decision string `json:"decision,omitempty"`
	Quality  string `json:"quality,omitempty"`
	Bytes    int64  `json:"bytes,omitempty"`
	Title    string `json:"title,omitempty"`
}

func sessionDoc(rows []SessionFact, omitted int, titles bool, uses []secretUse) map[string]any {
	out := make([]sessionRow, 0, len(rows))
	for _, row := range rows {
		item := sessionRow{
			State:    redactString(row.State, uses),
			Decision: redactString(row.Decision, uses),
			Quality:  redactString(row.Quality, uses),
			Bytes:    row.Bytes,
		}
		if titles {
			item.Title = clipNetwork(row.Title, maxString, uses)
		}
		out = append(out, item)
	}
	return map[string]any{"omittedOtherDevices": omitted, "sessions": out}
}
