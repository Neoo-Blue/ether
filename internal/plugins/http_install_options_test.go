package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPackageInstallOptionsAndAutoUpdate(t *testing.T) {
	env := newTestEnv(t)
	files := helloFiles()
	files["manifest.json"] = helloManifest("1.0.0")
	files["digest"] = []byte(PkgDigest(files))
	var pkg bytes.Buffer
	zw := zip.NewWriter(&pkg)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("package", "test.etherpkg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pkg.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := form.WriteField("options", `{"grants":{"viewers":[42],"folders":["/media"]},"shadow":true}`); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/install", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	res := httptest.NewRecorder()
	env.m.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("install: %d %s", res.Code, res.Body.String())
	}
	rec, err := env.m.store.record(req.Context(), "community.example.hello")
	if err != nil {
		t.Fatal(err)
	}
	grants, err := env.m.store.grants(req.Context(), rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Shadow || len(grants.Viewers) != 1 || grants.Viewers[0] != 42 || len(grants.Folders) != 1 || grants.Folders[0] != "/media" {
		t.Fatalf("options not saved: %+v %+v", rec, grants)
	}
	req = httptest.NewRequest(http.MethodPut, "/"+rec.ID+"/auto-update", bytes.NewBufferString(`{"autoUpdate":true}`))
	res = httptest.NewRecorder()
	env.m.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("put: %d %s", res.Code, res.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/"+rec.ID+"/auto-update", nil)
	res = httptest.NewRecorder()
	env.m.Handler().ServeHTTP(res, req)
	var got struct {
		AutoUpdate bool `json:"autoUpdate"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil || !got.AutoUpdate {
		t.Fatalf("get: %d %s %v", res.Code, res.Body.String(), err)
	}
}

func TestAutomaticUpdateOnlyForVerifiedPlugin(t *testing.T) {
	for _, verified := range []bool{false, true} {
		t.Run(fmt.Sprintf("verified=%t", verified), func(t *testing.T) {
			env := newTestEnv(t)
			ctx := context.Background()
			rec := env.installHello(t, "1.0.0")
			files := helloFiles()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.TrimPrefix(r.URL.Path, "/") == "manifest.json" {
					_, _ = w.Write(helloManifest("2.0.0"))
					return
				}
				if b, ok := files[strings.TrimPrefix(r.URL.Path, "/")]; ok {
					_, _ = w.Write(b)
					return
				}
				http.NotFound(w, r)
			}))
			defer srv.Close()
			env.m.client = srv.Client()
			rec.SourceURL, rec.SourceKind, rec.Verified, rec.AutoUpdate, rec.Shadow = srv.URL, "url", verified, true, true
			if err := env.m.store.upsert(ctx, rec); err != nil {
				t.Fatal(err)
			}
			if _, _, err := env.m.CheckUpdate(ctx, rec.ID); err != nil {
				t.Fatal(err)
			}
			got, err := env.m.Get(ctx, rec.ID)
			if err != nil {
				t.Fatal(err)
			}
			if verified && (got.Version != "2.0.0" || !got.AutoUpdate || !got.Shadow) {
				t.Fatalf("automatic update: %+v", got)
			}
			if !verified && (got.Version != "1.0.0" || got.UpdateVersion != "2.0.0") {
				t.Fatalf("unverified update applied: %+v", got)
			}
		})
	}
}
