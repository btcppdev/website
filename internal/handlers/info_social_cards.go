package handlers

import "btcpp-web/internal/imgproc"

func contactSocialCard() imgproc.SiteSocialCard {
	card := imgproc.SiteSocialCard{
		Kind: "contact", Eyebrow: "Get in touch", Title: "Contact us.",
		Subtitle: "Events, speaking, sponsorships, and technical bitcoin.",
		Callout:  "hello@btcpp.dev",
	}
	applySiteSocialCardPalette(&card, nil)
	return card
}

func developersAPISocialCard() imgproc.SiteSocialCard {
	card := imgproc.SiteSocialCard{
		Kind: "api", Eyebrow: "Developer platform", Title: "API reference.",
		Subtitle: "Conferences, people, recordings, and hackathons. Build with public data.",
		Callout:  "/api/v1", Details: []string{"/conferences", "/people", "/recordings"},
	}
	applySiteSocialCardPalette(&card, nil)
	return card
}
