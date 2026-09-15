package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"errors"
	"fmt"
)

func judgeCalendarSegment(kind string) bool {
	return kind == "judges-meeting" || kind == getters.JudgeTypeExpo || kind == getters.JudgeTypeFinals
}

// Link judges to the existing schedule proposals so ordinary schedule updates
// also reach them. Every judge attends the meeting and their assigned rounds.
func syncHackathonJudgeCalendars(ctx *config.AppContext, competitionID string, removing ...string) error {
	competition, err := getters.GetCompetitionByID(ctx, competitionID)
	if err != nil {
		return err
	}
	if competition == nil {
		return fmt.Errorf("hackathon not found")
	}
	conf, err := getters.GetConfByRef(ctx, competition.ConferenceID)
	if err != nil {
		return err
	}
	if conf == nil {
		return fmt.Errorf("conference not found")
	}
	judges, err := getters.ListCompetitionJudges(ctx, competitionID)
	if err != nil {
		return err
	}
	segments, err := getters.ListCompetitionScheduleSegments(ctx, competitionID)
	if err != nil {
		return err
	}
	byPerson := make(map[string]*types.CompetitionJudge)
	for _, judge := range judges {
		if judge != nil {
			byPerson[judge.PersonID] = judge
		}
	}
	removed := make(map[string]bool)
	for _, personID := range removing {
		removed[personID] = true
	}
	var failures []error
	for _, segment := range segments {
		if segment == nil || !judgeCalendarSegment(segment.SegmentType) {
			continue
		}
		if segment.ProposalID == "" {
			failures = append(failures, fmt.Errorf("%s has no schedule proposal", segment.Title))
			continue
		}
		for _, judge := range judges {
			if judge == nil || removed[judge.PersonID] || !judgeAttendsCalendarSegment(judge, segment.SegmentType) {
				continue
			}
			_, err := getters.UpsertSpeakerConf(ctx, getters.SpeakerConfInput{SpeakerID: judge.PersonID, ConfTag: conf.Tag, ProposalID: segment.ProposalID})
			if err != nil {
				failures = append(failures, fmt.Errorf("%s: attach judge: %w", segment.Title, err))
			}
		}
		proposal, err := getters.GetProposal(ctx, segment.ProposalID)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		ct, err := getters.GetConfTalkByProposal(ctx, segment.ProposalID)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		// Remove assignments that no longer belong to this round. Cancel before
		// unlinking so a failed queue request remains retryable on the next save.
		for _, sc := range resolveProposalSpeakers(proposal, ctx) {
			if sc.Speaker == nil {
				continue
			}
			judge, known := byPerson[sc.Speaker.ID]
			if !removed[sc.Speaker.ID] && (!known || judgeAttendsCalendarSegment(judge, segment.SegmentType)) {
				continue
			}
			if ct != nil && ct.Sched != nil && ct.Sched.End != nil && ct.CalNotif != "" {
				talk := &types.Talk{ID: ct.ID, Name: proposal.Title, Description: proposal.Description, Type: "hackathon", Sched: ct.Sched, Speakers: []*types.Speaker{sc.Speaker}, Venue: ct.Venue, CalNotif: ct.CalNotif}
				if err := dispatchTalkICSForTalk(ctx, talk, conf, kindCancel, false, true); err != nil {
					failures = append(failures, err)
					continue
				}
				ct, err = getters.GetConfTalkByProposal(ctx, segment.ProposalID)
				if err != nil {
					return err
				}
			}
			if err := getters.RemoveProposalFromSpeakerConf(ctx, sc.ID, segment.ProposalID); err != nil {
				failures = append(failures, err)
			}
		}
		proposal, err = getters.GetProposal(ctx, segment.ProposalID)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		// Preserve draft sessions; once placed, normal schedule invitations include
		// the attached judges. Never invent a meeting time.
		if ct == nil || ct.Sched == nil || ct.Sched.End == nil {
			continue
		}
		speakers, err := proposalSpeakers(ctx, proposal)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		eligible := speakers[:0]
		for _, speaker := range speakers {
			if speaker == nil {
				continue
			}
			judge, known := byPerson[speaker.ID]
			if !removed[speaker.ID] && (!known || judgeAttendsCalendarSegment(judge, segment.SegmentType)) {
				eligible = append(eligible, speaker)
			}
		}
		speakers = eligible
		if len(speakers) == 0 {
			continue
		}
		talk := &types.Talk{ID: ct.ID, Name: proposal.Title, Description: proposal.Description, Type: "hackathon", Sched: ct.Sched, Speakers: speakers, Venue: ct.Venue, CalNotif: ct.CalNotif}
		if err := dispatchTalkICSForTalk(ctx, talk, conf, kindRequest, false, true); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", segment.Title, err))
		}
	}
	return errors.Join(failures...)
}

func judgeCalendarFlash(ctx *config.AppContext, competitionID, message string) string {
	if err := syncHackathonJudgeCalendars(ctx, competitionID); err != nil {
		ctx.Err.Printf("hackathon %s judge calendars: %v", competitionID, err)
		return message + ". Calendar invitations did not fully complete; save judge roles again to retry."
	}
	return message + ". Judges linked to the meeting and assigned rounds; invitations queued for scheduled sessions."
}

func judgeAttendsCalendarSegment(judge *types.CompetitionJudge, kind string) bool {
	return judge != nil && (kind == "judges-meeting" || ((kind == getters.JudgeTypeExpo || kind == getters.JudgeTypeFinals) && competitionJudgeHasType(judge, kind)))
}
