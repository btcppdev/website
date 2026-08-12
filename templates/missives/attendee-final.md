---
kind: attendee-final
label: Event final details and tickets
title: your moment at bitcoin++ has arrived
order: 50
template: announce
palette: ember
issue: EVENT DETAILS
---

{{ lead "EVENT DETAILS" .CampaignTitle "Everything you need for next week." }}

Hi there,

{{ .Conf.Desc }} starts next week at {{ .Conf.Venue }} in {{ .Conf.Location }}.

{{ .GeneratedUpdates }}

### Your tickets

Your ticket PDFs are attached to this email.

{{ cta "YOUR EVENT" "You're all set" "Review your tickets and the latest event details before you arrive." "Open your dashboard" .DashboardLink }}

{{ .SponsorAcknowledgement }}
