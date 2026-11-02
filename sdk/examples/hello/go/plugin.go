package hello

import (
	"fmt"
	"net/url"

	etherplugin "github.com/braedonsaunders/ether/sdk/go"
)

// NewHelloPlugin constructs the reference Go hello plugin with identical routes
// and payloads to the TypeScript and Python hello plugins.
func NewHelloPlugin() *etherplugin.Plugin {
	return etherplugin.New(etherplugin.Definition{
		Manifest: etherplugin.Manifest{
			ID:          "org.ether.examples.hello",
			Version:     "0.1.0",
			Name:        "Hello plugin",
			Description: "Minimal greeting plugin for Ether.",
			Types:       []string{"movie"},
			IDPrefixes:  []string{"tt"},
			Catalogs: []etherplugin.StremioCatalogDecl{{
				Type:  "movie",
				ID:    "hello",
				Name:  "Hello movies",
				Extra: []etherplugin.StremioExtraField{{Name: "search"}},
			}},
			Resources: []any{"catalog", "meta", "subtitles"},
			Ether: &etherplugin.EtherManifestBlock{
				Protocol: 1,
				Kind:     etherplugin.KindExtension,
				Capabilities: []etherplugin.Capability{
					etherplugin.CapListSource,
					etherplugin.CapSubtitles,
					etherplugin.CapUI,
					etherplugin.CapRankingStep,
					etherplugin.CapRuleCondition,
				},
				Summary:     "Minimal greeting plugin for Ether.",
				Runtime:     etherplugin.RuntimeDecl{Kind: "remote", Entry: "plugin.go"},
				Permissions: &etherplugin.Permissions{Catalog: "none"},
				Settings: &etherplugin.SettingsSchema{
					Fields: []etherplugin.SettingField{{
						Key:     "greeting",
						Kind:    "text",
						Label:   "Greeting word",
						Hint:    "Word used to greet viewers in the catalog and view",
						Default: "Hello",
					}},
				},
				Contributes: &etherplugin.Contributes{
					ListSources: []etherplugin.ListSourceMeta{{
						Key:     "hello",
						Label:   "Hello movies",
						Hint:    "Sample movie list from the hello plugin",
						Catalog: "hello",
						Refresh: "1h",
					}},
					Subtitles: &etherplugin.SubtitlesMeta{
						Languages: []string{"en"},
						Formats:   []string{"vtt"},
					},
					Pages: []etherplugin.PageMeta{{
						Key:   "home",
						Label: "Hello overview",
						Route: "/ether/view/home",
					}},
					RankingSteps: []etherplugin.RankingStepMeta{{
						Key:   "cached_boost",
						Label: "Boost cached releases",
						Hint:  "Adds a score bonus when a release is cached",
					}},
					RuleConditions: []etherplugin.RuleConditionMeta{{
						Key:       "is_cached",
						Label:     "Release is cached",
						Operators: []string{"eq"},
					}},
				},
			},
		},
		DefaultSettings: map[string]any{"greeting": "Hello"},
		Routes: etherplugin.Routes{
			Test: func(_ *etherplugin.Context) (etherplugin.TestResponse, error) {
				return etherplugin.TestResponse{
					OK:      true,
					Message: "Hello plugin is healthy.",
				}, nil
			},
			Catalog: func(ctx *etherplugin.Context, req etherplugin.CatalogRequest) (etherplugin.CatalogResponse, error) {
				greeting, _ := ctx.Settings["greeting"].(string)
				if greeting == "" {
					greeting = "Hello"
				}
				target := req.Search
				if target == "" {
					target = "from Ether"
				}
				return etherplugin.CatalogResponse{
					Metas: []etherplugin.CatalogItem{{
						ID:          "tt0133093",
						Type:        req.Type,
						Name:        greeting + " " + target,
						ReleaseInfo: "1999",
						Description: "A computer hacker learns about the true nature of reality.",
					}},
				}, nil
			},
			Meta: func(_ *etherplugin.Context, req etherplugin.MetaRequest) (etherplugin.MetaResponse, error) {
				return etherplugin.MetaResponse{
					Meta: etherplugin.MetaItem{
						CatalogItem: etherplugin.CatalogItem{
							ID:          req.ID,
							Type:        req.Type,
							Name:        "The Matrix",
							ReleaseInfo: "1999",
							Description: "A computer hacker learns about the true nature of reality.",
						},
					},
				}, nil
			},
			Subtitles: func(_ *etherplugin.Context, req etherplugin.SubtitlesRequest) (etherplugin.SubtitlesResponse, error) {
				return etherplugin.SubtitlesResponse{
					Subtitles: []etherplugin.SubtitleItem{{
						ID:     req.ID + ":en",
						URL:    fmt.Sprintf("https://subtitles.example.invalid/%s/en.vtt", url.PathEscape(req.ID)),
						Lang:   "en",
						Label:  "English",
						Format: "vtt",
					}},
				}, nil
			},
			View: func(ctx *etherplugin.Context, _ etherplugin.ViewRequest) (etherplugin.ViewResponse, error) {
				greeting, _ := ctx.Settings["greeting"].(string)
				if greeting == "" {
					greeting = "Hello"
				}
				return etherplugin.ViewResponse{
					Spec: etherplugin.ViewSpecDocument{
						SpecVersion: 1,
						Layout:      "list",
						Header: []map[string]any{{
							"kind":        "page-header",
							"title":       map[string]any{"$": "title"},
							"description": map[string]any{"$": "subtitle"},
						}},
						Body: []map[string]any{{
							"kind":  "panel",
							"title": "Greeting",
							"blocks": []map[string]any{{
								"kind":    "text",
								"content": map[string]any{"$": "greetingText"},
							}},
						}},
					},
					Data: map[string]any{
						"title":        "Hello plugin",
						"subtitle":     "Greeting view",
						"greetingText": greeting + " from Ether",
					},
				}, nil
			},
			Action: func(ctx *etherplugin.Context, _ etherplugin.ActionRequest) (etherplugin.ActionResponse, error) {
				greeting, _ := ctx.Settings["greeting"].(string)
				if greeting == "" {
					greeting = "Hello"
				}
				return etherplugin.ActionResponse{
					Toast:   greeting + " from Ether",
					Refresh: true,
				}, nil
			},
			RankKey: func(candidate etherplugin.CandidateFacts, _ map[string]any) int64 {
				if candidate.Cached {
					return 1000
				}
				return 100
			},
			CondMatch: func(candidate etherplugin.CandidateFacts, _ string, value any) bool {
				want := true
				if b, ok := value.(bool); ok {
					want = b
				}
				return candidate.Cached == want
			},
		},
	})
}
