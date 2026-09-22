package handlers

import (
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

// AdminInviteSpeaker renders the form at /admin/{tag}/invite-speaker.
// Organizers use it to originate a speaker invitation: a fresh proposal
// with placeholder Title/Description lands in Invited status, the
// speaker gets a magic-link email, and the admin sees the same link on
// the post-submit page so they can copy it into Signal/Twitter as a
// personal nudge.
func AdminInviteSpeaker(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	page := &AdminInviteSpeakerPage{
		Conf:                conf,
		PresentationTypes:   helpers.GetPresentationTypes(),
		AttachableProposals: attachableProposals(ctx, conf),
		Year:                helpers.CurrentYear(),
	}
	if selected := strings.TrimSpace(r.URL.Query().Get("proposal")); selected != "" {
		found := false
		for _, p := range page.AttachableProposals {
			if p.ID == selected {
				found = true
				break
			}
		}
		if !found {
			http.Error(w, "Talk is not available for invitations in this event.", http.StatusBadRequest)
			return
		}
		page.Form.AttachProposalID = selected
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/invite_speaker.tmpl", page); err != nil {
		ctx.Err.Printf("/%s/admin/invite-speaker render: %s", conf.Tag, err)
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

// AdminInviteSpeakerSubmit is the POST counterpart: looks up or creates
// the speaker, creates a fresh Invited proposal (or attaches to an
// existing one), upserts the SpeakerConf with InvitedAt = now, mints
// the InviteToken, sends the talkinvited-direct letter, and redirects
// to the sent page.
func AdminInviteSpeakerSubmit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("Name"))
	email := strings.TrimSpace(r.FormValue("Email"))
	speakerID := strings.TrimSpace(r.FormValue("SpeakerID"))
	attachProposalID := strings.TrimSpace(r.FormValue("AttachProposalID"))
	talkType := strings.TrimSpace(r.FormValue("TalkType"))
	note := strings.TrimSpace(r.FormValue("Note"))

	formErr := func(msg string) {
		page := &AdminInviteSpeakerPage{
			Conf:                conf,
			PresentationTypes:   helpers.GetPresentationTypes(),
			AttachableProposals: attachableProposals(ctx, conf),
			FormError:           msg,
			Year:                helpers.CurrentYear(),
		}
		page.Form.Name = name
		page.Form.Email = email
		page.Form.SpeakerID = speakerID
		page.Form.AttachProposalID = attachProposalID
		page.Form.TalkType = talkType
		page.Form.Note = note
		if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/invite_speaker.tmpl", page); err != nil {
			ctx.Err.Printf("/%s/admin/invite-speaker re-render: %s", conf.Tag, err)
		}
	}

	if email == "" {
		formErr("Email is required.")
		return
	}
	address, err := mail.ParseAddress(email)
	if err != nil || !strings.EqualFold(address.Address, email) {
		formErr("Enter a valid email address.")
		return
	}
	if attachProposalID != "" {
		found := false
		for _, p := range attachableProposals(ctx, conf) {
			if p.ID == attachProposalID {
				found = true
				break
			}
		}
		if !found {
			formErr("Choose an available talk from this event.")
			return
		}
	}

	// 1. Resolve speaker by email, checking that any autocomplete selection
	// refers to the same person; otherwise create a new row.
	speaker, err := resolveOrCreateSpeaker(ctx, speakerID, name, email)
	if err != nil {
		ctx.Err.Printf("/%s/admin/invite-speaker resolve speaker: %s", conf.Tag, err)
		formErr("Couldn't look up or create the speaker — see logs.")
		return
	}

	// 2. Resolve proposal: attach to existing or create a fresh one.
	proposal, attachedToExisting, reusedInvitation, err := resolveOrCreateInvitedProposal(ctx, conf, speaker, attachProposalID, talkType)
	if err != nil {
		ctx.Err.Printf("/%s/admin/invite-speaker resolve proposal: %s", conf.Tag, err)
		formErr("Couldn't create or attach the proposal — see logs.")
		return
	}

	// 3. Upsert SpeakerConf linking speaker → conf → proposal.
	scID, err := getters.UpsertSpeakerConf(ctx, getters.SpeakerConfInput{
		SpeakerID:    speaker.ID,
		ConfTag:      conf.Tag,
		ProposalID:   proposal.ID,
		Availability: fullSpeakerAvailability(conf),
	})
	if err != nil {
		ctx.Err.Printf("/%s/admin/invite-speaker upsert speakerconf: %s", conf.Tag, err)
		formErr("Couldn't link speaker to conf — see logs.")
		return
	}
	// Mirror the relation on the Proposal side so admin queues see the
	// new co-speaker without waiting for a cache cycle.
	if err := getters.AddSpeakerConfToProposal(ctx, proposal.ID, scID); err != nil {
		ctx.Err.Printf("/%s/admin/invite-speaker add speakerconf to proposal: %s", conf.Tag, err)
		// non-fatal — Notion's two-way relation usually backfills
	}

	// 4. Stamp InvitedAt.
	if err := getters.SetSpeakerConfInvitedAt(ctx, scID, time.Now()); err != nil {
		ctx.Err.Printf("/%s/admin/invite-speaker stamp InvitedAt: %s", conf.Tag, err)
	}

	// 5. Mint an InviteToken if the proposal doesn't have one yet.
	if proposal.InviteToken == "" {
		proposal.InviteToken = helpers.MintInviteToken()
		if err := getters.SetProposalInviteToken(ctx, proposal.ID, proposal.InviteToken); err != nil {
			ctx.Err.Printf("/%s/admin/invite-speaker set token: %s", conf.Tag, err)
			formErr("Couldn't mint magic link — see logs.")
			return
		}
	}

	// 6. Send the invite letter to *just* the newly-invited speaker.
	// SendOnlyForProposal fans out across every SpeakerConf on the
	// proposal — that's the right behavior for status emails like
	// talkconfirmed (every co-speaker should hear the news), but
	// wrong for an admin invite: when attaching to an existing panel,
	// the existing co-speakers shouldn't receive a fresh invite. We
	// pass a shallow proposal copy whose SpeakerConfRefs holds only
	// the new SpeakerConf so the fanout collapses to one recipient.
	soloProposal := *proposal
	soloProposal.SpeakerConfRefs = []string{scID}
	if err := emails.SendOnlyForProposal(ctx, "talkinvited-direct", &soloProposal, conf, note); err != nil {
		ctx.Err.Printf("/%s/admin/invite-speaker send letter: %s", conf.Tag, err)
		// Non-fatal — the admin still gets the magic link to send manually.
	}

	// 7. Redirect (POST/redirect/GET) to the sent page so a refresh
	// doesn't re-fire the invite.
	http.Redirect(w, r,
		fmt.Sprintf("/%s/admin/invite-speaker/sent?proposal=%s&existing=%t&reused=%t&recipient=%s",
			conf.Tag, proposal.ID, attachedToExisting, reusedInvitation, speaker.ID),
		http.StatusSeeOther)
}

