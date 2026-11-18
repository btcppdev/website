package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/eventplan"
	"btcpp-web/internal/types"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

type StaffingChange struct {
	Action string          `json:"action"`
	Shift  eventplan.Shift `json:"shift"`
	Reason string          `json:"reason"`
}

func ReconcileEventStaffing(app *config.AppContext, conf *types.Conf, revision int64, apply bool) ([]StaffingChange, error) {
	c := app.DatabaseContext()
	tx, e := app.DB.Begin(c)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(c)
	var savedRevision int64
	var raw []byte
	if e = tx.QueryRow(c, `SELECT revision,data FROM event_plans WHERE conference_id=$1 FOR UPDATE`, conf.Ref).Scan(&savedRevision, &raw); e != nil {
		return nil, e
	}
	if revision != savedRevision {
		return nil, ErrEventPlanConflict
	}
	var plan eventplan.Plan
	if e = json.Unmarshal(raw, &plan); e != nil {
		return nil, e
	}
	wanted, e := eventplan.Staffing(plan, conf.Loc())
	if e != nil {
		return nil, e
	}
	type current struct {
		id       string
		shift    eventplan.Shift
		hash     string
		assigned bool
	}
	existing := map[string]current{}
	unmanaged := []current{}
	rows, e := tx.Query(c, `SELECT s.id::text,coalesce(s.generation_key,''),s.name,coalesce(j.tag,''),s.shift_start,s.shift_end,s.max_vols,s.priority,s.generation_hash,EXISTS(SELECT 1 FROM work_shifts_volunteers a WHERE a.shift_id=s.id) FROM work_shifts s LEFT JOIN job_types j ON j.id=s.job_type_id WHERE s.conference_id=$1 FOR UPDATE OF s`, conf.Ref)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var x current
		var a, b *time.Time
		if e = rows.Scan(&x.id, &x.shift.Key, &x.shift.Name, &x.shift.Job, &a, &b, &x.shift.Count, &x.shift.Priority, &x.hash, &x.assigned); e != nil {
			rows.Close()
			return nil, e
		}
		if a != nil {
			x.shift.Start = *a
		}
		if b != nil {
			x.shift.End = *b
		}
		if x.shift.Key == "" {
			unmanaged = append(unmanaged, x)
		} else {
			existing[x.shift.Key] = x
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(wanted) == 0 && len(existing) == 0 {
		return nil, fmt.Errorf("add venue days and access hours before generating staffing")
	}
	changes := []StaffingChange{}
	seen := map[string]bool{}
	for _, s := range wanted {
		seen[s.Key] = true
		x, ok := existing[s.Key]
		change := StaffingChange{Action: "add", Shift: s}
		if ok {
			switch {
			case x.shift.Hash() == s.Hash():
				change.Action = "keep"
			case x.assigned:
				change.Action = "review"
				change.Reason = "Has assigned volunteers; adjust this shift manually."
			case x.shift.Hash() != x.hash:
				change.Action = "review"
				change.Reason = "Manually edited; your changes are preserved."
			default:
				change.Action = "update"
			}
		}
		if !ok {
			for _, manual := range unmanaged {
				if manual.shift.Job == s.Job && manual.shift.Start.Before(s.End) && manual.shift.End.After(s.Start) {
					change.Action = "review"
					change.Reason = "An existing shift for this job overlaps; review it before adding more coverage."
					break
				}
			}
		}
		changes = append(changes, change)
		if !apply || (change.Action != "add" && change.Action != "update") {
			continue
		}
		var jobID string
		if e = tx.QueryRow(c, `INSERT INTO job_types(tag,title,show) VALUES($1,$2,true) ON CONFLICT(tag) DO UPDATE SET tag=excluded.tag RETURNING id::text`, s.Job, map[string]string{"setup": "Setup", "teardown": "Teardown", "checkin": "Check-in", "merch": "Merch desk", "showrunner": "Showrunner", "avdesk": "A/V monitor"}[s.Job]).Scan(&jobID); e != nil {
			return nil, e
		}
		_, e = tx.Exec(c, `INSERT INTO work_shifts(conference_id,job_type_id,name,max_vols,shift_start,shift_end,priority,generation_key,generation_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(conference_id,generation_key) WHERE generation_key IS NOT NULL DO UPDATE SET job_type_id=excluded.job_type_id,name=excluded.name,max_vols=excluded.max_vols,shift_start=excluded.shift_start,shift_end=excluded.shift_end,priority=excluded.priority,generation_hash=excluded.generation_hash`, conf.Ref, jobID, s.Name, s.Count, s.Start, s.End, s.Priority, s.Key, s.Hash())
		if e != nil {
			return nil, e
		}
	}
	keys := []string{}
	for key := range existing {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		x := existing[key]
		if seen[key] {
			continue
		}
		change := StaffingChange{Action: "remove", Shift: x.shift}
		if x.assigned || x.hash != x.shift.Hash() {
			change.Action = "review"
			change.Reason = "No longer in the plan; preserved because it has assignments or manual edits."
		}
		changes = append(changes, change)
		if apply && change.Action == "remove" {
			if _, e = tx.Exec(c, `DELETE FROM work_shifts WHERE id=$1`, x.id); e != nil {
				return nil, e
			}
		}
	}
	if apply {
		if e = tx.Commit(c); e != nil {
			return nil, e
		}
	}
	return changes, nil
}
