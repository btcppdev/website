package types

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Private defaults, deliberately separate from the public Speaker profile.
type AttendeePreferences struct {
	TShirt, Diet, DietOther, Allergies string
}

func (p *AttendeePreferences) Normalize() error {
	p.TShirt = strings.TrimSpace(p.TShirt)
	p.Diet = strings.TrimSpace(p.Diet)
	p.DietOther = strings.TrimSpace(p.DietOther)
	p.Allergies = strings.TrimSpace(p.Allergies)
	if p.TShirt != "" && ValidShirtSizeCode(p.TShirt) != p.TShirt {
		return fmt.Errorf("choose a valid shirt size")
	}
	if !ValidDietaryPreference(p.Diet) {
		return fmt.Errorf("choose a valid food preference")
	}
	if p.Diet != "other" {
		p.DietOther = ""
	} else if p.DietOther == "" {
		return fmt.Errorf("describe the other food preference")
	}
	if utf8.RuneCountInString(p.DietOther) > 500 || utf8.RuneCountInString(p.Allergies) > 500 {
		return fmt.Errorf("food preferences must be at most 500 characters")
	}
	return nil
}

type TicketAttendeeDetails struct {
	RefID, CheckoutID, ConferenceID, OwnerEmail, OwnerPersonID string
	AccessKey                                                  string `json:"-"`
	Type, Name, TShirt, Diet, DietOther, Allergies             string
	IsBuyer                                                    bool
	Version                                                    int64
	ProfileName, ProfileShirt                                  string
	ProfileDiet, ProfileDietOther, ProfileAllergies            string
	ProfilePhoto, ProfileHandle                                string
	BookletChoice, BookletName                                 string
	BookletUseAvatar                                           bool
	EffectiveName, EffectiveShirt                              string
}

func (d *TicketAttendeeDetails) IsSupporter() bool { return d.Type == "supporter" }

func (d *TicketAttendeeDetails) BookletAvatar() string {
	if d.IsSupporter() && d.BookletChoice == "named" && d.BookletUseAvatar && d.IsBuyer {
		return d.ProfilePhoto
	}
	return ""
}

func (d *TicketAttendeeDetails) ShirtLabel() string { return ShirtSizeLabel(d.EffectiveShirt) }
func (d *TicketAttendeeDetails) ShirtSizes() []ShirtSizeOption {
	return ShirtSizeOptions(d.TShirt, d.ProfileShirt)
}
func (d *TicketAttendeeDetails) DietLabel() string { return DietaryPreferenceLabel(d.Diet) }

func (d *TicketAttendeeDetails) TicketLabel() string {
	switch d.Type {
	case "genpop", "general":
		return "Standard"
	case "local":
		return "Local"
	case "student":
		return "Student"
	case "supporter":
		return "Supporter"
	case "speaker":
		return "Speaker"
	case "volunteer":
		return "Volunteer"
	case "sponsor":
		return "Sponsor"
	default:
		return d.Type
	}
}

func DietaryPreferenceLabel(value string) string {
	switch value {
	case "none":
		return "No preference"
	case "vegetarian":
		return "Vegetarian"
	case "vegan":
		return "Vegan"
	case "other":
		return "Other"
	default:
		return "Not provided"
	}
}

func ValidDietaryPreference(value string) bool {
	return value == "" || value == "none" || value == "vegetarian" || value == "vegan" || value == "other"
}

func (d *TicketAttendeeDetails) Ready() bool {
	return d.TShirt != "" && d.Diet != "" && (d.Diet != "other" || strings.TrimSpace(d.DietOther) != "")
}
