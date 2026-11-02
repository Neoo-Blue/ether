package etherplugin

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// TemplateOptions configures a capability starter template.
type TemplateOptions struct {
	ID          string
	Name        string
	Version     string
	Summary     string
	Description string
}

func baseManifest(opts TemplateOptions, kind PluginKind, caps []Capability) Manifest {
	version := opts.Version
	if version == "" {
		version = "0.1.0"
	}
	summary := opts.Summary
	if summary == "" {
		summary = opts.Name + " plugin for Ether."
	}
	desc := opts.Description
	if desc == "" {
		desc = summary
	}
	return Manifest{
		ID:          opts.ID,
		Name:        opts.Name,
		Version:     version,
		Description: desc,
		Types:       []string{"movie", "series"},
		IDPrefixes:  []string{"tt"},
		Ether: &EtherManifestBlock{
			Protocol:     1,
			Kind:         kind,
			Capabilities: caps,
			Summary:      summary,
			Runtime:      RuntimeDecl{Kind: "remote"},
			Permissions:  &Permissions{Catalog: "none"},
		},
	}
}

// NewCatalogTemplate builds a starter catalog and metadata plugin.
func NewCatalogTemplate(opts TemplateOptions) *Plugin {
	man := baseManifest(opts, KindExtension, []Capability{CapListSource})
	man.Catalogs = []StremioCatalogDecl{{
		Type:  "movie",
		ID:    "featured",
		Name:  "Featured movies",
		Extra: []StremioExtraField{{Name: "search"}},
	}}
	man.Resources = []any{"catalog", "meta"}
	man.Ether.Settings = &SettingsSchema{
		Fields: []SettingField{{
			Key:   "apiKey",
			Kind:  "secret",
			Label: "Access token",
			Hint:  "Token used to query the upstream catalog",
		}},
	}
	man.Ether.Contributes = &Contributes{
		ListSources: []ListSourceMeta{{
			Key:     "featured",
			Label:   "Featured movies",
			Hint:    "Curated movies from the upstream list",
			Catalog: "featured",
			Refresh: "1h",
		}},
	}
	return New(Definition{
		Manifest: man,
		Routes: Routes{
			Catalog: func(_ *Context, req CatalogRequest) (CatalogResponse, error) {
				title := "Featured in " + req.ID
				if req.Search != "" {
					title = "Result for " + req.Search
				}
				return CatalogResponse{
					Metas: []CatalogItem{{
						ID:          "tt0133093",
						Type:        req.Type,
						Name:        title,
						ReleaseInfo: "1999",
					}},
				}, nil
			},
			Meta: func(_ *Context, req MetaRequest) (MetaResponse, error) {
				return MetaResponse{
					Meta: MetaItem{
						CatalogItem: CatalogItem{
							ID:          req.ID,
							Type:        req.Type,
							Name:        "Title " + req.ID,
							ReleaseInfo: "1999",
							Description: "Catalog metadata entry.",
						},
					},
				}, nil
			},
		},
	})
}

// NewSubtitlesTemplate builds a starter subtitles provider plugin.
func NewSubtitlesTemplate(opts TemplateOptions) *Plugin {
	man := baseManifest(opts, KindExtension, []Capability{CapSubtitles})
	man.Resources = []any{"subtitles"}
	man.Ether.Settings = &SettingsSchema{
		Fields: []SettingField{{
			Key:     "preferredLanguage",
			Kind:    "select",
			Label:   "Preferred language",
			Default: "en",
			Options: []FieldOption{
				{Value: "en", Label: "English"},
				{Value: "es", Label: "Spanish"},
				{Value: "fr", Label: "French"},
			},
		}},
	}
	man.Ether.Contributes = &Contributes{
		Subtitles: &SubtitlesMeta{
			Languages: []string{"en", "es", "fr"},
			Formats:   []string{"vtt", "srt"},
		},
	}
	return New(Definition{
		Manifest:        man,
		DefaultSettings: map[string]any{"preferredLanguage": "en"},
		Routes: Routes{
			Subtitles: func(ctx *Context, req SubtitlesRequest) (SubtitlesResponse, error) {
				lang, _ := ctx.Settings["preferredLanguage"].(string)
				if lang == "" {
					lang = "en"
				}
				return SubtitlesResponse{
					Subtitles: []SubtitleItem{{
						ID:     req.ID + ":" + lang,
						URL:    fmt.Sprintf("https://subtitles.example.invalid/%s/%s.vtt", url.PathEscape(req.ID), lang),
						Lang:   lang,
						Label:  lang,
						Format: "vtt",
					}},
				}, nil
			},
		},
	})
}

