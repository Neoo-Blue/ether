package extend

// Meta types for the console descriptor kinds: page, navItem, command and
// integration (docs/extend/SPEC.md §5.9, WS1d). Mirrored in
// web/src/lib/console-descriptors.ts.

// Nav groups for pages and nav items. The empty group is the unheaded top
// group in the sidebar.
const (
	NavGroupTop       = ""
	NavGroupLibrary   = "library"
	NavGroupStreaming = "streaming"
	NavGroupServer    = "server"
)

// PageSection is one in-page section (tab) the sitemap and ⌘K can target.
type PageSection struct {
	Key string `json:"key"`
	// Label is declared with T for built-ins so the i18n extractor sees it.
	Label Text `json:"label"`
	// Keywords are search aliases, declared with T for built-ins. The
	// console applies its search-terms translation to the source string,
	// keeping the English terms beside the translated ones.
	Keywords Text `json:"keywords,omitempty"`
}

// PageMeta is the Meta of a KindPage descriptor. Built-in pages stay
// compiled React components; the console maps the descriptor ID to its
// component. A plugin page renders through the plugin page route.
type PageMeta struct {
	// Route is the console path, for example "/releases". Plugin pages omit
	// it; their route is /p/<plugin>/<page>.
	Route string `json:"route,omitempty"`
	// NavGroup places the page in the sidebar when InNav is set, and places
	// a plugin page in its declared group.
	NavGroup string `json:"navGroup,omitempty"`
	// InNav marks a built-in page that belongs in the sidebar. Plugin pages
	// appear in the sidebar whenever they declare a nav group.
	InNav bool `json:"inNav,omitempty"`
	// Mobile keeps the page in the mobile tab bar.
	Mobile bool `json:"mobile,omitempty"`
	// Exact matches only the route itself in the sidebar.
	Exact bool `json:"exact,omitempty"`
	// Keywords are search aliases for the page itself.
	Keywords Text `json:"keywords,omitempty"`
	// Sections are the page's in-page sections in display order.
	Sections []PageSection `json:"sections,omitempty"`
}

// NavItemMeta is the Meta of a KindNavItem descriptor. The descriptor's
// Group is the nav group, Icon the lucide icon name (built-ins) or asset
// URL (plugins), and Order the position inside the group.
type NavItemMeta struct {
	Route  string `json:"route"`
	Page   string `json:"page,omitempty"`
	Mobile bool   `json:"mobile,omitempty"`
	Exact  bool   `json:"exact,omitempty"`
}

// CommandMeta is the Meta of a KindCommand descriptor. A command either
// navigates (Route) or runs a named action (Action). Agent suggestions are
// commands Ask Ether offers on matching routes instead of ⌘K actions; the
// descriptor label is the suggestion text.
type CommandMeta struct {
	Route    string `json:"route,omitempty"`
	Action   string `json:"action,omitempty"`
	Keywords Text   `json:"keywords,omitempty"`
	// AgentSuggestion marks the command as an Ask Ether suggestion.
	AgentSuggestion bool `json:"agentSuggestion,omitempty"`
	// RoutePatterns limits an agent suggestion to console routes. A pattern
	// is an exact route or a prefix ending in "/*".
	RoutePatterns []string `json:"routePatterns,omitempty"`
}

// IntegrationSwitch is one capability switch an integration card exposes.
type IntegrationSwitch struct {
	Key   string `json:"key"`
	Label Text   `json:"label"`
	Hint  Text   `json:"hint,omitempty"`
}

// IntegrationMeta is the Meta of a KindIntegration descriptor. The
// descriptor's Group is the card category and Hint the card description.
// Tab, ConfigKey, Endpoint and Flag are owner-API wiring for built-ins;
// plugin integrations leave them empty and configure through their plugin.
type IntegrationMeta struct {
	// Category is the Integrations page group key: required,
	// watch-tracking, artwork-ratings, playback-extras or smart-features.
	Category string `json:"category"`
	// Logo names a built-in brand mark (for example "trakt") or an asset
	// URL for plugins.
	Logo string `json:"logo,omitempty"`
	// Adds are 1 to 4 short chips naming what the integration adds.
	Adds []Text `json:"adds,omitempty"`
	// Connect is the connect mode: fields, device_code, custom or none.
	Connect string `json:"connect,omitempty"`
	// Tab is the integrations sub-tab key this card configures on.
	Tab string `json:"tab,omitempty"`
	// ConfigKey is the config section key (built-ins only).
	ConfigKey string `json:"configKey,omitempty"`
	// Endpoint is the config section endpoint used to save (built-ins only).
	Endpoint string `json:"endpoint,omitempty"`
	// Test names the test behaviour the console runs (built-ins only); it is
	// a key into the console's compiled test map, like a page component.
	Test string `json:"test,omitempty"`
	// Flag is the boolean field the card switch writes.
	Flag string `json:"flag,omitempty"`
	// RequiredField must be non-empty before the integration can be
	// enabled; the switch opens the setup drawer instead.
	RequiredField string `json:"requiredField,omitempty"`
	// ReadyField is the field whose presence means "configured" for the
	// card status. It defaults to RequiredField.
	ReadyField string `json:"readyField,omitempty"`
	// PublicFallback means the integration works without ReadyField, at
	// public limits; the card says so instead of asking for a key.
	PublicFallback bool `json:"publicFallback,omitempty"`
	// Form names a compiled console form for built-ins whose settings are
	// not a plain schema form (for example "simkl" or "llm").
	Form string `json:"form,omitempty"`
	// Switches are the capability switches the card declares.
	Switches []IntegrationSwitch `json:"switches,omitempty"`
}
