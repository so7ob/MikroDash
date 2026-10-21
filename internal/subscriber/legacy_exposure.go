package subscriber

import (
	"database/sql"
	"encoding/json"
	"time"
)

// Retain the maximum already-issued exposure of known legacy sessions before
// admitting another device or planning renewal. Old current-period-only rows
// cannot be treated as proof that a later period is unreserved. Used counters,
// credentials, subscription identity and issued packets are never rewritten.
func retainLegacyExposure(tx *sql.Tx, account string, at time.Time) error {
	rows, err := tx.Query(`SELECT l.id,l.started_at,l.seconds,l.download,l.upload,l.deadline,l.decision,p.definition
FROM subscriber_leases l JOIN subscriber_subscriptions sub ON sub.id=l.subscription_id JOIN subscriber_plans p ON p.id=sub.plan_id AND p.version=sub.plan_version
WHERE sub.account_id=? AND l.state='active' AND coalesce(json_extract(l.decision,'$.timingBasis'),'')!=?`, account, admissionTimingBasis)
	if err != nil {
		return err
	}
	type legacy struct {
		id                string
		started, deadline int64
		seconds, down, up Quantity
		grantRaw, planRaw string
	}
	items := []legacy{}
	for rows.Next() {
		var item legacy
		if err = rows.Scan(&item.id, &item.started, &item.seconds, &item.down, &item.up, &item.deadline, &item.grantRaw, &item.planRaw); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range items {
		var initial Admission
		var plan Plan
		if err = json.Unmarshal([]byte(item.grantRaw), &initial); err != nil {
			return err
		}
		if err = json.Unmarshal([]byte(item.planRaw), &plan); err != nil {
			return err
		}
		if item.started <= 0 || item.seconds < 0 || item.seconds > 315360000 || item.down < 0 || item.up < 0 || initial.Download <= 0 || initial.Upload <= 0 || initial.Decision.Timeout <= 0 || initial.Decision.Timeout > 60 || plan.Validate() != nil {
			return denyAdmission("legacy-authorization-requires-review")
		}
		end := max(item.deadline, item.started+initial.Decision.Timeout)
		down, up, total := initial.Download, initial.Upload, initial.Total
		issued, err := tx.Query(`SELECT target_deadline,request FROM subscriber_operations WHERE lease_id=? AND kind='renew' AND attempts>0`, item.id)
		if err != nil {
			return err
		}
		for issued.Next() {
			var deadline int64
			var raw string
			var request OperationRequest
			if err = issued.Scan(&deadline, &raw); err != nil {
				issued.Close()
				return err
			}
			if err = json.Unmarshal([]byte(raw), &request); err != nil {
				issued.Close()
				return err
			}
			end, down, up, total = max(end, deadline), max(down, request.DownloadCeiling), max(up, request.UploadCeiling), max(total, request.TotalCeiling)
		}
		err = issued.Err()
		issued.Close()
		if err != nil {
			return err
		}
		from := item.started + int64(item.seconds)
		if end-from > 120 {
			return denyAdmission("legacy-authorization-requires-review")
		}
		for _, policy := range plan.Policies {
			for _, limit := range policy.Limits {
				if !hardLimit(limit) {
					continue
				}
				for cursor := from; cursor < end || (limit.Kind != "seconds" && cursor == end); {
					start, next, err := limit.Period.Bounds(time.Unix(cursor, 0))
					if err != nil || next.Unix() <= cursor {
						return denyAdmission("legacy-authorization-requires-review")
					}
					edge := min(end, next.Unix())
					amount := Quantity(edge - cursor)
					switch limit.Kind {
					case "download":
						amount = max(0, down-item.down)
					case "upload":
						amount = max(0, up-item.up)
					case "total":
						used, err := add(item.down, item.up)
						if err != nil {
							return err
						}
						amount, err = add(max(0, down-item.down), max(0, up-item.up))
						if err != nil {
							return err
						}
						if total > 0 {
							amount = min(amount, max(0, total-used))
						}
					}
					if _, err = tx.Exec(`INSERT INTO subscriber_reservations VALUES(?,?,?,?) ON CONFLICT(lease_id,limit_id,period_start) DO UPDATE SET amount=max(amount,excluded.amount)`, item.id, limit.ID, start.Unix(), amount); err != nil {
						return err
					}
					cursor = next.Unix()
				}
			}
		}
		initial.TimingBasis = admissionTimingBasis
		raw, err := json.Marshal(initial)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE subscriber_leases SET decision=? WHERE id=?`, string(raw), item.id); err != nil {
			return err
		}
		if err = audit(tx, "service:subscriber", "admission.legacy.exposure-retained", item.id, "known start and maximum issued caps; usage unchanged", at); err != nil {
			return err
		}
	}
	return nil
}
