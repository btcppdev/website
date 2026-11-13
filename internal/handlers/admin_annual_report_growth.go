package handlers

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type annualGrowthPoint struct {
	Event, Date string
	Estimated   bool
	Value       int64
	X, Y        float64
}

type annualGrowthTick struct {
	Label string
	X     float64
}

type annualGrowthChart struct {
	Kind, Label, Unit string
	Estimated         bool
	Year              int
	Total             int64
	Points            []annualGrowthPoint
	Ticks             []annualGrowthTick
}

func (c annualGrowthChart) Display() string { return approximateReportCount(c.Total, c.Estimated) }
func (p annualGrowthPoint) Display() string {
	value := fmt.Sprint(p.Value)
	if p.Estimated {
		return "~" + value
	}
	return value
}

func (c annualGrowthChart) Line() string {
	parts := []string{"40,164"}
	previousY := 164.0
	for _, point := range c.Points {
		// Totals change when an event closes; do not imply daily measurements.
		parts = append(parts, fmt.Sprintf("%.2f,%.2f", point.X, previousY), fmt.Sprintf("%.2f,%.2f", point.X, point.Y))
		previousY = point.Y
	}
	if len(c.Points) > 0 {
		parts = append(parts, fmt.Sprintf("440,%.2f", c.Points[len(c.Points)-1].Y))
	}
	return strings.Join(parts, " ")
}

func (c annualGrowthChart) Area() string { return c.Line() + " 440,164 40,164" }

func (p annualReportPage) RegistrationGrowth() *annualGrowthChart {
	if p.RegistrationBased {
		return p.eventGrowth("registrations", "registrations", "registrations", func(e annualEvent) (int64, bool) { return e.Registrations, true })
	}
	return p.eventGrowth("registrations", "event attendances", "event attendances", func(e annualEvent) (int64, bool) { return e.Attendees, true })
}

func (p annualReportPage) PrizeGrowth() *annualGrowthChart {
	label := p.PrizeUnit()
	if p.OfferedBased && p.Recipient == "NYDIG" {
		label = "in prizes"
	}
	return p.eventGrowth("prizes", label, "sats", func(e annualEvent) (int64, bool) { return e.Sats, e.HackKnown() })
}

func (p annualReportPage) BuildingGrowth() []annualGrowthChart {
	var out []annualGrowthChart
	for _, kind := range []string{"projects", "participants"} {
		label := "submitted projects"
		if kind == "participants" {
			label = "hackathon participations"
		}
		value := func(e annualEvent) (int64, bool) {
			if kind == "participants" {
				return e.Participants, e.HackKnown()
			}
			return e.Projects, e.HackKnown()
		}
		chart := p.eventGrowth(kind, label, label, value)
		if chart == nil {
			// Keep a known headline when there are too few dated observations
			// to chart it, even if the other building metric has a full series.
			chart = &annualGrowthChart{Kind: kind, Label: label, Unit: label, Year: p.Year}
			for _, e := range p.Included() {
				if n, known := value(e); known {
					chart.Total += n
					chart.Estimated = chart.Estimated || (kind == "participants" && e.ParticipantsEstimated)
				}
			}
		}
		if chart.Total > 0 {
			out = append(out, *chart)
		}
	}
	return out
}

// These are cumulative event totals ordered by local event end date, not a
// historical series of signups, prize payments, or live analytics snapshots.
// Each series uses the same event inclusion and hackathon coverage as its total.
func (p annualReportPage) eventGrowth(kind, label, unit string, value func(annualEvent) (int64, bool)) *annualGrowthChart {
	type datedValue struct {
		date      time.Time
		event     string
		value     int64
		estimated bool
	}
	var values []datedValue
	for _, e := range p.Included() {
		n, known := value(e)
		if !known || n == 0 {
			continue
		}
		if n < 0 || e.Event.End == nil {
			return nil
		}
		date, err := time.Parse(time.DateOnly, e.Event.End.In(reportEventLocation(e.Event)).Format(time.DateOnly))
		if err != nil || date.Year() != p.Year {
			return nil
		}
		values = append(values, datedValue{date: date, event: e.Event.Location, value: n, estimated: kind == "participants" && e.ParticipantsEstimated})
	}
	if len(values) < 2 {
		return nil
	}
	sort.SliceStable(values, func(i, j int) bool { return values[i].date.Before(values[j].date) })
	start := time.Date(p.Year, time.January, 1, 0, 0, 0, 0, time.UTC)
	end, err := time.Parse(time.DateOnly, p.Cutoff)
	if err != nil {
		end = values[len(values)-1].date
	}
	if end.Year() != p.Year || !end.After(start) || end.Before(values[len(values)-1].date) {
		return nil
	}
	chart := &annualGrowthChart{Kind: kind, Label: label, Unit: unit, Year: p.Year}
	for _, point := range values {
		chart.Total += point.value
		chart.Estimated = chart.Estimated || point.estimated
		// Events ending on the same day are one cumulative observation.
		date := point.date.Format(time.DateOnly)
		if last := len(chart.Points) - 1; last >= 0 && chart.Points[last].Date == date {
			chart.Points[last].Value = chart.Total
			chart.Points[last].Estimated = chart.Estimated
			chart.Points[last].Event += " · " + point.event
			continue
		}
		x := 40 + 400*point.date.Sub(start).Seconds()/end.Sub(start).Seconds()
		chart.Points = append(chart.Points, annualGrowthPoint{Event: point.event, Date: date, Value: chart.Total, X: x, Estimated: chart.Estimated})
	}
	if len(chart.Points) < 2 || chart.Total <= 0 {
		return nil
	}
	for i := range chart.Points {
		chart.Points[i].Y = 164 - 144*float64(chart.Points[i].Value)/float64(chart.Total)
	}
	dateFormat := "Jan"
	if end.Sub(start) < 90*24*time.Hour {
		dateFormat = "Jan 2"
	}
	for _, f := range []float64{0, .5, 1} {
		date := start.Add(time.Duration(float64(end.Sub(start)) * f))
		chart.Ticks = append(chart.Ticks, annualGrowthTick{Label: date.Format(dateFormat), X: 40 + 400*f})
	}
	return chart
}
