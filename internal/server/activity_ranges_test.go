package server

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"mikrodash/internal/activity"
	"mikrodash/internal/db"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestActivityHTTPAllPeriodsQueriesAndExports(t *testing.T) {
	s, mux, _ := activityTestServer(t)
	s.activityWire.Close()
	now := time.Now().UnixMilli() - 1000
	var events []activity.Event
	for i, day := range []int64{0, 2, 6, 13, 28} {
		events = append(events, activity.Event{RouterID: "r1", SourceKey: fmt.Sprint(i), At: now - day*86400000, Received: now, Kind: "dns", Domain: "range.example", User: "اسم محفوظ", Client: "synthetic", DNSIPs: []string{"2001:db8::8"}, TimeCertain: true})
	}
	if err := s.auditDB.PersistActivity(events, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	cookie := "mikrodash_sid=activity-test-token"
	for _, period := range []struct {
		name   string
		values url.Values
		count  int
	}{
		{"day", url.Values{"days": {"1"}}, 1},
		{"week", url.Values{"days": {"7"}}, 3},
		{"month", url.Values{"days": {"30"}}, 5},
		{"custom", url.Values{"from": {strconv.FormatInt(now-14*86400000, 10)}, "to": {strconv.FormatInt(now-86400000, 10)}}, 3},
	} {
		t.Run(period.name, func(t *testing.T) {
			for _, route := range []string{"query", "summary", "series", "domains", "clients", "events"} {
				for _, part := range []string{"", "overview", "table", "status", "rules"} {
					q := url.Values{"routerId": {"r1"}, "parts": {part}, "group": {"domain"}, "sort": {"last"}}
					for k, v := range period.values {
						q[k] = v
					}
					for repeat := 0; repeat < 2; repeat++ {
						w := activityRequest(mux, "GET", "/api/activity/"+route+"?"+q.Encode(), "", cookie)
						if w.Code != 200 {
							t.Fatalf("%s/%s status=%d %s", route, part, w.Code, w.Body.String())
						}
						var out activity.Result
						if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
							t.Fatal(err)
						}
						if out.Preparing || out.RouterID != "r1" || out.TotalStored != 5 {
							t.Fatalf("incomplete response %s/%s", route, part)
						}
						if part != "status" && part != "rules" && out.Totals.DNS != period.count {
							t.Fatalf("%s/%s count=%d want=%d", route, part, out.Totals.DNS, period.count)
						}
					}
				}
			}
			for _, search := range []string{"", "اسم", "__missing_109__"} {
				q := url.Values{"routerId": {"r1"}, "q": {search}}
				for k, v := range period.values {
					q[k] = v
				}
				w := activityRequest(mux, "GET", "/api/activity/export?"+q.Encode(), "", cookie)
				if w.Code != 200 {
					t.Fatal("CSV status", w.Code, w.Body.String())
				}
				rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
				want := period.count
				if search == "__missing_109__" {
					want = 0
				}
				if err != nil || len(rows) != want+1 {
					t.Fatal("CSV is incomplete", len(rows), want, err)
				}
				if want > 0 && !strings.Contains(w.Body.String(), "اسم محفوظ") {
					t.Fatal("Unicode lost in CSV")
				}
			}
		})
	}
	// A warm result must not survive permission revocation on any route/period.
	if _, err := s.auditDB.DeleteGrantsForScope("router", "r1"); err != nil {
		t.Fatal(err)
	}
	for _, days := range []string{"1", "7", "30"} {
		for _, route := range []string{"query", "summary", "series", "domains", "clients", "events", "export"} {
			w := activityRequest(mux, "GET", "/api/activity/"+route+"?routerId=r1&days="+days, "", cookie)
			if w.Code != 403 {
				t.Fatal("revoked route disclosed data", route, days, w.Code)
			}
		}
	}
}

func TestActivityLargeHTTPPreparationCompletesAndRechecksScope(t *testing.T) {
	s, mux, _ := activityTestServer(t)
	s.activityWire.Close()
	cookie := "mikrodash_sid=activity-test-token"
	path := "/api/activity/query?routerId=r1&days=30&view=summary&parts=overview"
	if w := activityRequest(mux, "GET", path, "", cookie); w.Code != 200 {
		t.Fatal("empty source failed", w.Code, w.Body.String())
	}
	now := time.Now().UnixMilli() - 1000
	var events []activity.Event
	for i := 0; i < 4200; i++ {
		events = append(events, activity.Event{RouterID: "r1", SourceKey: fmt.Sprint(i), At: now - int64(i)*1000, Received: now, Kind: "dns", Domain: fmt.Sprintf("d%d.example", i), Client: "synthetic", User: "اسم محفوظ", DNSIPs: []string{"2001:db8::8"}})
	}
	if err := s.auditDB.PersistActivity(events, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	pending := 0
	for attempt := 0; attempt < 30; attempt++ {
		w := activityRequest(mux, "GET", path, "", cookie)
		var out activity.Result
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if w.Code == 200 {
			if out.Preparing || out.Totals.DNS != 4200 || out.TotalStored != 4200 || len(out.Rows) == 0 || pending < 7 {
				t.Fatal("incomplete heavy response", out.Preparing, out.Totals.DNS, pending)
			}
			return
		}
		if w.Code != 202 || !out.Preparing || w.Header().Get("Retry-After") != "1" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("not resumable", w.Code, w.Header())
		}
		if out.Totals.DNS != 0 || len(out.Rows) != 0 || len(out.Series) != 0 || len(out.Categories) != 0 {
			t.Fatal("partial heavy result disclosed")
		}
		pending++
		if pending == 2 {
			if _, err := s.auditDB.DeleteGrantsForScope("router", "r1"); err != nil {
				t.Fatal(err)
			}
			if denied := activityRequest(mux, "GET", path, "", cookie); denied.Code != 403 {
				t.Fatal("pending query ignored revoked scope", denied.Code)
			}
			if err := s.auditDB.UpsertGrant(db.GrantSpec{PrincipalType: "user", PrincipalID: "activity-user", RoleID: "administrator", ScopeType: "router", ScopeID: "r1"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Fatal("heavy response never completed")
}
