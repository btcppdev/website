---
kind: volunteer-orientation
label: Event volunteer orientation reminder
title: Volunteer Orientation Next Week!
order: 60
template: announce
palette: ember
issue: VOLUNTEER UPDATE
---

{{ lead "VOLUNTEER UPDATE" .CampaignTitle "Volunteer orientation is next week." }}

Hi {{ .Name }},

This is a reminder about volunteer orientation for {{ .Conf.Desc }}.

{{ .GeneratedUpdates }}

{{ cta "VOLUNTEER DASHBOARD" "Get ready for orientation" "Review your schedule and the latest volunteer details." "Open your dashboard" .DashboardLink }}
