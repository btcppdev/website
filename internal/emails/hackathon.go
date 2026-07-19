package emails

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

// SendHackathonMessage sends a transactional hackathon email through the
// existing idempotent mailer. The caller supplies a stable job key so retries
// cannot fan out duplicate messages.
func SendHackathonMessage(ctx *config.AppContext, jobKey, email, title, markdown string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil
	}
	htmlBody, err := BuildHTMLEmail(ctx, []byte(markdown))
	if err != nil {
		return fmt.Errorf("render hackathon email: %w", err)
	}
	return ComposeAndSendMail(ctx, &Mail{
		JobKey:   jobKey,
		Email:    email,
		Title:    title,
		HTMLBody: htmlBody,
		TextBody: []byte(markdown),
		SendAt:   time.Now(),
	})
}

func SendProjectSubmissionConfirmations(ctx *config.AppContext, conf *types.Conf, competition *types.HackathonCompetition, project *types.HackathonProject, members []*types.ProjectMember, projectURL string) []error {
	var errs []error
	seen := map[string]bool{}
	for _, member := range members {
		if member == nil {
			continue
		}
		email := strings.ToLower(strings.TrimSpace(member.Email))
		if email == "" || seen[email] {
			continue
		}
		seen[email] = true
		body := fmt.Sprintf("# Project submitted\n\nHi %s,\n\n**%s** has been submitted to **%s**. Everyone currently listed on the team is receiving this confirmation.\n\n[Review the submission](button#%s)\n\nIf the team needs to change after submission, contact a hackathon coordinator.", member.Name, project.Title, competition.Title, projectURL)
		if err := SendHackathonMessage(ctx, "hackathon-submission-"+project.ID+"-"+member.PersonID, member.Email, "["+conf.Tag+"] Project submitted: "+project.Title, body); err != nil {
			errs = append(errs, fmt.Errorf("send submission confirmation to %s: %w", member.Email, err))
		}
	}
	return errs
}

func SendJudgeInvitation(ctx *config.AppContext, conf *types.Conf, competition *types.HackathonCompetition, invite *types.CompetitionJudgeInvite, inviteURL string) error {
	if invite == nil || strings.TrimSpace(invite.Email) == "" {
		return nil
	}
	roles := append([]string(nil), invite.JudgeTypes...)
	sort.Strings(roles)
	roleLabel := strings.Join(roles, " and ")
	body := fmt.Sprintf("# Judge invitation\n\nYou've been invited to judge **%s** as an **%s** judge.\n\n[Accept judge invitation](button#%s)\n\nThis link is intended for %s. Sign in with that email address before accepting it.", competition.Title, roleLabel, inviteURL, invite.Email)
	return SendHackathonMessage(ctx, "hackathon-judge-invite-"+invite.ID, invite.Email, "["+conf.Tag+"] Judge invitation", body)
}

type AwardNotification struct {
	Person  *types.ProjectMember
	Project *types.HackathonProject
	Awards  []*types.Award
}

func SendAwardNotification(ctx *config.AppContext, conf *types.Conf, competition *types.HackathonCompetition, notification AwardNotification, publicURL string) error {
	if notification.Person == nil || notification.Project == nil || strings.TrimSpace(notification.Person.Email) == "" || len(notification.Awards) == 0 {
		return nil
	}
	titles := make([]string, 0, len(notification.Awards))
	ids := make([]string, 0, len(notification.Awards))
	for _, award := range notification.Awards {
		if award != nil {
			titles = append(titles, "- "+award.Title)
			ids = append(ids, award.ID)
		}
	}
	sort.Strings(titles)
	sort.Strings(ids)
	body := fmt.Sprintf("# Hackathon results are live\n\nCongratulations %s! **%s** received:\n\n%s\n\n[View the published results](button#%s)\n\nThe bitcoin++ team will follow up about prize distribution. Please make sure the private payout details on your profile are complete.", notification.Person.Name, notification.Project.Title, strings.Join(titles, "\n"), publicURL)
	return SendHackathonMessage(ctx, "hackathon-awards-"+competition.ID+"-"+notification.Person.PersonID+"-"+strings.Join(ids, "-"), notification.Person.Email, "["+conf.Tag+"] Hackathon award results", body)
}
