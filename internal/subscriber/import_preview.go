package subscriber

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mikrodash/internal/routeros"
)

type ImportProblem struct {
	Menu  string `json:"menu"`
	Row   int    `json:"row"` // zero-based index in the corresponding returned rows
	Field string `json:"field"`
	Code  string `json:"code"`
}

type ImportPreview struct {
	Rows               map[string][]routeros.Reply `json:"rows"`
	Warnings           []string                    `json:"warnings"`
	Unavailable        []string                    `json:"unavailable"`
	Problems           []ImportProblem             `json:"problems"`
	FingerprintVersion int                         `json:"fingerprintVersion"`
	Fingerprint        string                      `json:"fingerprint"`
}

var userManagerPreviewMenus = []struct{ path, fields string }{
	{"/user-manager/profile/print", ".id,name,validity,starts-when,override-shared-users"},
	{"/user-manager/limitation/print", ".id,name,uptime-limit,download-limit,upload-limit,transfer-limit,reset-counters-interval,reset-counters-start-time,rate-limit-rx,rate-limit-tx,rate-limit-priority"},
	{"/user-manager/profile-limitation/print", ".id,profile,limitation,from-time,till-time,weekdays"},
	{"/user-manager/user/print", ".id,name,disabled,shared-users"},
	{"/user-manager/user-profile/print", ".id,user,profile,state,end-time"},
}

// PreviewUserManager reads source definitions only. Problems are source-review
// diagnostics, never an import approval or a conversion of source quota semantics.
func PreviewUserManager(reader Reader) ImportPreview {
	p := ImportPreview{
		Rows: map[string][]routeros.Reply{}, Unavailable: []string{}, Problems: []ImportProblem{},
		FingerprintVersion: 2,
		Warnings: []string{
			"Preview only. Historical totals are not current-period opening balances.",
			"Credentials are not read; migrated subscribers need a new credential or an approved credential handover.",
			"Menu reads are sequential, not an atomic source snapshot. No mapping or import is approved by this report.",
		},
	}
	for _, menu := range userManagerPreviewMenus {
		rows, err := reader.Do(routeros.Cmd{Path: menu.path, Args: []string{"=.proplist=" + menu.fields}, Timeout: 5 * time.Second})
		if err != nil {
			p.Unavailable = append(p.Unavailable, menu.path)
			p.Warnings = append(p.Warnings, "Unavailable menu: "+menu.path)
			continue
		}
		// Project returned data as well as requesting a proplist. An unexpected NAS
		// field must not leak a credential into a review or change its fingerprint.
		clean := make([]routeros.Reply, 0, len(rows))
		fields := strings.Split(menu.fields, ",")
		for _, row := range rows {
			copy := routeros.Reply{}
			for _, field := range fields {
				if value, ok := row[field]; ok {
					copy[field] = value
				}
			}
			clean = append(clean, copy)
		}
		p.Rows[menu.path] = clean
		previewIdentities(&p, menu.path, clean)
		if menu.path == "/user-manager/limitation/print" {
			for _, row := range clean {
				name := row["name"]
				if strings.Contains(strings.ToLower(name), "1min") && row["uptime-limit"] != "1m" {
					p.Warnings = append(p.Warnings, "Name does not match uptime value: "+name)
				}
				if strings.Contains(strings.ToLower(name), "daily") && row["reset-counters-interval"] != "daily" {
					p.Warnings = append(p.Warnings, "Name does not match reset interval: "+name)
				}
			}
		}
	}
	// Structured encoding distinguishes menu boundaries, missing vs empty reads,
	// missing vs explicit-zero properties and ambiguous textual map renderings.
	// Preserve row order: preview does not assume source ordering is irrelevant.
	material, _ := json.Marshal(struct {
		Version     int                         `json:"version"`
		Rows        map[string][]routeros.Reply `json:"rows"`
		Unavailable []string                    `json:"unavailable"`
	}{p.FingerprintVersion, p.Rows, p.Unavailable}) // Only strings/integers/slices/maps: encoding cannot fail.
	p.Fingerprint = fmt.Sprintf("%x", sha256.Sum256(material))
	return p
}

func previewIdentities(p *ImportPreview, menu string, rows []routeros.Reply) {
	fields := []string{".id"}
	switch menu {
	case "/user-manager/profile/print", "/user-manager/limitation/print", "/user-manager/user/print":
		fields = append(fields, "name")
	}
	for _, field := range fields {
		counts := map[string]int{}
		for _, row := range rows {
			counts[row[field]]++
		}
		for index, row := range rows {
			code := ""
			if row[field] == "" {
				code = "missing-identity"
			} else if counts[row[field]] > 1 {
				code = "duplicate-identity"
			}
			if code != "" {
				p.Problems = append(p.Problems, ImportProblem{Menu: menu, Row: index, Field: field, Code: code})
			}
		}
	}
	// References stay raw; identifying their target or quota mapping is a later
	// explicit review. Missing link fields are never silently treated as defaults.
	var references []string
	switch menu {
	case "/user-manager/profile-limitation/print":
		references = []string{"profile", "limitation"}
	case "/user-manager/user-profile/print":
		references = []string{"user", "profile"}
	}
	for index, row := range rows {
		for _, field := range references {
			if row[field] == "" {
				p.Problems = append(p.Problems, ImportProblem{Menu: menu, Row: index, Field: field, Code: "missing-reference"})
			}
		}
	}
}
