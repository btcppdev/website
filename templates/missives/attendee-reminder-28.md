---
kind: attendee-reminder-28
label: Event attendee reminder · 28 days
title: the final countdown has started
order: 30
template: announce
palette: ember
issue: EVENT UPDATE
---

{{ lead "EVENT UPDATE" .CampaignTitle "One month to go. Here's what to expect." }}

Hi there,

The event is coming up in {{ .Conf.Location }}. Here's the latest.

{{ .GeneratedUpdates }}

### Keep everything handy

Your dashboard has your tickets, the latest registration details, and your affiliate link.

{{ cta "YOUR EVENT" "Get ready for bitcoin++" "Review your tickets and the latest event details." "Open your dashboard" .DashboardLink }}

{{ .SponsorAcknowledgement }}
