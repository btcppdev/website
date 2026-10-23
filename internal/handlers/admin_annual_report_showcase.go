package handlers

import "time"

type annualShowcaseEvent struct {
	Event  reportEvent
	Status string
}

// Future events belong in the year's story, but never in completed-event totals.
// Use the edition's collection date so a frozen review does not change tense.
func (p annualReportPage) Forthcoming() []annualShowcaseEvent {
	asOf, err := time.Parse(time.RFC3339, p.Generated)
	if err != nil {
		asOf = time.Now()
	}
	var out []annualShowcaseEvent
	for _, e := range p.Events {
		if e.Included || e.Event.Start == nil || e.Event.Start.In(reportEventLocation(e.Event)).Year() != p.Year {
			continue
		}
		if e.Event.Start.After(asOf) {
			out = append(out, annualShowcaseEvent{Event: e.Event, Status: "Upcoming"})
		}
	}
	return out
}
func (p annualReportPage) CoverEvents() []annualShowcaseEvent {
	var out []annualShowcaseEvent
	for _, e := range p.Included() {
		out = append(out, annualShowcaseEvent{Event: e.Event})
	}
	return append(out, p.Forthcoming()...)
}
