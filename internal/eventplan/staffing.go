package eventplan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type Shift struct {
	Key      string    `json:"key"`
	Job      string    `json:"job"`
	Name     string    `json:"name"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Count    int       `json:"count"`
	Priority int       `json:"priority"`
}

func (s Shift) Hash() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%d|%d", s.Job, s.Name, s.Start.UTC().Format(time.RFC3339), s.End.UTC().Format(time.RFC3339), s.Count, s.Priority)))
	return hex.EncodeToString(sum[:])
}
func Staffing(p Plan, loc *time.Location) ([]Shift, error) {
	if e := ValidateVenues(p.Venues, loc); e != nil {
		return nil, e
	}
	hours := p.ShiftHours
	if hours < 1 || hours > 8 {
		hours = 4
	}
	out := []Shift{}
	for _, v := range p.Venues {
		for _, d := range v.Days {
			at := func(value string) *time.Time { t, _ := LocalTime(d.Date+"T"+value, loc); return t }
			add := func(job, label, start, end string, n, priority int) {
				if start == "" || end == "" {
					return
				}
				a, b := at(start), at(end)
				if a == nil || b == nil || !b.After(*a) {
					return
				}
				for i, t := 0, *a; t.Before(*b); i++ {
					next := t.Add(time.Duration(hours) * time.Hour)
					if next.After(*b) {
						next = *b
					}
					out = append(out, Shift{Key: fmt.Sprintf("%s/%s/%s/%s/%d", v.ID, d.Date, job, label, i), Job: job, Name: fmt.Sprintf("%s · %s · %s", label, v.Name, d.Date), Start: t, End: next, Count: n, Priority: priority})
					t = next
				}
			}
			if d.DoorsOpen == "" {
				add("setup", "Setup", d.StaffOpen, d.StaffClose, 4, 0)
				continue
			}
			add("setup", "Setup", d.StaffOpen, d.DoorsOpen, 4, 0)
			add("teardown", "Teardown", d.DoorsClose, d.StaffClose, 4, 0)
			checkinStart := d.DoorsOpen
			if d.StaffOpen != "" {
				early := at(d.DoorsOpen).Add(-30 * time.Minute)
				if early.Before(*at(d.StaffOpen)) {
					early = *at(d.StaffOpen)
				}
				checkinStart = early.Format("15:04")
			}
			add("checkin", "Check-in", checkinStart, d.DoorsClose, 3, 1)
			if p.MerchDesk {
				add("merch", "Merch desk", d.DoorsOpen, d.DoorsClose, 2, 1)
			}
			for _, stage := range strings.Split(v.Stages, ",") {
				stage = strings.TrimSpace(stage)
				if stage == "" {
					continue
				}
				add("showrunner", stage+" showrunner", d.DoorsOpen, d.DoorsClose, 1, 2)
				add("avdesk", stage+" A/V", d.DoorsOpen, d.DoorsClose, 1, 3)
			}
		}
	}
	return out, nil
}
