package collections

import (
	"encoding/json"
	"flag"
	"os"
	"reflect"
	"testing"
)

var updateMock = flag.Bool("update", false, "rewrite the web mock descriptor JSON from Go")

// The console mock backend serves descriptor JSON that must mirror Go
// exactly; this test fails on drift. Regenerate with:
//
//	go test ./internal/collections/ -run TestMockDescriptorsMatchGo -update
func TestMockDescriptorsMatchGo(t *testing.T) {
	want, err := json.MarshalIndent(BuiltinDescriptors(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	path := "../../web/src/mock/extend/descriptors/collections.json"
	if *updateMock {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("mock descriptors missing (run with -update): %v", err)
	}
	var gotV, wantV any
	if err := json.Unmarshal(got, &gotV); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &wantV); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotV, wantV) {
		t.Fatal("web/src/mock/extend/descriptors/collections.json has drifted from Go descriptors; run: go test ./internal/collections/ -run TestMockDescriptorsMatchGo -update")
	}
}
