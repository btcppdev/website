Hi {{ .Name }},

You've signed up to speak at {{ .Conf.Desc }}. Here's what to expect:

- The speaker dinner begins at {{ .SpeakerDinnerTime }} the night before the event at {{ .SpeakerDinnerLocation }}.
- Doors open at {{ .DoorsOpen }} on the first day.
- We'll send a calendar invitation once your talk time is scheduled.
- Bring your own laptop. We'll connect it over HDMI and have power available.
- Your ticket is included with your talk.

{{ .GeneratedUpdates }}

{{ .TalkDetails }}

[Review, update, or decline your talk]({{ .DashboardLink }}).

Need help? Email speak@btcpp.dev or message niftynei.99 on Signal.
