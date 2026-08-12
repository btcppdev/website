package emails

import (
	"bytes"
	"fmt"
	"html"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/mtypes"
	"btcpp-web/internal/types"
)

type ConferenceCampaignData struct {
	Conf                   *types.Conf
	CampaignTitle          string
	Email                  string
	Name                   string
	URI                    string
	DashboardLink          string
	AffiliateDashboardLink string
	DoorsOpen              string
	BreakfastStart         string
	SpeakerDinnerTime      string
	SpeakerDinnerLocation  string
	GeneratedUpdates       string
	SponsorAcknowledgement string
	TalkDetails            string
	SendAt                 time.Time
}

func SendConferenceCampaignDraftReview(ctx *config.AppContext, conf *types.Conf, occurrence *types.ConferenceEmailOccurrence, letter *mtypes.Letter) error {
	if ctx == nil || conf == nil || occurrence == nil || letter == nil {
		return fmt.Errorf("conference campaign review is incomplete")
	}
	link := fmt.Sprintf("%s/%s/admin/missives/occurrences/%s", ctx.Env.GetURI(), conf.Tag, occurrence.ID)
	sendLabel := occurrence.SendAt.In(conf.Loc()).Format("Monday, January 2 at 3:04 PM MST")
	textBody := fmt.Sprintf("A draft for %s is ready for review.\n\nScheduled send: %s\nAudience: %s\n\nView or edit: %s\n",
		conf.Desc, sendLabel, occurrence.Audience, link)
	htmlBody := fmt.Sprintf(`<p>A draft for <strong>%s</strong> is ready for review.</p><p>Scheduled send: %s<br>Audience: %s</p><p><a href="%s">View or edit MISS-%d</a></p>`,
		html.EscapeString(conf.Desc), html.EscapeString(sendLabel), html.EscapeString(occurrence.Audience), html.EscapeString(link), letter.UID)
	return ComposeAndSendMail(ctx, &Mail{
		JobKey:  "conference-email-review-" + occurrence.ID,
		Missive: letter.Missive(), Email: "inbox@btcpp.dev",
		Title: fmt.Sprintf("[%s] Event email draft ready", conf.Desc), SendAt: time.Now(),
		HTMLBody: []byte(htmlBody), TextBody: []byte(textBody),
	})
}

func SendConferenceCampaign(ctx *config.AppContext, letter *mtypes.Letter, data *ConferenceCampaignData, jobKey string, files []*EmailFile) error {
	if ctx == nil || letter == nil || data == nil || data.Email == "" || jobKey == "" {
		return fmt.Errorf("conference campaign mail is incomplete")
	}
	title := templatizeTitle(letter.Title, data)
	data.CampaignTitle = conferenceCampaignHeadline(title)
	var body bytes.Buffer
	if err := executeMissiveTemplate(ctx, letter, &body, data); err != nil {
		return fmt.Errorf("render conference campaign %s: %w", letter.Missive(), err)
	}
	htmlBody, _, err := BuildTemplatedNewsletterEmailAt(ctx, letter.ImgRef(), body.Bytes(), "", data.SendAt)
	if err != nil {
		return fmt.Errorf("build conference campaign html: %w", err)
	}
	return ComposeAndSendMail(ctx, &Mail{
		JobKey: jobKey, Missive: letter.Missive(), Email: data.Email,
		Title: title, SendAt: time.Now(), HTMLBody: htmlBody, TextBody: body.Bytes(), Files: files,
	})
}

func RenderConferenceCampaignPreview(ctx *config.AppContext, letter *mtypes.Letter, data *ConferenceCampaignData) ([]byte, error) {
	if ctx == nil || letter == nil || data == nil {
		return nil, fmt.Errorf("conference campaign preview is incomplete")
	}
	data.CampaignTitle = conferenceCampaignHeadline(templatizeTitle(letter.Title, data))
	var body bytes.Buffer
	if err := executeMissiveTemplate(ctx, letter, &body, data); err != nil {
		return nil, fmt.Errorf("render conference campaign preview: %w", err)
	}
	htmlBody, _, err := BuildTemplatedNewsletterEmailAt(ctx, letter.ImgRef(), body.Bytes(), "", data.SendAt)
	return htmlBody, err
}

func conferenceCampaignHeadline(title string) string {
	title = strings.TrimSpace(title)
	if _, headline, ok := strings.Cut(title, ": "); ok && strings.TrimSpace(headline) != "" {
		return strings.TrimSpace(headline)
	}
	return title
}
