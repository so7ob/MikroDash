package subscriber

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"mikrodash/internal/routeros"
)

type previewReader struct {
	rows        map[string][]routeros.Reply
	unavailable map[string]bool
	calls       []routeros.Cmd
}

func (r *previewReader) Do(cmd routeros.Cmd) ([]routeros.Reply, error) {
	r.calls = append(r.calls, cmd)
	if r.unavailable[cmd.Path] {
		return nil, errors.New("synthetic secret error must not be reported")
	}
	return r.rows[cmd.Path], nil
}

func TestImportPreviewProjectsSourceWithoutSecretsOrMutation(t *testing.T) {
	const menu = "/user-manager/limitation/print"
	source := routeros.Reply{".id": "*synthetic", "name": "Synthetic daily 1min", "uptime-limit": "0s", "download-limit": "0", "reset-counters-interval": "never", "password": "synthetic-unrequested-secret", "comment": "synthetic-unrequested-comment"}
	reader := &previewReader{rows: map[string][]routeros.Reply{menu: {source}}}
	p := PreviewUserManager(reader)
	if len(reader.calls) != 5 || p.FingerprintVersion != 2 || len(p.Fingerprint) != 64 {
		t.Fatal("missing preview reads or version", p)
	}
	paths := map[string]bool{}
	for _, cmd := range reader.calls {
		if !strings.HasPrefix(cmd.Path, "/user-manager/") || !strings.HasSuffix(cmd.Path, "/print") || paths[cmd.Path] || len(cmd.Args) != 1 || !strings.HasPrefix(cmd.Args[0], "=.proplist=") || strings.Contains(cmd.Args[0], "password") || strings.Contains(cmd.Args[0], "secret") || cmd.Timeout != 5*time.Second {
			t.Fatal("unexpected command", cmd)
		}
		paths[cmd.Path] = true
	}
	row := p.Rows[menu][0]
	if row["uptime-limit"] != "0s" || row["download-limit"] != "0" {
		t.Fatal("source zero semantics changed", row)
	}
	if _, exists := row["upload-limit"]; exists {
		t.Fatal("missing quota was fabricated")
	}
	raw, err := json.Marshal(p)
	if err != nil || strings.Contains(string(raw), "synthetic-unrequested") || strings.Contains(string(raw), `"password"`) {
		t.Fatal("unexpected fields leaked", err)
	}
	if len(p.Warnings) != 5 {
		t.Fatal("existing mismatch warnings lost", p.Warnings)
	}
	source["name"] = "Source changed later"
	if row["name"] != "Synthetic daily 1min" {
		t.Fatal("preview aliased source data")
	}
	row["download-limit"] = "42"
	if source["download-limit"] != "0" || source["password"] != "synthetic-unrequested-secret" {
		t.Fatal("preview mutated source")
	}
}

func TestImportPreviewDistinguishesUnknownEmptyAndMenuBoundaries(t *testing.T) {
	empty := PreviewUserManager(&previewReader{})
	if empty.Problems == nil || empty.Unavailable == nil || len(empty.Rows) != 5 {
		t.Fatal("empty preview lost explicit arrays", empty)
	}
	unknown := PreviewUserManager(&previewReader{unavailable: map[string]bool{"/user-manager/user/print": true}})
	if unknown.Fingerprint == empty.Fingerprint || !reflect.DeepEqual(unknown.Unavailable, []string{"/user-manager/user/print"}) {
		t.Fatal("unavailable read looked empty", unknown)
	}
	if _, exists := unknown.Rows["/user-manager/user/print"]; exists {
		t.Fatal("unavailable source fabricated rows")
	}
	raw, _ := json.Marshal(unknown)
	if strings.Contains(string(raw), "synthetic secret") {
		t.Fatal("source error leaked")
	}
	a := PreviewUserManager(&previewReader{rows: map[string][]routeros.Reply{"/user-manager/profile/print": {{".id": "*a", "name": "Synthetic"}}}})
	b := PreviewUserManager(&previewReader{rows: map[string][]routeros.Reply{"/user-manager/user/print": {{".id": "*a", "name": "Synthetic"}}}})
	if a.Fingerprint == b.Fingerprint {
		t.Fatal("moving identical rows between menus kept fingerprint")
	}
}

