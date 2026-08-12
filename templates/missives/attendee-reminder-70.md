---
kind: attendee-reminder-70
label: Event attendee reminder · 70 days
title: 70 days til bitcoin++ -- you're ready right?
order: 10
template: announce
palette: ember
issue: EVENT UPDATE
---

{{ lead "EVENT UPDATE" .CampaignTitle "We're getting closer." }}

We're only 70 days away from {{ .Conf.Desc }} in {{ .Conf.Location }}. Here's what is taking shape.

{{ .GeneratedUpdates }}

### Keep everything handy

Your dashboard has your tickets, the latest registration details, and your affiliate link. If you haven't reached out to invite your friends and colleagues to join you, now's the time.

{{ cta "YOUR EVENT" "Get ready for bitcoin++" "Review your tickets and the latest event details." "Open your dashboard" .DashboardLink }}

{{ .SponsorAcknowledgement }}
