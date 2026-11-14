package server

import (
	"encoding/json"
	"fmt"
	"io"
	"mikrodash/internal/activity"
	"mikrodash/internal/db"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"
)

// Opt-in: uses synthetic data and the real authenticated HTTP handler/TCP stack.
// Cold means a result-cache miss, not dropping the host's filesystem cache.
func TestActivityHTTPPerformance(t *testing.T) {
	if os.Getenv("MIKRODASH_ACTIVITY_PERF") == "" {
		t.Skip("set MIKRODASH_ACTIVITY_PERF to run 135k/1m HTTP acceptance")
	}
	sizes := []int{135000, 1000000}
	if n, _ := strconv.Atoi(os.Getenv("MIKRODASH_ACTIVITY_PERF_SIZE")); n > 0 {
		sizes = []int{n}
	}
	type measurement struct {
		Size        int
		Case, Cache string
		P95ms       float64
		Samples     int
	}
	report := []measurement{}
	for _, size := range sizes {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			s, mux, _ := activityTestServer(t)
			s.activityWire.Close()
			reused := false
			if fixture := os.Getenv("MIKRODASH_ACTIVITY_PERF_FIXTURE"); fixture != "" {
				// Reusable fixtures are completed, idle synthetic databases. Every
				// benchmark mutates its own copy, including concurrent imports.
				copyDir := t.TempDir()
				input, err := os.Open(filepath.Join(fixture, "mikrodash.db"))
				if err != nil {
					t.Fatal(err)
				}
				output, err := os.Create(filepath.Join(copyDir, "mikrodash.db"))
				if err != nil {
					input.Close()
					t.Fatal(err)
				}
				_, err = io.Copy(output, input)
				input.Close()
				output.Close()
				if err != nil {
					t.Fatal(err)
				}
				existing, err := db.Open(copyDir)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { existing.Close() })
				s.auditDB = existing
				state, err := existing.ActivityIndexState(t.Context(), "r1")
				if err != nil || state.Total != int64(size) {
					t.Fatal("incomplete reusable synthetic fixture", state.Total, err)
				}
				reused = true
			}
			now := time.Now().UnixMilli() - 1000
			if reused {
				if n, err := strconv.ParseInt(os.Getenv("MIKRODASH_ACTIVITY_PERF_NOW"), 10, 64); err == nil {
					now = n
				} else {
					t.Fatal("reused synthetic fixture requires MIKRODASH_ACTIVITY_PERF_NOW")
				}
			}
			if err := s.auditDB.SetActivitySettings("r1", activity.Settings{Timezone: "Asia/Aden"}); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			for start := 0; !reused && start < size; start += 1000 {
				events := make([]activity.Event, 0, 1000)
				for i := start; i < min(start+1000, size); i++ {
					// Repeated domain/device evidence across all 30 days; genuine duplicates
					// share one second/shape but always have a separate source identity.
					slot := i / 2
					e := activity.Event{RouterID: "r1", SourceKey: fmt.Sprint(i), At: now - int64(slot)*int64(29*86400000)/int64(size/2), Received: now, Kind: "dns", Domain: fmt.Sprintf("d%d.example", slot%2000), User: fmt.Sprintf("مستخدم %d", slot%100), IP: fmt.Sprintf("198.51.100.%d", slot%200+1), MAC: fmt.Sprintf("02:00:00:00:%02x:%02x", slot%200/256, slot%200%256), DeviceName: fmt.Sprintf("جهاز %d", slot%100), Canonical: "example.test", QType: "A", DNSIPs: []string{"198.51.100.8"}, IPBasis: "dns-cache", Match: "manual-mac", TimeCertain: true}
					if slot%20 == 0 {
						e.Kind = "firewall"
						e.Domain = ""
						e.DNSIPs = nil
						e.Destination = "198.51.100.9:443"
						e.Protocol = "tcp"
					}
					e.Client = activity.Identity(e)
					events = append(events, e)
				}
				if err := s.auditDB.PersistActivity(events, nil, nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			t.Logf("seed %d in %s", size, time.Since(started))
			httpServer := httptest.NewServer(mux)
			defer httpServer.Close()
			client := httpServer.Client()
			client.Timeout = 60 * time.Second
			get := func(q url.Values) (activity.Result, time.Duration) {
				t.Helper()
				req, _ := http.NewRequest("GET", httpServer.URL+"/api/activity/query?"+q.Encode(), nil)
				req.Header.Set("Cookie", "mikrodash_sid=activity-test-token")
				at := time.Now()
				for attempt := 0; attempt < 60; attempt++ {
					resp, err := client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					var result activity.Result
					err = json.NewDecoder(resp.Body).Decode(&result)
					resp.Body.Close()
					if err != nil {
						t.Fatal(err)
					}
					if resp.StatusCode == http.StatusAccepted && result.Preparing {
						time.Sleep(time.Second)
						continue
					}
					if resp.StatusCode != http.StatusOK || result.Preparing {
						t.Fatal(resp.StatusCode)
					}
					return result, time.Since(at)
				}
				t.Fatal("preparation did not finish after bounded retries")
				return activity.Result{}, time.Since(at)
			}
			base := url.Values{"routerId": {"r1"}, "from": {fmt.Sprint(now - 29*86400000 - 1)}, "to": {fmt.Sprint(now + 1)}, "view": {"summary"}, "parts": {"overview"}}
			warmupQuery := url.Values{}
			for k, v := range base {
				warmupQuery[k] = append([]string{}, v...)
			}
			warmupQuery.Set("parts", "")
			first, warmup := get(warmupQuery)
			t.Logf("initial projection preparation + summary %s, events %d", warmup, first.Totals.DNS+first.Totals.Firewall)
			if first.TotalStored != int64(size) || first.Totals.DNS+first.Totals.Firewall != size {
				t.Fatal("incomplete seeded history")
			}
			for _, name := range []string{"overview", "search", "filter", "combined", "sort", "sort-label", "sort-user", "cursor", "deep-page"} {
				cold, warm := []float64{}, []float64{}
				cursor := ""
				for i := 1; i <= 20; i++ {
					q := url.Values{}
					for k, v := range base {
						q[k] = append([]string{}, v...)
					}
					q.Set("page", strconv.Itoa(i))
					if name != "overview" && name != "search" {
						q.Set("view", "events")
						q.Set("parts", "table")
						q.Set("sort", "last")
					}
					switch name {
					case "search":
						q.Set("q", "مستخدم 1")
					case "filter":
						q.Set("page", "1")
						q.Set("domain", fmt.Sprintf("d%d.example", i))
					case "combined":
						q.Set("page", "1")
						q.Set("domain", fmt.Sprintf("d%d.example", i))
						q.Set("deviceName", fmt.Sprintf("جهاز %d", i))
						q.Set("qtype", "A")
					case "sort":
						q.Set("sort", "count")
					case "sort-label":
						q.Set("sort", "label")
					case "sort-user":
						q.Set("sort", "user")
					case "cursor":
						q.Set("page", "1")
						if cursor != "" {
							q.Set("cursor", cursor)
						}
					case "deep-page":
						q.Set("page", strconv.Itoa(1000+i))
					}
					result, elapsed := get(q)
					cold = append(cold, float64(elapsed.Microseconds())/1000)
					cursor = result.NextCursor
					_, elapsed = get(q)
					warm = append(warm, float64(elapsed.Microseconds())/1000)
				}
				for cache, values := range map[string][]float64{"cold": cold, "warm": warm} {
					sort.Float64s(values)
					p95 := values[18]
					report = append(report, measurement{size, name, cache, p95, len(values)})
					t.Logf("%s %s p95 %.1fms", name, cache, p95)
					target := 500.0
					if name == "overview" || name == "search" {
						target = 2000
					}
					if p95 > target {
						t.Errorf("%s %s p95 %.1fms exceeds %.0fms", name, cache, p95, target)
					}
				}
			}
			stop, done := make(chan struct{}), make(chan struct {
				n   int
				err error
			}, 1)
			go func() {
				imported := 0
				for {
					select {
					case <-stop:
						done <- struct {
							n   int
							err error
						}{imported, nil}
						return
					default:
					}
					events := []activity.Event{}
					for j := 0; j < 100; j++ {
						i := imported + j
						events = append(events, activity.Event{RouterID: "r1", SourceKey: fmt.Sprintf("perf-live:%d", i), At: now - 1, Received: time.Now().UnixMilli(), Kind: "dns", Domain: "live.example", User: fmt.Sprint(i % 7), Client: fmt.Sprint(i % 7)})
					}
					if err := s.auditDB.PersistActivity(events, nil, nil, nil); err != nil {
						done <- struct {
							n   int
							err error
						}{imported, err}
						return
					}
					imported += len(events)
					select {
					case <-stop:
						done <- struct {
							n   int
							err error
						}{imported, nil}
						return
					case <-time.After(50 * time.Millisecond):
					}
				}
			}()
			stopped := false
			defer func() {
				if !stopped {
					close(stop)
					<-done
				}
			}()
			samples := []float64{}
			cursor := ""
			seen := map[int64]bool{}
			revisions := map[int64]bool{}
			for i := 0; i < 20; i++ {
				q := url.Values{}
				for k, v := range base {
					q[k] = append([]string{}, v...)
				}
				q.Set("view", "events")
				q.Set("parts", "table")
				q.Set("sort", "last")
				if cursor != "" {
					q.Set("cursor", cursor)
				}
				result, elapsed := get(q)
				samples = append(samples, float64(elapsed.Microseconds())/1000)
				revisions[result.DataRevision] = true
				for _, event := range result.Events {
					if seen[event.ID] {
						t.Fatal("live navigation repeated a row")
					}
					seen[event.ID] = true
				}
				cursor = result.NextCursor
			}
			close(stop)
			outcome := <-done
			stopped = true
			if outcome.err != nil || outcome.n == 0 || len(revisions) < 2 {
				t.Fatal("concurrent import was not exercised", outcome, len(revisions))
			}
			state, err := s.auditDB.ActivityIndexState(t.Context(), "r1")
			if err != nil || state.Total != int64(size+outcome.n) || state.Pending != 0 {
				t.Fatal("concurrent import projection mismatch", state, err)
			}
			sort.Float64s(samples)
			p95 := samples[18]
			report = append(report, measurement{size, "during-import", "cold", p95, len(samples)})
			t.Logf("during-import cold p95 %.1fms; imported %d events across %d observed revisions", p95, outcome.n, len(revisions))
			if p95 > 500 {
				t.Errorf("during-import p95 %.1fms exceeds 500ms", p95)
			}

		})
	}
	if path := os.Getenv("MIKRODASH_ACTIVITY_PERF_REPORT"); path != "" {
		b, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