// NewScrobblerTemplate builds a starter playback event subscriber plugin.
func NewScrobblerTemplate(opts TemplateOptions) *Plugin {
	man := baseManifest(opts, KindIntegration, []Capability{CapEvents})
	man.Ether.Contributes = &Contributes{
		Integration: &IntegrationMeta{
			Category: "scrobbler",
			Adds:     []string{"events"},
			Connect:  "none",
		},
		Events: &EventsMeta{
			Subscribe: []string{
				"playback.started",
				"playback.paused",
				"playback.stopped",
				"playback.completed",
			},
		},
	}
	return New(Definition{
		Manifest: man,
		Routes: Routes{
			Event: func(ctx *Context, events []EtherEvent) (EventResponse, error) {
				ack := make([]string, 0, len(events))
				for _, ev := range events {
					b, _ := json.Marshal(ev)
					_ = ctx.Ether.KVPut(ctx, fmt.Sprintf("last_event:%d", ev.ViewerID), string(b))
					ack = append(ack, ev.ID)
				}
				return EventResponse{Ack: ack}, nil
			},
		},
	})
}

// NewViewTemplate builds a starter ViewSpec UI and action plugin.
func NewViewTemplate(opts TemplateOptions) *Plugin {
	man := baseManifest(opts, KindExtension, []Capability{CapUI})
	man.Ether.Contributes = &Contributes{
		Pages: []PageMeta{{
			Key:   "home",
			Label: opts.Name,
			Route: "/ether/view/home",
		}},
	}
	return New(Definition{
		Manifest: man,
		Routes: Routes{
			View: func(ctx *Context, _ ViewRequest) (ViewResponse, error) {
				raw, _, _ := ctx.Ether.KVGet(ctx, "refresh_count")
				count, _ := strconv.Atoi(raw)
				return ViewResponse{
					Spec: ViewSpecDocument{
						SpecVersion: 1,
						Layout:      "list",
						Header: []map[string]any{{
							"kind":        "page-header",
							"title":       map[string]any{"$": "title"},
							"description": map[string]any{"$": "subtitle"},
						}},
						Body: []map[string]any{{
							"kind":  "panel",
							"title": "Status",
							"blocks": []map[string]any{{
								"kind":    "text",
								"content": map[string]any{"$": "statusText"},
							}},
						}},
					},
					Data: map[string]any{
						"title":      opts.Name,
						"subtitle":   fmt.Sprintf("Refreshed %d times", count),
						"statusText": "Plugin view is ready.",
					},
				}, nil
			},
			Action: func(ctx *Context, req ActionRequest) (ActionResponse, error) {
				if req.Action == "refresh" {
					raw, _, _ := ctx.Ether.KVGet(ctx, "refresh_count")
					count, _ := strconv.Atoi(raw)
					_ = ctx.Ether.KVPut(ctx, "refresh_count", strconv.Itoa(count+1))
					return ActionResponse{Toast: "State refreshed.", Refresh: true}, nil
				}
				return ActionResponse{Toast: "Unknown action."}, nil
			},
		},
	})
}

// NewRankerTemplate builds a starter release-ranking step plugin.
func NewRankerTemplate(opts TemplateOptions) *Plugin {
	man := baseManifest(opts, KindExtension, []Capability{CapRankingStep})
	man.Ether.Settings = &SettingsSchema{
		Fields: []SettingField{{
			Key:     "boost4k",
			Kind:    "bool",
			Label:   "Boost 2160p releases",
			Default: true,
		}},
	}
	man.Ether.Contributes = &Contributes{
		RankingSteps: []RankingStepMeta{{
			Key:   "resolution_boost",
			Label: "Boost 2160p releases",
			Hint:  "Adds a score bonus when a candidate is 2160p",
		}},
	}
	return New(Definition{
		Manifest:        man,
		DefaultSettings: map[string]any{"boost4k": true},
		Routes: Routes{
			RankKey: func(candidate CandidateFacts, config map[string]any) int64 {
				var score int64
				if candidate.Cached {
					score += 500
				}
				boost4k, _ := config["boost4k"].(bool)
				is4k := candidate.Resolution == 2160 || candidate.Resolution == float64(2160) || candidate.Resolution == "2160p"
				if boost4k && is4k {
					score += 1000
				}
				return score
			},
		},
	})
}

// NewConditionTemplate builds a starter rule-condition plugin.
func NewConditionTemplate(opts TemplateOptions) *Plugin {
	man := baseManifest(opts, KindExtension, []Capability{CapRuleCondition})
	man.Ether.Contributes = &Contributes{
		RuleConditions: []RuleConditionMeta{{
			Key:       "cached_release",
			Label:     "Release is cached",
			Operators: []string{"eq"},
		}},
	}
	return New(Definition{
		Manifest: man,
		Routes: Routes{
			CondMatch: func(candidate CandidateFacts, _ string, value any) bool {
				want := true
				if b, ok := value.(bool); ok {
					want = b
				}
				return candidate.Cached == want
			},
		},
	})
}
