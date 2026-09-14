package getters

import (
	"btcpp-web/internal/pgpkeys"
	"bytes"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	openpgp "github.com/ProtonMail/go-crypto/openpgp/v2"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPersonPGPKeyLifecycle(t *testing.T) {
	ctx := postgresSmokeContext(t)
	person := insertSmokePerson(t, ctx, "pgp-owner")
	other := insertSmokePerson(t, ctx, "pgp-other")
	e, err := openpgp.NewEntity("PGP test", "", "test@example.test", &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA})
	if err != nil {
		t.Fatal(err)
	}
	var binary bytes.Buffer
	if err := e.Serialize(&binary); err != nil {
		t.Fatal(err)
	}
	armored, err := pgpkeys.Armor(binary.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	key, err := pgpkeys.Parse(armored, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := AddPersonPGPKey(ctx, person, armored); err != nil {
		t.Fatal(err)
	}
	if err := AddPersonPGPKey(ctx, person, armored); err != nil {
		t.Fatal(err)
	}
	list, err := ListPersonPGPKeys(ctx, person, false)
	if err != nil || len(list) != 1 {
		t.Fatalf("duplicate keys: %v %v", list, err)
	}
	public, err := ListPersonPGPKeys(ctx, person, true)
	if err != nil || len(public) != 0 {
		t.Fatal("unverified key exposed", err)
	}
	if err := StartPersonPGPChallenge(ctx, other, key.Fingerprint); err == nil {
		t.Fatal("other account created challenge")
	}
	if err := StartPersonPGPChallenge(ctx, person, key.Fingerprint); err != nil {
		t.Fatal(err)
	}
	list, _ = ListPersonPGPKeys(ctx, person, false)
	challenge := list[0].Challenge
	sign := func(message string) string {
		t.Helper()
		var sig bytes.Buffer
		if err := openpgp.ArmoredDetachSign(&sig, []*openpgp.Entity{e}, strings.NewReader(message), nil); err != nil {
			t.Fatal(err)
		}
		return sig.String()
	}
	signature := sign(challenge)
	if err := VerifyPersonPGPKey(ctx, other, key.Fingerprint, signature); err == nil {
		t.Fatal("other account verified key")
	}
	if err := VerifyPersonPGPKey(ctx, person, key.Fingerprint, sign(challenge+"modified")); err == nil {
		t.Fatal("accepted invalid proof")
	}
	if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE person_pgp_keys SET challenge_expires_at=now()-interval '1 second' WHERE person_id=$1`, person); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPersonPGPKey(ctx, person, key.Fingerprint, signature); err == nil {
		t.Fatal("accepted expired challenge")
	}
	if err := StartPersonPGPChallenge(ctx, person, key.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPersonPGPKey(ctx, person, key.Fingerprint, signature); err == nil {
		t.Fatal("accepted replaced challenge")
	}
	list, _ = ListPersonPGPKeys(ctx, person, false)
	signature = sign(list[0].Challenge)
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); results <- VerifyPersonPGPKey(ctx, person, key.Fingerprint, signature) }()
	}
	workers.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent proof submissions succeeded %d times, want once", successes)
	}
	if err := VerifyPersonPGPKey(ctx, person, key.Fingerprint, signature); err == nil {
		t.Fatal("accepted consumed challenge")
	}
	public, err = ListPersonPGPKeys(ctx, person, true)
	if err != nil || len(public) != 1 || public[0].Challenge != "" || public[0].VerifiedAt == nil {
		t.Fatal("verified key missing or challenge retained", err)
	}
	if err := RemovePersonPGPKey(ctx, other, key.Fingerprint); err == nil {
		t.Fatal("other account removed key")
	}
	if err := RemovePersonPGPKey(ctx, person, key.Fingerprint); err != nil {
		t.Fatal(err)
	}
	public, err = ListPersonPGPKeys(ctx, person, true)
	if err != nil || len(public) != 0 {
		t.Fatal("deleted key still public", err)
	}
}

func TestPersonPGPKeysMergeAndUndo(t *testing.T) {
	f := newMergeAccountsFixture(t)
	ctx := f.app
	fp1, fp2 := strings.Repeat("A", 40), strings.Repeat("B", 40)
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `INSERT INTO person_pgp_keys(person_id,fingerprint,key_id,public_key) VALUES ($1,$3,'AAAAAAAAAAAAAAAA','source-duplicate'),($2,$3,'AAAAAAAAAAAAAAAA','canonical'),($1,$4,'BBBBBBBBBBBBBBBB','source-only')`, f.source, f.canonical, fp1, fp2)
	if err != nil {
		t.Fatal(err)
	}
	event, err := MergePeople(ctx, PersonMergeInput{CanonicalPersonID: f.canonical, SourcePersonID: f.source, MergedByPersonID: f.canonical})
	if err != nil {
		t.Fatal(err)
	}
	keys, err := ListPersonPGPKeys(ctx, f.canonical, false)
	if err != nil || len(keys) != 2 {
		t.Fatal("keys not transferred", err)
	}
	preview, err := GetPersonMergeUndoPreview(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if err := UndoPersonMerge(ctx, event, f.canonical, preview); err != nil {
		t.Fatal(err)
	}
	keys, err = ListPersonPGPKeys(ctx, f.source, false)
	if err != nil || len(keys) != 2 {
		t.Fatal("source keys not restored", err)
	}
	keys, err = ListPersonPGPKeys(ctx, f.canonical, false)
	if err != nil || len(keys) != 1 || keys[0].PublicKey != "canonical" {
		t.Fatal("canonical changed", err)
	}
}
