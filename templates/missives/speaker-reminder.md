---
kind: speaker-reminder
label: Event speaker reminder
title: 3 Weeks To Go
order: 40
template: announce
palette: ember
issue: SPEAKER UPDATE
---

{{ lead "SPEAKER UPDATE" .CampaignTitle "Three weeks to go." }}

Hi {{ .Name }},

Your current talk information is below. Please give everything a careful review—especially the scheduled time, session format, and any co-speakers. You can use your dashboard to keep your slides and GitHub repository attached to the talk as those materials become available.

{{ .GeneratedUpdates }}

{{ .TalkDetails }}

### Speaker dinner

The speaker dinner begins at {{ .SpeakerDinnerTime }} the night before the event at {{ .SpeakerDinnerLocation }}.

{{ cta "SPEAKER DASHBOARD" "Review your talk" "Confirm your session details and add or update your slides and GitHub repository." "Open your dashboard" .DashboardLink }}

Need help? Email speak@btcpp.dev or message niftynei.99 on Signal.
