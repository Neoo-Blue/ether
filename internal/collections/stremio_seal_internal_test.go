package collections

import (
	"fmt"
	"github.com/braedonsaunders/ether/internal/core"
	"strings"
	"testing"
)

type testBoundSealer struct{ values map[string]string }

func (s *testBoundSealer) SealField(plain string, context ...string) (string, error) {
	key := strings.Join(context, "|")
	s.values[key] = plain
	return "sealed:" + key, nil
}
func (s *testBoundSealer) OpenField(stored string, context ...string) (string, bool, error) {
	key := strings.Join(context, "|")
	if stored != "sealed:"+key {
		return "", false, fmt.Errorf("context mismatch")
	}
	return s.values[key], false, nil
}
func TestStremioCollectionSecretBoundToCollection(t *testing.T) {
	sealer := &testBoundSealer{values: map[string]string{}}
	svc := &Service{sealer: sealer}
	spec := core.CollectionSpec{Sources: []core.CollectionSource{{Kind: "stremioCatalog", URL: "https://addon.example/key/manifest.json"}}}
	sealed, err := svc.sealSpec(spec, 1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed.Sources[0].URL, "key/manifest") {
		t.Fatal("URL not sealed")
	}
	copied := sealed
	copied.Sources = append([]core.CollectionSource(nil), sealed.Sources...)
	if err := openSpecSources(sealer, &copied, 2); err == nil {
		t.Fatal("secret opened in another collection")
	}
	if err := openSpecSources(sealer, &sealed, 1); err != nil || sealed.Sources[0].URL != spec.Sources[0].URL {
		t.Fatalf("round trip: %+v err=%v", sealed, err)
	}
}
