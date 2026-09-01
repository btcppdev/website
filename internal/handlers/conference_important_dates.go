package handlers

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"btcpp-web/internal/types"
)

func buildConferenceImportantDates(conf *types.Conf, custom []*types.ConferenceMilestone, now time.Time) []*ConferenceImportantDate {
	if conf == nil {
		return nil
	}
	loc := conf.Loc()
	seen := make(map[string]bool)
	dates := make([]*ConferenceImportantDate, 0, len(custom)+len(conf.Tickets)+2)
	add := func(label, detail, category, target string, at time.Time) {
		if at.IsZero() || strings.TrimSpace(label) == "" {
			return
		}
		at = at.In(loc)
		key := category + "|" + at.UTC().Format(time.RFC3339)
		if seen[key] {
			return
		}
		seen[key] = true
		dates = append(dates, &ConferenceImportantDate{
			Label: label, Detail: detail, Category: category, URL: target, OccursAt: at,
			DateLabel: at.Format("Mon · Jan 2, 2006"), TimeLabel: at.Format("3:04 PM MST"),
		})
	}

	for _, milestone := range custom {
		if milestone == nil || !milestone.Published {
			continue
		}
		target := strings.TrimSpace(milestone.URL)
		if target == "" {
			target = conferenceImportantDateDefaultURL(conf, milestone.Category)
		}
		add(milestone.Label, "", milestone.Category, target, milestone.OccursAt)
	}

	if !conf.StartDate.IsZero() {
		add("Talk applications close", "Final proposals due", "talks", "/talk/"+conf.Tag, conf.TalksDueDate())
	}

	tickets := append(types.ConfTickets(nil), conf.Tickets...)
	sort.Sort(tickets)
	for index, ticket := range tickets {
		if ticket == nil || ticket.SalesEndAt.IsZero() {
			continue
		}
		if index+1 < len(tickets) && tickets[index+1] != nil {
			next := tickets[index+1]
			detail := fmt.Sprintf("%s → %s", conferenceTicketPriceLabel(ticket), conferenceTicketPriceLabel(next))
			add("Ticket price increases", detail, "tickets", "#tickets", ticket.SalesEndAt)
			continue
		}
		add("Ticket sales close", "Last chance to register", "tickets", "#tickets", ticket.SalesEndAt)
	}

	if !conf.StartDate.IsZero() {
		add("Conference begins", conf.Location, "event", "#agenda", conf.StartDate)
	}

	sort.SliceStable(dates, func(i, j int) bool { return dates[i].OccursAt.Before(dates[j].OccursAt) })
	nextMarked := false
	for _, date := range dates {
		date.IsPast = date.OccursAt.Before(now)
		if !date.IsPast && !nextMarked {
			date.IsNext = true
			date.Status = "Up next"
			nextMarked = true
		} else if date.IsPast {
			date.Status = "Passed"
		} else {
			date.Status = "Upcoming"
		}
	}
	return dates
}

func conferenceImportantDateDefaultURL(conf *types.Conf, category string) string {
	switch category {
	case "tickets":
		return "#tickets"
	case "talks":
		return "/talk/" + conf.Tag
	default:
		return "#about"
	}
}

func conferenceTicketPriceLabel(ticket *types.ConfTicket) string {
	if ticket == nil {
		return "next tier"
	}
	return strings.TrimSpace(ticket.Symbol + fmt.Sprint(ticket.StandardPrice()) + ticket.PostSymbol)
}
