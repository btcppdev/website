package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

const whoIsFeaturedBadgeLimit = 6

func loadWhoIsBadgeCollection(r *http.Request, ctx *config.AppContext, person *WhoIsPerson, label string, publicOnly bool) *WhoIsBadgeCollection {
	if person == nil || person.Speaker == nil {
		return &WhoIsBadgeCollection{}
	}
	var profile *WhoIsBadgeProfile
	if ctx.Env != nil && ctx.Env.BadgeStudioURL != "" {
		loaded, err := loadBadgeStudioProfile(r.Context(), ctx.Env.BadgeStudioURL, person.Speaker.ID)
		if err != nil {
			if ctx.Err != nil {
				ctx.Err.Printf("%s Badge Studio profile: %s", label, err)
			}
		} else {
			profile = loaded
		}
	}
	badgeGrants, err := getters.ListPersonBadgeGrants(ctx, person.Speaker.ID)
	if err != nil {
		if ctx.Err != nil {
			ctx.Err.Printf("%s Bitcoin++ badge grants: %s", label, err)
		}
		badgeGrants = nil
	}
	if publicOnly {
		profile, badgeGrants = organizationOnlyProfileBadges(profile, badgeGrants)
	}
	attachWhoIsBadgeIssuers(profile, badgeGrants)
	visibleGrants := whoIsBadgeGrants(badgeGrants, profile)
	profile = publicWhoIsBadgeProfile(profile)
	presentations, err := getters.ListPersonBadgePresentations(ctx, person.Speaker.ID)
	if err != nil {
		if ctx.Err != nil {
			ctx.Err.Printf("%s badge presentation preferences: %s", label, err)
		}
		presentations = nil
	}
	return buildWhoIsBadgeCollection(profile, visibleGrants, presentations)
}

// Public profiles require a local organization grant, not an issuer label or
// an arbitrary Studio award linked only to the recipient's bitcoin++ identity.
func organizationOnlyProfileBadges(profile *WhoIsBadgeProfile, grants []*types.OrganizationBadgeGrant) (*WhoIsBadgeProfile, []*types.OrganizationBadgeGrant) {
	eligible := make([]*types.OrganizationBadgeGrant, 0, len(grants))
	byAward := make(map[string]*types.OrganizationBadgeGrant)
	for _, grant := range grants {
		if grant == nil || grant.OrganizationID == "" || grant.State == getters.BadgeGrantStateCanceled || grant.State == getters.BadgeGrantStateCorrected || grant.State == getters.BadgeGrantStateRevoked {
			continue
		}
		eligible = append(eligible, grant)
		if grant.AwardEventID != "" {
			byAward[grant.AwardEventID] = grant
		}
	}
	filtered := &WhoIsBadgeProfile{}
	if profile != nil {
		for _, badge := range profile.Issued {
			grant := byAward[badge.Award.EventID]
			if grant != nil && grant.IssuerPubkey != "" && grant.IssuerPubkey == badge.Definition.IssuerPubkey {
				filtered.Issued = append(filtered.Issued, badge)
			}
		}
	}
	// Pending badges come from the authoritative local grants, never Studio metadata.
	return filtered, eligible
}