func fullSpeakerAvailability(conf *types.Conf) []string {
	if conf == nil {
		return nil
	}
	days := conf.DaysList("", false)
	out := make([]string, 0, len(days))
	for _, day := range days {
		if strings.TrimSpace(day.ItemID) != "" {
			out = append(out, day.ItemID)
		}
	}
	return out
}

// AdminInviteSpeakerSent renders the post-invite confirmation page
// with the magic link visible for the admin to copy.
func AdminInviteSpeakerSent(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	proposalID := r.URL.Query().Get("proposal")
	proposal, err := getters.GetProposal(ctx, proposalID)
	if err != nil || proposal == nil {
		handle404(w, r, ctx)
		return
	}
	// Keep the confirmation link bound to the recipient from this send, even
	// if another organizer invites a panelist before this page is refreshed.
	var recipient *types.Speaker
	for _, ref := range proposal.SpeakerConfRefs {
		sc, err := getters.GetSpeakerConfByID(ctx, ref)
		if err != nil {
			http.Error(w, "Unable to load invitation", http.StatusInternalServerError)
			return
		}
		if sc != nil && sc.Speaker != nil && sc.Speaker.ID == r.URL.Query().Get("recipient") {
			recipient = sc.Speaker
			break
		}
	}
	if recipient == nil {
		http.Error(w, "Select the intended speaker and send a fresh invitation.", http.StatusBadRequest)
		return
	}

	page := &AdminInviteSpeakerSentPage{
		Conf:               conf,
		Speaker:            recipient,
		Proposal:           proposal,
		MagicLink:          helpers.SpeakerInviteLink(ctx, proposal.ID, proposal.InviteToken, recipient.ID),
		AttachedToExisting: r.URL.Query().Get("existing") == "true",
		ReusedInvitation:   r.URL.Query().Get("reused") == "true",
		Year:               helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/invite_speaker_sent.tmpl", page); err != nil {
		ctx.Err.Printf("/%s/admin/invite-speaker/sent render: %s", conf.Tag, err)
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

// resolveOrCreateSpeaker resolves by email and validates any autocomplete ID.
// A stale or tampered selection cannot redirect an invitation to another person.
func resolveOrCreateSpeaker(ctx *config.AppContext, speakerID, name, email string) (*types.Speaker, error) {
	person, err := getters.GetPersonByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("lookup by email: %w", err)
	}
	if speakerID != "" && (person == nil || person.ID != speakerID) {
		return nil, fmt.Errorf("selected speaker does not own the supplied email; clear the selection and try again")
	}
	if person != nil {
		return person, nil
	}
	if name == "" {
		name = types.InvitedSpeakerName
	}
	id, err := getters.CreateSpeaker(ctx, getters.SpeakerInput{
		Name:  name,
		Email: email,
	})
	if err != nil {
		return nil, fmt.Errorf("create speaker: %w", err)
	}
	return &types.Speaker{ID: id, Name: name, Email: email}, nil
}

// resolveOrCreateInvitedProposal returns the proposal the new speaker
// should be attached to: either the one identified by attachProposalID
// (panel co-speaker case) or a fresh Invited-status proposal seeded
// with placeholder title/description. Returns the proposal and a flag
// indicating whether an existing one was reused.
func resolveOrCreateInvitedProposal(ctx *config.AppContext, conf *types.Conf, speaker *types.Speaker, attachProposalID, talkType string) (*types.Proposal, bool, bool, error) {
	if attachProposalID != "" {
		p, err := getters.GetProposal(ctx, attachProposalID)
		if err != nil || p == nil {
			return nil, false, false, fmt.Errorf("attach proposal lookup: %w", err)
		}
		if p.ScheduleFor == nil || p.ScheduleFor.Ref != conf.Ref {
			return nil, false, false, fmt.Errorf("attach proposal %s does not belong to conf %s", attachProposalID, conf.Tag)
		}
		return p, true, false, nil
	}
	title := types.PlaceholderTitlePrefix + speaker.Name + ")"
	p, created, err := getters.GetOrCreateDirectSpeakerInvitation(ctx, speaker.ID, conf.Tag, title, talkType)
	if err != nil {
		return nil, false, false, fmt.Errorf("create proposal: %w", err)
	}
	return p, false, !created, nil
}

// attachableProposals lists the proposals the organizer can attach a
// new co-speaker to: anything for this conf that isn't terminally
// declined or rejected. Fronted to the form as a select.
func attachableProposals(ctx *config.AppContext, conf *types.Conf) []*types.Proposal {
	var out []*types.Proposal
	for _, p := range loadConfProposals(ctx, conf) {
		if p == nil {
			continue
		}
		switch p.Status {
		case "WeDecline", "TheyDecline", "Rejected":
			continue
		}
		out = append(out, p)
	}
	return out
}
