package handlers

import (
	"bytes"
	"context"
	"net/http/httptest"

	"os"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEmailOnlySpeakerInvitationsResolveDistinctRecipients(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires local postgres")
	}
	dbctx := context.Background()
	pool, err := pgxpool.New(dbctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	app := &config.AppContext{DB: pool, Env: &types.EnvConfig{}}
	var confID, tag string
	err = pool.QueryRow(dbctx, `INSERT INTO conferences(tag,description,publication_status,start_date,end_date) VALUES('invite-'||gen_random_uuid()::text,'Invitation test','published',now()+interval '30 days',now()+interval '32 days') RETURNING id::text,tag`).Scan(&confID, &tag)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(dbctx, `DELETE FROM conferences WHERE id=$1`, confID) })
	conf, err := getters.GetConfByTag(app, tag)
	if err != nil {
		t.Fatal(err)
	}
	first, err := resolveOrCreateSpeaker(app, "", "", tag+"-a@example.test")
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolveOrCreateSpeaker(app, "", "", tag+"-b@example.test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(dbctx, `DELETE FROM people WHERE id::text=ANY($1::text[])`, []string{first.ID, second.ID})
	})
	if !first.InvitationNamePending() || !second.InvitationNamePending() {
		t.Fatal("email-only invitations should request names from recipients")
	}
	reused, err := resolveOrCreateSpeaker(app, "", "", first.Email)
	if err != nil || reused.ID != first.ID {
		t.Fatal("email lookup created duplicate person")
	}
	proposal, _, _, err := resolveOrCreateInvitedProposal(app, conf, first, "", "talk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(dbctx, `DELETE FROM proposals WHERE id=$1`, proposal.ID) })
	var refs []string
	for _, person := range []*types.Speaker{first, second} {
		sc, err := getters.UpsertSpeakerConf(app, getters.SpeakerConfInput{SpeakerID: person.ID, ConfTag: tag, ProposalID: proposal.ID})
		if err != nil {
			t.Fatal(err)
		}
		if err := getters.AddSpeakerConfToProposal(app, proposal.ID, sc); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, sc)
	}
	proposal.SpeakerConfRefs = refs
	proposal.InviteToken = "test-token"
	// Opening the second invitation first must still select the second person.
	for _, i := range []int{1, 0, 1} {
		req := httptest.NewRequest("GET", helpers.SpeakerInviteLink(app, proposal.ID, proposal.InviteToken, []*types.Speaker{first, second}[i].ID), nil)
		sc, err := resolveSpeakerInviteRecipient(app, req, proposal)
		if err != nil {
			t.Fatal(err)
		}
		if sc.ID != refs[i] {
			t.Fatal("wrong recipient selected")
		}
	}
	if _, err := resolveOrCreateSpeaker(app, first.ID, "", second.Email); err == nil {
		t.Fatal("mismatched selected identity accepted")
	}
}

func TestEmailOnlyInviteTemplatesAndProfileName(t *testing.T) {
	person := &types.Speaker{Name: types.InvitedSpeakerName, Email: "recipient@example.test"}
	up := buildSpeakerUpdateFromForm(person, &types.TalkApp{Name: "Actual Name"})
	if up.Name != "Actual Name" {
		t.Fatal("submitted name must replace invitation placeholder")
	}
	if buildSpeakerUpdateFromForm(&types.Speaker{Name: "Existing Name"}, &types.TalkApp{Name: "Changed"}).Name != "" {
		t.Fatal("invitation must preserve an existing profile name")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	app := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	conf := &types.Conf{Tag: "seoul", Desc: "Seoul", StartDate: time.Now().AddDate(0, 0, 30)}
	proposal := &types.Proposal{ID: "talk-id", Title: "Existing talk", Status: "Accepted"}
	page := &AdminInviteSpeakerPage{Conf: conf, AttachableProposals: []*types.Proposal{proposal}}
	page.Form.AttachProposalID = proposal.ID
	var out bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&out, "admin/invite_speaker.tmpl", page); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `value="talk-id" selected`) || strings.Contains(out.String(), `name="Name" required`) {
		t.Fatal("email-only form must preselect the talk and allow an omitted name")
	}
	out.Reset()
	if err := app.TemplateCache.ExecuteTemplate(&out, "embeds/talk.tmpl", &SpeakerPage{Conf: conf, Proposal: proposal, InviteMode: true, InviteToken: "token", InviteRecipient: "recipient-person", InviteSignature: "signature", KnownSpeaker: person}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="Name"`, `recipient=recipient-person`, `signature=signature`, `name="Email" value="recipient@example.test"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("invite form missing %q", want)
		}
	}
	if strings.Contains(out.String(), `name="Name" value="Invited speaker"`) {
		t.Fatal("placeholder must not hide name entry")
	}
}