func TestImportPreviewFingerprintIsStructuredAndPreservesRawMeaning(t *testing.T) {
	fingerprint := func(rows []routeros.Reply) string {
		return PreviewUserManager(&previewReader{rows: map[string][]routeros.Reply{"/user-manager/profile/print": rows}}).Fingerprint
	}
	// These two rows had identical fmt.Sprint text, despite different source fields.
	a := []routeros.Reply{{".id": "*a", "name": "x validity:y"}}
	b := []routeros.Reply{{".id": "*a", "name": "x", "validity": "y"}}
	if fingerprint(a) == fingerprint(b) {
		t.Fatal("text delimiter collision")
	}
	if fingerprint(a) != fingerprint([]routeros.Reply{{"name": "x validity:y", ".id": "*a"}}) {
		t.Fatal("map insertion order changed fingerprint")
	}
	if fingerprint(a) == fingerprint([]routeros.Reply{{".id": "*a", "name": "x validity:y", "validity": "0s"}}) {
		t.Fatal("missing and explicit zero collided")
	}
	ordered := []routeros.Reply{{".id": "*a", "name": "A"}, {".id": "*b", "name": "B"}}
	if fingerprint(ordered) == fingerprint([]routeros.Reply{ordered[1], ordered[0]}) {
		t.Fatal("source order silently discarded")
	}
	if fingerprint(a) != fingerprint([]routeros.Reply{{".id": "*a", "name": "x validity:y", "password": "ignored synthetic"}}) {
		t.Fatal("unrequested field affected fingerprint")
	}
}

func TestImportPreviewReportsAllAmbiguousIdentitiesAndMissingReferences(t *testing.T) {
	const users = "/user-manager/user/print"
	const links = "/user-manager/user-profile/print"
	p := PreviewUserManager(&previewReader{rows: map[string][]routeros.Reply{
		users: {{".id": "*a", "name": "اسم"}, {".id": "*a", "name": "اسم"}, {".id": "*b", "name": "Name"}, {".id": "*c", "name": "name"}, {"name": ""}},
		links: {{".id": "*link", "user": "", "profile": ""}},
	}})
	want := []ImportProblem{
		{Menu: users, Row: 0, Field: ".id", Code: "duplicate-identity"},
		{Menu: users, Row: 1, Field: ".id", Code: "duplicate-identity"},
		{Menu: users, Row: 4, Field: ".id", Code: "missing-identity"},
		{Menu: users, Row: 0, Field: "name", Code: "duplicate-identity"},
		{Menu: users, Row: 1, Field: "name", Code: "duplicate-identity"},
		{Menu: users, Row: 4, Field: "name", Code: "missing-identity"},
		{Menu: links, Row: 0, Field: "user", Code: "missing-reference"},
		{Menu: links, Row: 0, Field: "profile", Code: "missing-reference"},
	}
	if !reflect.DeepEqual(p.Problems, want) {
		t.Fatalf("problems=%+v want=%+v", p.Problems, want)
	}
	if p.Rows[users][0]["name"] != "اسم" || p.Rows[users][3]["name"] != "name" {
		t.Fatal("identities normalized")
	}
}

func FuzzImportPreviewIgnoresUnrequestedFields(f *testing.F) {
	f.Add("synthetic secret")
	f.Add("\xff\x00synthetic")
	f.Fuzz(func(t *testing.T, extra string) {
		if len(extra) > 4096 {
			t.Skip()
		}
		const menu = "/user-manager/user/print"
		baseline := PreviewUserManager(&previewReader{rows: map[string][]routeros.Reply{menu: {{".id": "*a", "name": "Synthetic user"}}}})
		candidate := PreviewUserManager(&previewReader{rows: map[string][]routeros.Reply{menu: {{".id": "*a", "name": "Synthetic user", "password": extra, "unexpected": extra}}}})
		if !reflect.DeepEqual(candidate, baseline) {
			t.Fatal("unrequested data changed source review")
		}
	})
}