func buildWhoIsBadgeCollection(profile *WhoIsBadgeProfile, grants []*WhoIsBadgeGrant, presentations []*types.PersonBadgePresentation) *WhoIsBadgeCollection {
	collection := &WhoIsBadgeCollection{}
	seen := make(map[string]struct{})
	appendBadge := func(badge *WhoIsProfileBadge) {
		if badge == nil || badge.Reference == "" {
			return
		}
		if _, exists := seen[badge.Reference]; exists {
			return
		}
		seen[badge.Reference] = struct{}{}
		badge.Issuer = publicWhoIsBadgeIssuer(badge.Issuer)
		collection.All = append(collection.All, badge)
	}
	if profile != nil {
		for index := range profile.Issued {
			item := &profile.Issued[index]
			reference := "nostr-award:" + strings.TrimSpace(item.Award.EventID)
			if item.GrantID != "" {
				reference = "btcpp-grant:" + item.GrantID
			} else if item.Award.EventID == "" {
				reference = "nostr-badge:" + item.Definition.IssuerPubkey + ":" + item.Definition.Identifier
			}
			issuer := item.Issuer
			if issuer.Pubkey == "" {
				issuer.Pubkey = item.Definition.IssuerPubkey
			}
			appendBadge(&WhoIsProfileBadge{Reference: reference, Name: item.Definition.Name, Description: item.Definition.Description, ImageURL: item.Definition.ImageURL, Issuer: issuer})
		}
		for index := range profile.Pending {
			item := &profile.Pending[index]
			if item.Badge == nil {
				continue
			}
			reference := "studio-grant:" + strings.TrimSpace(item.ID)
			if item.ID == "" {
				reference = "studio-pending:" + item.IssuerPubkey + ":" + item.Badge.Identifier
			}
			issuer := item.Issuer
			if issuer.Pubkey == "" {
				issuer.Pubkey = item.IssuerPubkey
			}
			appendBadge(&WhoIsProfileBadge{Reference: reference, Name: item.Badge.Name, Description: item.Badge.Description, ImageURL: item.Badge.ImageURL, Issuer: issuer})
		}
	}
	for _, item := range grants {
		if item == nil || item.OrganizationBadgeGrant == nil {
			continue
		}
		appendBadge(&WhoIsProfileBadge{
			Reference:   "btcpp-grant:" + item.ID,
			Name:        item.BadgeName,
			Description: item.BadgeDescription,
			ImageURL:    item.BadgeImageURL,
			Issuer:      item.Issuer,
		})
	}

	preferences := make(map[string]*types.PersonBadgePresentation, len(presentations))
	for _, item := range presentations {
		if item != nil {
			preferences[item.BadgeRef] = item
		}
	}
	sourceOrder := make(map[*WhoIsProfileBadge]int, len(collection.All))
	for index, badge := range collection.All {
		sourceOrder[badge] = index
		if preference := preferences[badge.Reference]; preference != nil {
			badge.Configured = true
			badge.Hidden = preference.Hidden
			badge.FeaturedPosition = preference.FeaturedPosition
		}
	}

	explicitFeatured := make([]*WhoIsProfileBadge, 0, whoIsFeaturedBadgeLimit)
	for _, badge := range collection.All {
		if !badge.Hidden && badge.FeaturedPosition > 0 {
			explicitFeatured = append(explicitFeatured, badge)
		}
	}
	sort.SliceStable(explicitFeatured, func(i, j int) bool {
		if explicitFeatured[i].FeaturedPosition == explicitFeatured[j].FeaturedPosition {
			return sourceOrder[explicitFeatured[i]] < sourceOrder[explicitFeatured[j]]
		}
		return explicitFeatured[i].FeaturedPosition < explicitFeatured[j].FeaturedPosition
	})
	if len(explicitFeatured) > whoIsFeaturedBadgeLimit {
		explicitFeatured = explicitFeatured[:whoIsFeaturedBadgeLimit]
	}
	collection.Featured = append(collection.Featured, explicitFeatured...)
	featuredRefs := make(map[string]struct{}, whoIsFeaturedBadgeLimit)
	for _, badge := range collection.Featured {
		featuredRefs[badge.Reference] = struct{}{}
	}
	for _, badge := range collection.All {
		if len(collection.Featured) >= whoIsFeaturedBadgeLimit {
			break
		}
		if badge.Hidden || badge.Configured {
			continue
		}
		if _, exists := featuredRefs[badge.Reference]; exists {
			continue
		}
		collection.Featured = append(collection.Featured, badge)
		featuredRefs[badge.Reference] = struct{}{}
	}
	for index, badge := range collection.Featured {
		badge.Featured = true
		badge.FeaturedPosition = index + 1
	}
	for _, badge := range collection.All {
		if badge.Hidden {
			collection.Hidden = append(collection.Hidden, badge)
			continue
		}
		collection.TotalVisible++
		if !badge.Featured {
			collection.OtherVisible = append(collection.OtherVisible, badge)
		}
	}
	collection.HasMore = collection.TotalVisible > len(collection.Featured)
	collection.Groups = badgeIssuerGroups(collection.Featured, collection.OtherVisible)
	return collection
}

