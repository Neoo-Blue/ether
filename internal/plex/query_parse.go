package plex

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// /library/sections/{id}/query/parse translates a section browse URL's
// parameters into PMS's query tree. Plex Web's Advanced Filters editor asks
// for it before it renders (#278): it replaces /all with /query/parse on the
// current view and spins until the tree arrives, so it opens pre-filled.
//
// The shape follows a real PMS: no rules omits Rule; one rule is a plain
// Rule; several are a "composite" "and" group; push=1 … pop=1 opens a nested
// group, and or=1 makes the enclosing group "or". A parameter's operator is
// the characters before its "=": "year>>=2000" is year ">>=" 2000,
// "genre!=28" is genre "!=" 28, and "title==Up" is title "=" with value "=Up",
// exactly as PMS splits it.

type queryRule struct {
	Type       string      `json:"type,omitempty"`
	Field      string      `json:"field,omitempty"`
	Operator   string      `json:"operator"`
	FieldTitle *string     `json:"fieldTitle,omitempty"`
	Value      *string     `json:"value,omitempty"`
	Rule       []queryRule `json:"Rule,omitempty"`
}

type queryOrder struct {
	Field     string `json:"field"`
	Ascending bool   `json:"ascending"`
}

type parsedQuery struct {
	Type  int          `json:"type"`
	Order []queryOrder `json:"Order,omitempty"`
	Rule  *queryRule   `json:"Rule,omitempty"`
}

// queryFieldTitles names fields PMS reports that the section Meta does not list.
var queryFieldTitles = map[string]string{
	"addedAt":      "Date Added",
	"lastViewedAt": "Last Played",
	"viewCount":    "Plays",
	"label":        "Label",
	"resolution":   "Resolution",
	"producer":     "Producer",
	"rating":       "Critic Rating",
	"userRating":   "Rating",
}

func (s *Server) handleSectionQueryParse(w http.ResponseWriter, r *http.Request, sec Section) {
	typ := typeNumber(sec.Type)
	titles := map[string]string{}
	for _, f := range s.metaType(sec, sec.Type, "").Field {
		titles[f.Key] = f.Title
	}
	for k, v := range queryFieldTitles {
		if _, ok := titles[k]; !ok {
			titles[k] = v
		}
	}
	q := parsedQuery{Type: typ}
	// Groups are built in parameter order, which url.Values loses.
	stack := []*queryRule{{Type: "composite", Operator: "and"}}
	for _, pair := range strings.Split(r.URL.RawQuery, "&") {
		if pair == "" {
			continue
		}
		rawKey, rawVal, _ := strings.Cut(pair, "=")
		key, err := url.QueryUnescape(rawKey)
		if err != nil {
			continue
		}
		val, err := url.QueryUnescape(rawVal)
		if err != nil {
			continue
		}
		top := stack[len(stack)-1]
		switch {
		case key == "type":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				q.Type = n
			}
		case key == "sort":
			for _, term := range strings.Split(val, ",") {
				field, dir, _ := strings.Cut(strings.TrimSpace(term), ":")
				if field != "" {
					q.Order = append(q.Order, queryOrder{Field: field, Ascending: dir != "desc"})
				}
			}
		case key == "push":
			stack = append(stack, &queryRule{Type: "composite", Operator: "and"})
		case key == "pop":
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
				parent := stack[len(stack)-1]
				if g := collapseQueryGroup(*top); g != nil {
					parent.Rule = append(parent.Rule, *g)
				}
			}
		case key == "or":
			top.Operator = "or"
		case queryParamIgnored(key):
		default:
			field := strings.TrimRight(key, "!<>")
			if field == "" {
				continue
			}
			op := key[len(field):] + "="
			title := titles[stripFieldTypePrefix(field)]
			v := val
			top.Rule = append(top.Rule, queryRule{Field: field, Operator: op, FieldTitle: &title, Value: &v})
		}
	}
	// An unclosed push still counts, as if popped at the end.
	for len(stack) > 1 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if g := collapseQueryGroup(*top); g != nil {
			stack[len(stack)-1].Rule = append(stack[len(stack)-1].Rule, *g)
		}
	}
	q.Rule = collapseQueryGroup(*stack[0])
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // operators such as ">>=" and "<=" stay literal, as PMS writes them
	_ = enc.Encode(map[string]parsedQuery{"Query": q})
}

// collapseQueryGroup drops an empty group and unwraps a group of one rule.
func collapseQueryGroup(g queryRule) *queryRule {
	switch len(g.Rule) {
	case 0:
		return nil
	case 1:
		return &g.Rule[0]
	}
	return &g
}

// queryParamIgnored reports parameters that shape a response rather than
// filter it, which PMS leaves out of the query tree.
func queryParamIgnored(key string) bool {
	if strings.HasPrefix(key, "X-Plex-") || strings.HasPrefix(key, "include") || strings.HasPrefix(key, "exclude") {
		return true
	}
	switch key {
	case "limit", "group", "checkFiles", "asyncCheckFiles", "skipRefresh", "asyncRefreshLocalMediaAgent", "asyncRefreshAnalysis", "X-Plex-Container-Start", "X-Plex-Container-Size":
		return true
	}
	return false
}
