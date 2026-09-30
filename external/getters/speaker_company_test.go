package getters

import "testing"

func TestSpeakerCompanyProfileEdit(t *testing.T) {
	ctx := postgresSmokeContext(t)
	person := insertSmokePerson(t, ctx, "company-edit")
	for _, tc := range []struct {
		update SpeakerUpdate
		want   string
	}{
		{SpeakerUpdate{Company: "Chaincode"}, "Chaincode"},
		{SpeakerUpdate{Company: " alloc init ", CompanySet: true}, "alloc init"},
		{SpeakerUpdate{Bio: "new bio", BioSet: true}, "alloc init"},
		{SpeakerUpdate{Company: " ", CompanySet: true}, ""},
	} {
		if err := UpdateSpeaker(ctx, person, tc.update); err != nil {
			t.Fatal(err)
		}
		got, err := FetchSpeakerByID(ctx, person)
		if err != nil {
			t.Fatal(err)
		}
		if got.Company != tc.want {
			t.Fatalf("company = %q; want %q", got.Company, tc.want)
		}
	}
}

func TestSpeakerProfileHiringAndIdentityEdits(t *testing.T) {
	ctx := postgresSmokeContext(t)
	person := insertSmokePerson(t, ctx, "profile-fields")
	for _, up := range []SpeakerUpdate{
		{Name: "New Name", OrgLogo: "logo.png", AvailToHire: true, LookingToHire: true, HiringFieldsSet: true},
		{Bio: "new bio", BioSet: true},
		{HiringFieldsSet: true},
	} {
		if err := UpdateSpeaker(ctx, person, up); err != nil {
			t.Fatal(err)
		}
		got, err := FetchSpeakerByID(ctx, person)
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "New Name" || got.OrgLogo != "logo.png" {
			t.Fatal("name/logo not preserved")
		}
		expected := !up.HiringFieldsSet || up.AvailToHire
		if got.AvailToHire != expected || got.LookingToHire != expected {
			t.Fatal("hiring flags not updated/preserved")
		}
	}
}