func publicWhoIsBadgeIssuer(issuer WhoIsBadgeIssuer) WhoIsBadgeIssuer {
	issuer.Name = strings.TrimSpace(issuer.Name)
	issuer.Pubkey = strings.TrimSpace(issuer.Pubkey)
	if issuer.Name == "" {
		if issuer.Pubkey != "" {
			short := issuer.Pubkey
			if len(short) > 8 {
				short = short[:8] + "…"
			}
			issuer.Name = "Nostr issuer · " + short
		} else {
			issuer.Name = "Independent issuer"
		}
	}
	return issuer
}

func badgeIssuerGroups(featured, others []*WhoIsProfileBadge) []*WhoIsBadgeIssuerGroup {
	ordered := make([]*WhoIsProfileBadge, 0, len(featured)+len(others))
	ordered = append(ordered, featured...)
	ordered = append(ordered, others...)
	groupsByKey := make(map[string]*WhoIsBadgeIssuerGroup)
	var groups []*WhoIsBadgeIssuerGroup
	for _, badge := range ordered {
		key := badge.Issuer.ProfileURL
		if key == "" {
			key = badge.Issuer.Pubkey
		}
		if key == "" {
			key = badge.Issuer.Name
		}
		group := groupsByKey[key]
		if group == nil {
			group = &WhoIsBadgeIssuerGroup{Issuer: badge.Issuer}
			groupsByKey[key] = group
			groups = append(groups, group)
		}
		group.Badges = append(group.Badges, badge)
	}
	return groups
}

func RenderWhoIsBadges(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	slug := strings.TrimSpace(mux.Vars(r)["speaker"])
	if slug == "" {
		handle404(w, r, ctx)
		return
	}
	person, err := findWhoIsPerson(ctx, slug)
	if err != nil {
		http.Error(w, "Unable to load badge profile, please try again later", http.StatusInternalServerError)
		return
	}
	if person == nil {
		handle404(w, r, ctx)
		return
	}
	manageURL := ""
	if whoIsProfileEditURL(ctx, r, person) != "" {
		manageURL = "/dashboard/profile/badges"
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "whois_badges.tmpl", &WhoIsBadgesPage{
		Person: person, Badges: loadWhoIsBadgeCollection(r, ctx, person, "/whois/"+slug+"/badges", true),
		ManageBadgesURL: manageURL, Year: helpers.CurrentYear(),
		SocialCardURL: siteSocialCardPath("person", person.PublicID, personSocialCard(ctx, person)),
	}); err != nil {
		http.Error(w, "Unable to load badge profile, please try again later", http.StatusInternalServerError)
		if ctx.Err != nil {
			ctx.Err.Printf("/whois/%s/badges template: %s", slug, err)
		}
	}
}

func DashboardProfileBadges(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	identity := requirePersonIdentity(w, r, ctx)
	if identity == nil {
		return
	}
	person := &WhoIsPerson{Speaker: identity.Speaker}
	if slug, ok := resolvedWhoIsPublicID(ctx, identity.Speaker); ok {
		person.PublicID = slug
	}
	collection := loadWhoIsBadgeCollection(r, ctx, person, "/dashboard/profile/badges", false)
	if r.Method == http.MethodPost {
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
			redirectProfileBadges(w, r, "", "That badge display request expired. Reload and try again.")
			return
		}
		message, err := updateProfileBadgePresentation(ctx, identity.PersonID, collection, r.FormValue("badge_ref"), r.FormValue("action"))
		if err != nil {
			redirectProfileBadges(w, r, "", err.Error())
			return
		}
		redirectProfileBadges(w, r, message, "")
		return
	}
	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "Unable to prepare badge controls", http.StatusInternalServerError)
		return
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_profile_badges.tmpl", &ProfileBadgesPage{
		Person: person, Badges: collection, CSRF: csrf,
		FlashMessage: r.URL.Query().Get("flash"), FlashError: r.URL.Query().Get("error"), Year: helpers.CurrentYear(),
	}); err != nil {
		http.Error(w, "Unable to load badge controls", http.StatusInternalServerError)
		if ctx.Err != nil {
			ctx.Err.Printf("/dashboard/profile/badges template: %s", err)
		}
	}
}

