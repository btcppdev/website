package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/api"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/pgpkeys"
	"btcpp-web/internal/types"
	"bytes"
	"context"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	openpgp "github.com/ProtonMail/go-crypto/openpgp/v2"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The optional preview serves only synthetic records in an isolated database.
func TestProfilePGPHTTPFlow(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires disposable migrated database")
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	wd, _ := os.Getwd()
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	app := &config.AppContext{DB: pool, Session: scs.New(), Env: &types.EnvConfig{MailOff: true}, Err: log.New(io.Discard, "", 0), Infos: log.New(io.Discard, "", 0)}
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	c := context.Background()
	id := func(q string, args ...any) string {
		t.Helper()
		var value string
		if err := pool.QueryRow(c, q, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(c, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	person := id(`INSERT INTO people(name,company,bio,github_url) VALUES ('Mara PGP Preview','Bitcoin builders','Building tools for a more private, open Bitcoin.','marapreview') RETURNING id::text`)
	conf := id(`INSERT INTO conferences(tag,description,publication_status,start_date,end_date,timezone) VALUES($1,'Bitcoin++ Preview','published',now(),now()+interval '1 day','UTC') RETURNING id::text`, "pgp-"+uuid.NewString())
	sc := id(`INSERT INTO speaker_confs(speaker_id) VALUES($1) RETURNING id::text`, person)
	proposal := id(`INSERT INTO proposals(conference_id,title,status) VALUES($1,'Keys to a more private web','Accepted') RETURNING id::text`, conf)
	exec(`INSERT INTO speaker_confs_conferences(speaker_conf_id,conference_id) VALUES($1,$2)`, sc, conf)
	exec(`INSERT INTO proposals_speaker_confs(proposal_id,speaker_conf_id) VALUES($1,$2)`, proposal, sc)
	exec(`INSERT INTO conf_talks(conference_id,proposal_id) VALUES($1,$2)`, conf, proposal)
	defer func() {
		exec(`DELETE FROM conferences WHERE id=$1`, conf)
		exec(`DELETE FROM people WHERE id=$1`, person)
	}()
	people, err := buildWhoIsDirectory(app)
	if err != nil {
		t.Fatal(err)
	}
	slug := ""
	for _, p := range people {
		if p.Speaker.ID == person {
			slug = p.PublicID
		}
	}
	if slug == "" {
		t.Fatal("fixture not publicly visible")
	}
	root := mux.NewRouter()
	root.HandleFunc("/preview", func(w http.ResponseWriter, r *http.Request) {
		if err := auth.LoginPerson(app, r, person, auth.MethodEmailLink); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		http.Redirect(w, r, "/dashboard/profile/keys", 303)
	})
	root.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	root.HandleFunc("/dashboard/profile/keys", func(w http.ResponseWriter, r *http.Request) { DashboardProfilePGPKeys(w, r, app) }).Methods("GET", "POST")
	root.HandleFunc("/whois/{speaker}/key.{format:asc|gpg}", func(w http.ResponseWriter, r *http.Request) { RenderWhoIsPGPKeys(w, r, app) }).Methods("GET")
	root.HandleFunc("/whois/{speaker}/keys/{fingerprint:[0-9A-Fa-f]+}.{format:asc|gpg}", func(w http.ResponseWriter, r *http.Request) { RenderWhoIsPGPKeys(w, r, app) }).Methods("GET")
	root.HandleFunc("/whois/{speaker}", func(w http.ResponseWriter, r *http.Request) { RenderWhoIsProfile(w, r, app) }).Methods("GET")
	api.Register(root, app)
	handler := app.Session.LoadAndSave(root)
	server := httptest.NewServer(handler)
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(path string) (int, string) {
		t.Helper()
		resp, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	if code, _ := get("/dashboard/profile/keys"); code != 303 {
		t.Fatal("anonymous controls not blocked", code)
	}
	get("/preview")
	code, body := get("/dashboard/profile/keys")
	if code != 200 {
		t.Fatal(code, body)
	}
	matches := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(body)
	if len(matches) != 2 {
		t.Fatal("missing CSRF")
	}
	csrf := matches[1]
	post := func(form url.Values) int {
		t.Helper()
		resp, err := client.PostForm(server.URL+"/dashboard/profile/keys", form)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if code := post(url.Values{"action": {"add"}, "public_key": {"invalid"}}); code != 403 {
		t.Fatal("missing CSRF accepted", code)
	}
	if code, body := get("/api/v1/people/" + person); code != 200 || !strings.Contains(body, `"pgp_keys":[]`) {
		t.Fatal("empty public key array missing", code)
	}
	if code, body := get("/whois/" + slug); code != 200 || strings.Contains(body, `class="whois-pgp-links"`) {
		t.Fatal("empty PGP section shown", code)
	}
	if code, _ := get("/whois/" + slug + "/key.gpg"); code != 404 {
		t.Fatal("empty key bundle should be 404", code)
	}
	var fingerprints []string
	for i := 0; i < 2; i++ {
		e, err := openpgp.NewEntity("Mara preview", "", "mara@example.test", &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA})
		if err != nil {
			t.Fatal(err)
		}
		var binary bytes.Buffer
		if err := e.Serialize(&binary); err != nil {
			t.Fatal(err)
		}
		armored, _ := pgpkeys.Armor(binary.Bytes())
		key, err := pgpkeys.Parse(armored, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		fingerprints = append(fingerprints, key.Fingerprint)
		if post(url.Values{"csrf": {csrf}, "action": {"add"}, "public_key": {armored}}) != 303 {
			t.Fatal("add failed")
		}
		if code, body := get("/api/v1/people/" + person); code != 200 || strings.Contains(body, key.Fingerprint) {
			t.Fatal("API exposed pending key", code)
		}
		if code, _ := get("/whois/" + slug + "/keys/" + key.Fingerprint + ".asc"); code != 404 {
			t.Fatal("unverified download public", code)
		}
		post(url.Values{"csrf": {csrf}, "action": {"challenge"}, "fingerprint": {key.Fingerprint}})
		code, challenge := get("/dashboard/profile/keys?challenge=" + key.Fingerprint)
		if code != 200 || !strings.Contains(challenge, "Account: "+person) {
			t.Fatal("challenge download failed", code)
		}
		var sig bytes.Buffer
		if err := openpgp.ArmoredDetachSign(&sig, []*openpgp.Entity{e}, strings.NewReader(challenge), nil); err != nil {
			t.Fatal(err)
		}
		post(url.Values{"csrf": {csrf}, "action": {"verify"}, "fingerprint": {key.Fingerprint}, "signature": {sig.String()}})
		code, body := get("/whois/" + slug)
		if code != 200 || !strings.Contains(body, key.KeyID) {
			t.Fatal("verified badge absent", code, body)
		}
	}
	for _, format := range []string{"asc", "gpg"} {
		code, body := get("/whois/" + slug + "/key." + format)
		if code != 200 {
			t.Fatal("bundle status", code)
		}
		var keys openpgp.EntityList
		if format == "asc" {
			keys, err = openpgp.ReadArmoredKeyRing(strings.NewReader(body))
		} else {
			keys, err = openpgp.ReadKeyRing(strings.NewReader(body))
		}
		if err != nil || len(keys) != 2 {
			t.Fatal("invalid bundle", len(keys), err)
		}
	}
	code, body = get("/api/v1/people/" + person)
	if code != 200 || !strings.Contains(body, `"pgp_keys":[{`) || strings.Contains(body, "Nonce:") {
		t.Fatal("API public key projection", code, body)
	}
	post(url.Values{"csrf": {csrf}, "action": {"remove"}, "fingerprint": {fingerprints[0]}})
	if code, _ := get("/whois/" + slug + "/keys/" + fingerprints[0] + ".asc"); code != 404 {
		t.Fatal("removed key remains downloadable")
	}
	if code, body := get("/api/v1/people/" + person); code != 200 || strings.Contains(body, fingerprints[0]) {
		t.Fatal("removed key remains in API")
	}
	if os.Getenv("PGP_BROWSER_PREVIEW") == "1" {
		// Leave a pending key alongside the verified key for both UI states.
		e, _ := openpgp.NewEntity("Preview pending", "", "pending@example.test", &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA})
		var b bytes.Buffer
		e.Serialize(&b)
		armored, _ := pgpkeys.Armor(b.Bytes())
		key, _ := pgpkeys.Parse(armored, time.Now())
		if err := getters.AddPersonPGPKey(app, person, armored); err != nil {
			t.Fatal(err)
		}
		if err := getters.StartPersonPGPChallenge(app, person, key.Fingerprint); err != nil {
			t.Fatal(err)
		}
		t.Logf("PGP preview: http://127.0.0.1:8095/preview ; profile http://127.0.0.1:8095/whois/%s", slug)
		t.Fatal(http.ListenAndServe("127.0.0.1:8095", handler))
	}
}
