package types

const (
	InsiderStreamLastWeekInBitcoin = "insider.last-week-in-bitcoin"
	InsiderStreamBitcoinCore       = "insider.bitcoin-core"
	InsiderStreamDeepDives         = "insider.deep-dives"
	InsiderStreamInterviews        = "insider.interviews"
	InsiderStreamPodcasts          = "insider.podcasts"
	InsiderStreamFridayShow        = "insider.friday-show"

	InsiderDeliveryAsPublished  = "as_published"
	InsiderDeliveryWeeklyDigest = "weekly_digest"
)

// InsiderStreamDefinition is the narrow delivery contract shared with the
// mailer. Insider owns the labels, descriptions, consent state, and cadence.
type InsiderStreamDefinition struct {
	ID string
}

func InsiderStreams() []InsiderStreamDefinition {
	return []InsiderStreamDefinition{
		{ID: InsiderStreamLastWeekInBitcoin},
		{ID: InsiderStreamBitcoinCore},
		{ID: InsiderStreamDeepDives},
		{ID: InsiderStreamInterviews},
		{ID: InsiderStreamPodcasts},
		{ID: InsiderStreamFridayShow},
	}
}

func IsInsiderStream(value string) bool {
	for _, stream := range InsiderStreams() {
		if stream.ID == value {
			return true
		}
	}
	return false
}