func updateProfileBadgePresentation(ctx *config.AppContext, personID string, collection *WhoIsBadgeCollection, reference, action string) (string, error) {
	reference = strings.TrimSpace(reference)
	action = strings.TrimSpace(action)
	var target *WhoIsProfileBadge
	for _, badge := range collection.All {
		if badge.Reference == reference {
			target = badge
			break
		}
	}
	if target == nil {
		return "", fmt.Errorf("That badge is no longer available on your profile.")
	}
	items := make(map[string]*types.PersonBadgePresentation, len(collection.All))
	for _, badge := range collection.All {
		items[badge.Reference] = &types.PersonBadgePresentation{BadgeRef: badge.Reference, Hidden: badge.Hidden, FeaturedPosition: badge.FeaturedPosition}
	}
	item := items[target.Reference]
	message := "Badge display updated."
	switch action {
	case "hide":
		item.Hidden = true
		item.FeaturedPosition = 0
		message = "Badge hidden from your public profile."
	case "show":
		item.Hidden = false
		item.FeaturedPosition = 0
		message = "Badge restored to your public profile."
	case "feature":
		if item.Hidden {
			item.Hidden = false
		}
		if item.FeaturedPosition == 0 {
			if len(collection.Featured) >= whoIsFeaturedBadgeLimit {
				return "", fmt.Errorf("Your featured showcase already has six badges. Unfeature one first.")
			}
			item.FeaturedPosition = len(collection.Featured) + 1
		}
		message = "Badge added to your featured showcase."
	case "unfeature":
		item.FeaturedPosition = 0
		message = "Badge removed from your featured showcase."
	case "move_up", "move_down":
		if item.FeaturedPosition == 0 {
			return "", fmt.Errorf("Only featured badges can be reordered.")
		}
		direction := -1
		if action == "move_down" {
			direction = 1
		}
		otherPosition := item.FeaturedPosition + direction
		if otherPosition < 1 || otherPosition > len(collection.Featured) {
			return "", fmt.Errorf("That badge is already at the edge of your showcase.")
		}
		for _, candidate := range items {
			if candidate.FeaturedPosition == otherPosition {
				candidate.FeaturedPosition = item.FeaturedPosition
				break
			}
		}
		item.FeaturedPosition = otherPosition
		message = "Featured badge order updated."
	default:
		return "", fmt.Errorf("Choose a valid badge display action.")
	}

	ordered := make([]*types.PersonBadgePresentation, 0, len(items))
	for _, presentation := range items {
		ordered = append(ordered, presentation)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].BadgeRef < ordered[j].BadgeRef })
	featured := make([]*types.PersonBadgePresentation, 0, whoIsFeaturedBadgeLimit)
	for _, presentation := range ordered {
		if presentation.FeaturedPosition > 0 && !presentation.Hidden {
			featured = append(featured, presentation)
		}
	}
	sort.Slice(featured, func(i, j int) bool { return featured[i].FeaturedPosition < featured[j].FeaturedPosition })
	for index, presentation := range featured {
		presentation.FeaturedPosition = index + 1
	}
	if err := getters.ReplacePersonBadgePresentations(ctx, personID, ordered); err != nil {
		return "", fmt.Errorf("Unable to save your badge display. Please try again.")
	}
	return message, nil
}

func redirectProfileBadges(w http.ResponseWriter, r *http.Request, flash, errorMessage string) {
	query := url.Values{}
	if flash != "" {
		query.Set("flash", flash)
	}
	if errorMessage != "" {
		query.Set("error", errorMessage)
	}
	destination := "/dashboard/profile/badges"
	if encoded := query.Encode(); encoded != "" {
		destination += "?" + encoded
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}
