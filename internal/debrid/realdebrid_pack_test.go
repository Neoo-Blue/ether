package debrid

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
)

// A season pack cached on Real-Debrid as the whole pack must play any of
// its episodes: selecting the wanted episode alone is a file set Real-Debrid
// does not hold and would start a download (#262).
func TestRealDebridSelectsWholePackForAnEpisode(t *testing.T) {
	const pack = `[{"id":1,"path":"/Sleepers.S01/Sleepers.S01E01.mkv","bytes":2000000000,"selected":%[1]d},` +
		`{"id":2,"path":"/Sleepers.S01/Sleepers.S01E02.mkv","bytes":2000000000,"selected":%[1]d},` +
		`{"id":3,"path":"/Sleepers.S01/Sleepers.S01E03.mkv","bytes":2000000000,"selected":%[1]d},` +
		`{"id":4,"path":"/Sleepers.S01/Sample/sample.mkv","bytes":30000000,"selected":0},` +
		`{"id":5,"path":"/Sleepers.S01/Sleepers.S01E02.nl.srt","bytes":40000,"selected":0}]`
	var mu sync.Mutex
	selected := ""
	mux := http.NewServeMux()
	mux.HandleFunc("POST /torrents/addMagnet", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		fmt.Fprint(w, `{"id":"T1"}`)
	})
	mux.HandleFunc("GET /torrents/info/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch selected {
		case "":
			fmt.Fprintf(w, `{"id":"T1","status":"waiting_files_selection","files":`+pack+`,"links":[]}`, 0)
		case "2,1,3": // the cached variant: every episode
			fmt.Fprintf(w, `{"id":"T1","status":"downloaded","files":`+pack+`,"links":["https://rd.example/d/E1","https://rd.example/d/E2","https://rd.example/d/E3"]}`, 1)
		default:
			fmt.Fprintf(w, `{"id":"T1","status":"queued","files":`+pack+`,"links":[]}`, 0)
		}
	})
	mux.HandleFunc("POST /torrents/selectFiles/{id}", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		selected = r.PostForm.Get("files")
		mu.Unlock()
		w.WriteHeader(204)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := newRealDebrid(testKey, fastOpts(srv.URL))
	target := &core.ReleaseTarget{Season: 1, Episode: 2}
	req := acquireReq(true)
	req.Choose = func(fs []File) (File, error) { return ChooseFile(fs, target) }
	acq, err := p.Acquire(t.Context(), req)
	if err != nil {
		t.Fatalf("cached pack not served: %v (selected %q)", err, selected)
	}
	if acq.File.ID != "2" || acq.File.Link != "https://rd.example/d/E2" {
		t.Fatalf("acquired %+v, want episode 2 and its link", acq.File)
	}
}
