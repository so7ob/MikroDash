package subscriber

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func TestQuantityLeadingZerosAreDecimal(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want Quantity
	}{
		{`"0"`, 0}, {`"000"`, 0}, {`"010"`, 10}, {`"08"`, 8}, {`"0009"`, 9}, {`"00100"`, 100},
		{`"09007199254740993"`, 9007199254740993}, {`"09223372036854775807"`, math.MaxInt64},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			var q Quantity
			if err := json.Unmarshal([]byte(tc.raw), &q); err != nil || q != tc.want {
				t.Fatalf("quantity=%d want=%d err=%v", q, tc.want, err)
			}
			encoded, err := json.Marshal(q)
			if err != nil || string(encoded) != fmt.Sprintf(`"%d"`, tc.want) {
				t.Fatalf("canonical output=%s err=%v", encoded, err)
			}
		})
	}
}

func TestQuantityRejectsNonDecimalAndOverflowWithoutChangingValue(t *testing.T) {
	for _, raw := range []string{`""`, `"-1"`, `"+1"`, `"0x10"`, `"0o10"`, `"0b10"`, `"1_000"`, `"1.5"`, `"1e3"`, `" 1"`, `"1 "`, `"١"`, `"\u0031"`, `1`, `null`, `"9223372036854775808"`, `"09223372036854775808"`, `"99999999999999999999999999"`} {
		q := Quantity(17)
		if err := json.Unmarshal([]byte(raw), &q); err == nil || q != 17 {
			t.Fatalf("invalid %s accepted or changed value: %d err=%v", raw, q, err)
		}
	}
}

func TestDecimalQuantitiesReachConcurrentQuotaAndSpeedDecision(t *testing.T) {
	const raw = `{"id":"synthetic","version":1,"name":"Synthetic plan","validSeconds":"03600","start":"first-login","maxSessions":1,"networks":["synthetic"],"down":"01000000","up":"00800000","speeds":[{"id":"standard","name":"Synthetic speed","down":"01000000","up":"00800000"}],"policies":[{"id":"concurrent","limits":[{"id":"time","kind":"seconds","state":"limited","amount":"010","period":{"kind":"none","zone":"UTC","anchor":"2026-01-01T00:00:00Z"},"action":"disconnect","down":"0","up":"0"},{"id":"data","kind":"total","state":"limited","amount":"0100","period":{"kind":"none","zone":"UTC","anchor":"2026-01-01T00:00:00Z"},"action":"disconnect","down":"0","up":"0"}],"windows":[]}]}`
	var p Plan
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	start := p.Policies[0].Limits[0].Period.Anchor
	usage := map[string]Usage{Key("time", start): {Used: 8}, Key("data", start): {Used: 99}}
	d := Evaluate(p, "standard", at, time.Time{}, usage)
	if !d.Allowed || d.Disconnect || d.Timeout != 2 || d.Down != 1000000 || d.Up != 800000 || p.ValidSeconds != 3600 {
		t.Fatal("decimal input changed entitlement", p.ValidSeconds, d)
	}
	usage[Key("data", start)] = Usage{Used: 100}
	d = Evaluate(p, "standard", at, time.Time{}, usage)
	if d.Allowed || !d.Disconnect {
		t.Fatal("data exhaustion did not disconnect", d)
	}
	usage[Key("data", start)] = Usage{Used: 99}
	usage[Key("time", start)] = Usage{Used: 10}
	d = Evaluate(p, "standard", at, time.Time{}, usage)
	if d.Allowed || !d.Disconnect {
		t.Fatal("time exhaustion did not disconnect", d)
	}
}

func FuzzQuantityDecimalLeadingZeros(f *testing.F) {
	f.Add(uint64(8), uint8(1))
	f.Add(uint64(math.MaxInt64), uint8(1))
	f.Fuzz(func(t *testing.T, n uint64, zeros uint8) {
		n &= math.MaxInt64
		raw := `"` + strings.Repeat("0", int(zeros%16)) + fmt.Sprint(n) + `"`
		var q Quantity
		if err := json.Unmarshal([]byte(raw), &q); err != nil || uint64(q) != n {
			t.Fatalf("%s => %d: %v", raw, q, err)
		}
	})
}
