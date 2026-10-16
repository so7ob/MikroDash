package activity

import (
	"path"
	"regexp"
	"strings"
)

const DefaultFolder = "usb1-part1/Log"

func ValidFolder(v string) bool {
	return v != "" && len(v) <= 240 && path.Clean(v) == v && !strings.HasPrefix(v, "/") && !strings.Contains(v, "..") && !strings.ContainsAny(v, "\x00\n\r\\")
}

// A checkpoint describes content, not a rotating filename. Offset ends after a
// complete line; partial UTF-8 and partial JSON remain on the router for retry.
type Checkpoint struct {
	Generation string `json:"generation"`
	Name       string `json:"name"`
	Offset     int64  `json:"offset"`
	PrefixLen  int    `json:"prefixLen"`
	PrefixHash string `json:"prefixHash"`
	TailHash   string `json:"tailHash"`
	Size       int64  `json:"size"`
	Modified   string `json:"modified"`
}

var archiveLine = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}(?:\.\d+)?|[A-Za-z]{3}/\d{1,2}(?:/\d{4})? \d{2}:\d{2}:\d{2}|\d{2}:\d{2}:\d{2})\s+\[?([A-Za-z0-9,._-]+)\]?\s+(.*)$`)

func SplitArchiveLine(line string) (stamp, topics, message string, ok bool) {
	line = strings.TrimPrefix(strings.TrimSuffix(line, "\r"), "\ufeff")
	m := archiveLine.FindStringSubmatch(line)
	if len(m) != 4 {
		return "", "", "", false
	}
	return strings.ReplaceAll(m[1], "T", " "), m[2], m[3], true
}
