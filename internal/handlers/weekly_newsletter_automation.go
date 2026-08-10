package handlers

import (
	"net/http"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
)

const weeklyNewsletterDraftHour = 14

// StartWeeklyNewsletterDrafting runs the production-only Monday editorial
// automation. A Monday restart after 2 PM Central catches up that day's run;
// the weekly issue dedupe key keeps repeated or multi-instance attempts safe.
func StartWeeklyNewsletterDrafting(ctx *config.AppContext) {
	if ctx == nil || ctx.Env == nil || !ctx.InProduction || ctx.Env.MailOff {
		if ctx != nil && ctx.Infos != nil {
			ctx.Infos.Println("weekly newsletter drafting disabled outside mail-enabled production")
		}
		return
	}
	go func() {
		for {
			now := time.Now()
			if weeklyNewsletterDraftIsDue(now) {
				runWeeklyNewsletterDraftAutomation(ctx, now)
				now = time.Now()
			}
			next := nextWeeklyNewsletterDraftAt(now)
			ctx.Infos.Printf("next weekly newsletter draft automation at %s", next.Format(time.RFC3339))
			timer := time.NewTimer(time.Until(next))
			<-timer.C
		}
	}()
}

func runWeeklyNewsletterDraftAutomation(ctx *config.AppContext, builtAt time.Time) {
	result, err := createWeeklyNewsletterDraft(ctx, builtAt)
	if err != nil {
		ctx.Err.Printf("weekly newsletter auto-draft failed: %s", err)
		return
	}
	if err := emails.SendWeeklyNewsletterDraftReview(ctx, result.Letter); err != nil {
		ctx.Err.Printf("weekly newsletter draft review email failed for MISS-%d: %s", result.Letter.UID, err)
		return
	}
	if result.Existing {
		ctx.Infos.Printf("weekly newsletter MISS-%d already existed; review notification reconciled", result.Letter.UID)
		return
	}
	ctx.Infos.Printf("weekly newsletter MISS-%d auto-drafted and sent to inbox@btcpp.dev for review", result.Letter.UID)
}

// TemplatedMissivesTestWeeklyAutomation exposes the production workflow to an
// authenticated admin in development. Requiring DEV_EMAIL_OVERRIDE makes it
// impossible for this test action to address the real editorial inbox.
func TemplatedMissivesTestWeeklyAutomation(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	if ctx.InProduction {
		http.NotFound(w, r)
		return
	}
	override := strings.TrimSpace(ctx.Env.DevEmailOverride)
	if override == "" {
		redirectTemplatedMissivesErr(w, r, "Set DEV_EMAIL_OVERRIDE before testing the weekly auto-draft email")
		return
	}
	result, err := createWeeklyNewsletterDraft(ctx, time.Now())
	if err != nil {
		redirectTemplatedMissivesErr(w, r, "Test auto-draft failed: "+err.Error())
		return
	}
	if err := emails.SendWeeklyNewsletterDraftReviewTest(ctx, result.Letter); err != nil {
		redirectTemplatedMissivesErr(w, r, "Test review email failed: "+err.Error())
		return
	}
	flash := "Test auto-draft created and review sent to " + override
	if result.Existing {
		flash = "Existing weekly draft reused and a fresh test review sent to " + override
	}
	http.Redirect(w, r, templatedMissiveEditorURL(result.Letter.UID, "flash", flash), http.StatusSeeOther)
}

func nextWeeklyNewsletterDraftAt(now time.Time) time.Time {
	loc := weeklyNewsletterCentralLocation()
	localNow := now.In(loc)
	days := (int(time.Monday) - int(localNow.Weekday()) + 7) % 7
	candidate := time.Date(localNow.Year(), localNow.Month(), localNow.Day()+days, weeklyNewsletterDraftHour, 0, 0, 0, loc)
	if !candidate.After(localNow) {
		candidate = candidate.AddDate(0, 0, 7)
	}
	return candidate
}

func weeklyNewsletterDraftIsDue(now time.Time) bool {
	localNow := now.In(weeklyNewsletterCentralLocation())
	return localNow.Weekday() == time.Monday && localNow.Hour() >= weeklyNewsletterDraftHour
}

func weeklyNewsletterCentralLocation() *time.Location {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return time.FixedZone("America/Chicago", -6*60*60)
	}
	return loc
}
