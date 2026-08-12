---
kind: speaker-onboarding
label: Event speaker onboarding
title: You've signed up to speak. Here's what's next
order: 70
template: announce
palette: ember
issue: SPEAKER UPDATE
---

{{ lead "SPEAKER PREP" .CampaignTitle "Welcome to the program." }}

Hi {{ .Name }},

You're officially registered to speak at {{ .Conf.Desc }}. We're so thrilled to have you.

### What to expect

- *VIP Dinner*: You're cordially invited to our VIP dinner. Typically it begins at 6.30p the night before the conference starts. We'll send out a separate invitation via Paperless Post once we've made a reservation.
- *Arriving:* Doors open at {{ .DoorsOpen }} on the first day.
- *Your Talk:* We'll send a calendar invitation once your talk time is scheduled.
- *Presenting:* If you're giving a talk or workshop, bring your own laptop to present from. We'll connect it over HDMI and have power available. We don't ask for slides ahead of time, as we typically expect you to (potentially) have xyz
- *Tickets*: You should have received a ticket for the event once you officially accepted the speaker invitation.

{{ .GeneratedUpdates }}

{{ .TalkDetails }}

{{ cta "SPEAKER DASHBOARD" "Review your talk" "Update your submission or let us know if your plans change." "Open your dashboard" .DashboardLink }}

Need help? Email speak@btcpp.dev or message niftynei.99 on Signal.
