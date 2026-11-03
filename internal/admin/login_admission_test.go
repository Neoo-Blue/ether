package admin

import (
	"io"
	"net/http/httptest"
	"testing"
)

type unreadLoginBody struct{ read bool }

func (b *unreadLoginBody) Read([]byte) (int, error) { b.read = true; return 0, io.EOF }
func (*unreadLoginBody) Close() error               { return nil }
func TestLoginAdmissionBeforeBody(t *testing.T) {
	h := &Handler{loginBodySlots: make(chan struct{}, 32)}
	for i := 0; i < cap(h.loginBodySlots); i++ {
		h.loginBodySlots <- struct{}{}
	}
	body := &unreadLoginBody{}
	r := httptest.NewRequest("POST", "/api/auth/local", nil)
	r.Body = body
	w := httptest.NewRecorder()
	h.localLogin(w, r, "owner")
	if w.Code != 503 || body.read {
		t.Fatalf("status %d, body read %v", w.Code, body.read)
	}
}
