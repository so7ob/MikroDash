package server

import (
	"mikrodash/internal/routeros"
	"testing"
)

func TestSubscriberQueueReadbackRequiresUniqueExactDevice(t *testing.T) {
	valid := routeros.Reply{"target": "198.51.100.2/32", "max-limit": "2000000/8000000", "dynamic": "true"}
	up, down, ok := subscriberQueueRate([]routeros.Reply{valid}, "198.51.100.2")
	if !ok || up != 2000000 || down != 8000000 {
		t.Fatal("queue direction or units", up, down, ok)
	}
	for _, rows := range [][]routeros.Reply{{valid, valid}, {{"target": "198.51.100.0/24", "max-limit": "2000000/8000000", "dynamic": "true"}}, {{"target": "198.51.100.2/32", "max-limit": "0/0", "dynamic": "true"}}, {{"target": "198.51.100.2/32", "max-limit": "2M/8M", "dynamic": "true"}}, {{"target": "198.51.100.2/32", "max-limit": "2000000/8000000", "dynamic": "false"}}} {
		if _, _, ok := subscriberQueueRate(rows, "198.51.100.2"); ok {
			t.Fatal("ambiguous/shared/unlimited/static queue treated as verified", rows)
		}
	}
	if _, ok := subscriberCounter("9223372036854775808"); ok {
		t.Fatal("overflow accepted")
	}
	if n, ok := subscriberCounter("9007199254740993"); !ok || n != 9007199254740993 {
		t.Fatal("large integer lost precision", n)
	}
	if n, ok := subscriberCounter("0"); !ok || n != 0 {
		t.Fatal("explicit zero became unknown")
	}
	if _, ok := subscriberCounter(""); ok {
		t.Fatal("missing counter became zero")
	}
}
