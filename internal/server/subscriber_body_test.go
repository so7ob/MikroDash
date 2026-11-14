package server

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubscriberBodyRequiresOneCompleteBoundedObject(t *testing.T) {
	const valid = `{"name":"Synthetic Unicode: مشترك"}`
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"object", valid, true},
		{"whitespace", " \n" + valid + "\t\r\n", true},
		{"exact limit", valid + strings.Repeat(" ", (64<<10)-len(valid)), true},
		{"second object", valid + `{}`, false},
		{"second null", valid + `null`, false},
		{"trailing garbage", valid + `!`, false},
		{"oversized whitespace", valid + strings.Repeat(" ", 64<<10), false},
		{"oversized field", `{"name":"` + strings.Repeat("x", 64<<10) + `"}`, false},
		{"null", `null`, false},
		{"leading vertical tab", "\v" + valid, false},
		{"trailing vertical tab", valid + "\v", false},
		{"leading nonbreaking space", "\u00a0" + valid, false},
		{"trailing nonbreaking space", valid + "\u00a0", false},
		{"leading form feed", "\f" + valid, false},
		{"array", `[]`, false},
		{"unknown field", `{"name":"synthetic","unknown":true}`, false},
		{"truncated", `{"name":`, false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var in struct {
				Name string `json:"name"`
			}
			r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			w := httptest.NewRecorder()
			if got := subscriberBody(w, r, &in); got != tc.valid {
				t.Fatalf("accepted=%v, want %v; response %s", got, tc.valid, w.Body.String())
			}
			if !tc.valid && w.Code != 400 {
				t.Fatalf("status=%d", w.Code)
			}
		})
	}
}

type subscriberFailingBody struct{ io.Reader }

func (r subscriberFailingBody) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}
func (r subscriberFailingBody) Close() error { return nil }

func TestSubscriberBodyRejectsInterruptedReadAfterObject(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.Body = subscriberFailingBody{strings.NewReader(`{"name":"synthetic"}`)}
	w := httptest.NewRecorder()
	var in struct {
		Name string `json:"name"`
	}
	if subscriberBody(w, r, &in) || w.Code != 400 {
		t.Fatal("interrupted body accepted", w.Code)
	}
}
