package getters

import "testing"

func TestDatabaseSmokeDashboardSpeakerGraph(t *testing.T) {
	ctx := databaseSmokeContext(t)
	var personID string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT speaker_id::text
		FROM speaker_confs
		ORDER BY created_at
		LIMIT 1
	`).Scan(&personID)
	if err != nil {
		t.Skipf("development database has no speaker conference fixture: %s", err)
	}

	speakers, speakerConfs, err := GetSpeakerConfsByPersonID(ctx, personID)
	if err != nil {
		t.Fatalf("GetSpeakerConfsByPersonID: %v", err)
	}
	if len(speakers) != 1 || speakers[0] == nil || speakers[0].ID != personID {
		t.Fatalf("speakers = %#v, want person %s", speakers, personID)
	}
	if len(speakerConfs) == 0 {
		t.Fatal("GetSpeakerConfsByPersonID returned no speaker conferences")
	}
	for _, speakerConf := range speakerConfs {
		if speakerConf == nil || speakerConf.Speaker == nil || speakerConf.Speaker.ID != personID {
			t.Fatalf("speaker conference was not scoped to %s: %#v", personID, speakerConf)
		}
		for _, proposal := range speakerConf.Proposals {
			if proposal == nil {
				t.Fatal("speaker conference contained a nil proposal")
			}
			linked := false
			for _, ref := range proposal.SpeakerConfRefs {
				if ref == speakerConf.ID {
					linked = true
					break
				}
			}
			if !linked {
				t.Fatalf("proposal %s is not linked to speaker conference %s", proposal.ID, speakerConf.ID)
			}
		}
	}
}
