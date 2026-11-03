package livetv

import (
	"testing"

	"github.com/braedonsaunders/ether/internal/extend"
)

func TestSourceKindClassification(t *testing.T) {
	reg := NewKindRegistry()
	cases := []struct {
		name string
		src  Source
		want string
	}{
		{"m3u url", Source{PlaylistURL: "https://x.test/p.m3u"}, "m3u"},
		{"stremio", Source{Kind: "stremio", PlaylistURL: "https://x.test/manifest.json"}, "stremio"},
		{"m3u upload", Source{Playlist: "#EXTM3U"}, "m3u"},
		{"guide only url", Source{GuideURL: "https://x.test/g.xml"}, "guideOnly"},
		{"guide only upload", Source{Guide: "<tv/>"}, "guideOnly"},
		{"xtream beats derived urls", Source{PlaylistURL: "https://x.test/get.php", GuideURL: "https://x.test/xmltv.php",
			Auth: &SourceAuth{Type: "xtream", Server: "https://x.test"}}, "xtream"},
		{"empty", Source{}, ""},
	}
	for _, c := range cases {
		k, ok := reg.Classify(c.src)
		got := ""
		if ok {
			got = k.ID()
		}
		if got != c.want {
			t.Errorf("%s: kind = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLiveTVSourceDescriptors(t *testing.T) {
	ds := BuiltinDescriptors()
	if len(ds) != 5 {
		t.Fatalf("descriptors = %d, want 5", len(ds))
	}
	byID := map[string]extend.Descriptor{}
	for _, d := range ds {
		if err := d.Validate(); err != nil {
			t.Errorf("%s: %v", d.ID, err)
		}
		byID[d.ID] = d
	}
	meta, err := extend.MetaOf[extend.LiveTVSourceMeta](byID["guideOnly"])
	if err != nil {
		t.Fatal(err)
	}
	if meta.YieldsChannels || !meta.YieldsGuide {
		t.Errorf("guideOnly meta = %+v", meta)
	}
	meta, _ = extend.MetaOf[extend.LiveTVSourceMeta](byID["xtream"])
	if !meta.YieldsChannels || meta.Editor != "xtream" {
		t.Errorf("xtream meta = %+v", meta)
	}
	// Password fields are secret-kind so the schema form never echoes them.
	found := false
	for _, p := range meta.Params {
		if p.Name == "password" {
			found = p.Kind == "secret"
		}
	}
	if !found {
		t.Error("xtream password param is not a secret field")
	}
}
