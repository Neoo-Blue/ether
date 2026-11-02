package frontend

import (
	"encoding/json"
	"testing"
)

func TestSettingsSchemasValidate(t *testing.T) {
	jf := MediaBrowserSchema(true)
	for name, tc := range map[string]struct {
		schema SettingsSchema
		in     Settings
		ok     bool
	}{
		"jellyfin full":        {jf, Settings{"port": 8096.0, "discovery": true, "quickConnect": true, "publicViewerPicker": false, "homeRows": true, "homeRowCount": 4.0, "homeRowKinds": []any{"trending"}}, true},
		"jellyfin port range":  {jf, Settings{"port": 70000.0}, false},
		"jellyfin row count":   {jf, Settings{"homeRowCount": 13.0}, false},
		"jellyfin row kind":    {jf, Settings{"homeRowKinds": []any{"nope"}}, false},
		"jellyfin unknown":     {jf, Settings{"bogus": true}, false},
		"jellyfin bool type":   {jf, Settings{"discovery": "yes"}, false},
		"emby no quickconnect": {MediaBrowserSchema(false), Settings{"quickConnect": true}, false},
		"silo":                 {NamedServerSchema(), Settings{"port": 8090.0, "discovery": false}, true},
		"silo picker rejected": {NamedServerSchema(), Settings{"publicViewerPicker": true}, false},
		"dlna viewer":          {DLNASchema(), Settings{"viewerId": 3.0}, true},
		"dlna viewer zero":     {DLNASchema(), Settings{"viewerId": 0.0}, false},
		"dlna no port":         {DLNASchema(), Settings{"port": 8200.0}, false},
		"account empty":        {AccountSchema("addon"), Settings{}, true},
		"account setting":      {AccountSchema("addon"), Settings{"port": 1.0}, false},
		"plex port":            {PortSchema(), Settings{"port": 32400.0}, true},
		"plex other":           {PortSchema(), Settings{"discovery": true}, false},
	} {
		t.Run(name, func(t *testing.T) {
			err := tc.schema.validate(tc.in)
			if tc.ok && err != nil {
				t.Fatalf("validate(%v) = %v, want ok", tc.in, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("validate(%v) = nil, want error", tc.in)
			}
		})
	}
}

func TestSettingsSchemaJSON(t *testing.T) {
	out, err := json.Marshal(Descriptor{ID: "x", SettingsSchema: MediaBrowserSchema(true)})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		SettingsSchema SettingsSchema `json:"settingsSchema"`
	}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.SettingsSchema.SignInHelp || len(decoded.SettingsSchema.Sections) != 2 {
		t.Fatalf("schema did not round trip: %s", out)
	}
}
