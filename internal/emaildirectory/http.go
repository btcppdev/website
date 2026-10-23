package emaildirectory

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Handler struct {
	Account  func(*http.Request) (personID, csrf string, err error)
	Store    Store
	Tokens   map[string]string
	Origin   string
	SendLink func(context.Context, string, string) error
}

func (h *Handler) Routes() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("POST /api/internal/email-directory/{service}/resolve", h.resolve)
	m.HandleFunc("POST /api/internal/email-directory/{service}/verified-account", h.verifiedAccount)
	m.HandleFunc("GET /api/internal/email-directory/{service}/contacts/{id}", h.contact)
	m.HandleFunc("PUT /api/internal/email-directory/{service}/contacts/{id}", h.change)
	m.HandleFunc("GET /email-preferences", h.requestPage)
	m.HandleFunc("GET /dashboard/email-subscriptions", h.accountManage)
	m.HandleFunc("POST /dashboard/email-subscriptions", h.accountSave)
	m.HandleFunc("POST /email-preferences", h.requestLink)
	m.HandleFunc("POST /email-preferences/open", h.openLink)
	m.HandleFunc("GET /email-preferences/manage", h.manage)
	m.HandleFunc("POST /email-preferences/manage", h.save)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; font-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		if h.Store.DB == nil {
			http.Error(w, "Email preferences temporarily unavailable", 503)
			return
		}
		m.ServeHTTP(w, r)
	})
}
func (h *Handler) authorized(w http.ResponseWriter, r *http.Request) bool {
	service := r.PathValue("service")
	secret := h.Tokens[service]
	actual := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || (service != "insider" && service != "alerts") || (secret != "" && h.Tokens["insider"] == h.Tokens["alerts"]) || len(secret) < 32 || subtle.ConstantTimeCompare([]byte(actual), []byte(secret)) != 1 {
		http.Error(w, "Unauthorized", 401)
		return false
	}
	return true
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		http.Error(w, "Invalid request", 400)
		return false
	}
	if d.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "Invalid request", 400)
		return false
	}
	return true
}
func respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		status := 503
		if errors.Is(err, ErrInput) {
			status = 422
		}
		if errors.Is(err, ErrConflict) {
			status = 409
		}
		if errors.Is(err, pgx.ErrNoRows) {
			status = 404
		}
		http.Error(w, http.StatusText(status), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(w, r) {
		return
	}
	var in struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &in) {
		return
	}
	c, err := h.Store.Resolve(r.Context(), in.Email, r.PathValue("service"))
	respond(w, c, err)
}
func (h *Handler) contact(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(w, r) {
		return
	}
	c, err := h.Store.Get(r.Context(), r.PathValue("id"), r.PathValue("service"))
	respond(w, c, err)
}
func (h *Handler) change(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(w, r) {
		return
	}
	var in struct {
		Changes []Change `json:"changes"`
		Import  bool     `json:"import_only"`
	}
	if !decode(w, r, &in) {
		return
	}
	err := h.Store.Change(r.Context(), r.PathValue("id"), r.PathValue("service"), r.Header.Get("Idempotency-Key"), in.Changes, in.Import)
	if err != nil {
		respond(w, nil, err)
		return
	}
	c, err := h.Store.Get(r.Context(), r.PathValue("id"), r.PathValue("service"))
	respond(w, c, err)
}
func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func tokenHash(raw string) []byte { d := sha256.Sum256([]byte(raw)); return d[:] }
func (h *Handler) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	return origin == "" || origin == strings.TrimRight(h.Origin, "/")
}

//go:embed preferences.html
var preferencesHTML string

var preferencesPage = template.Must(template.New("preferences").Parse(preferencesHTML))

type page struct {
	Notice, Token, CSRF, Key string
	Contact                  *Contact
	Contacts                 []*Contact
	AccountPage              bool
}

func render(w http.ResponseWriter, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	preferencesPage.Execute(w, p)
}
func (h *Handler) requestPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("token") == "" && h.Account != nil {
		person, _, err := h.Account(r)
		if err != nil {
			http.Error(w, "Unable to load account", 500)
			return
		}
		if person != "" {
			http.Redirect(w, r, "/dashboard/email-subscriptions", http.StatusSeeOther)
			return
		}
	}
	raw := r.URL.Query().Get("token")

	if len(raw) > 100 {
		raw = ""
	}
	render(w, page{Token: raw})
}
func (h *Handler) rateLimit(ctx context.Context, bucket string, limit int) (bool, error) {
	var n int
	err := h.Store.DB.QueryRow(ctx, `INSERT INTO email_management_rate_limits(bucket,window_start,attempts) VALUES($1,now(),1) ON CONFLICT(bucket) DO UPDATE SET attempts=CASE WHEN email_management_rate_limits.window_start<now()-interval '1 hour' THEN 1 ELSE email_management_rate_limits.attempts+1 END,window_start=CASE WHEN email_management_rate_limits.window_start<now()-interval '1 hour' THEN now() ELSE email_management_rate_limits.window_start END RETURNING attempts`, bucket).Scan(&n)
	return n <= limit, err
}
func (h *Handler) requestLink(w http.ResponseWriter, r *http.Request) {
	if !h.sameOrigin(r) {
		http.Error(w, "Invalid origin", 403)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	r.ParseForm()
	// Always identical public response, even for unknown or rate-limited addresses.
	defer render(w, page{Notice: "If this address has subscriptions, a management link will arrive shortly."})
	email, err := NormalizeEmail(r.FormValue("email"))
	if err != nil {
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	h.Store.DB.Exec(r.Context(), `DELETE FROM email_management_tokens WHERE expires_at<now(); DELETE FROM email_management_rate_limits WHERE window_start<now()-interval '1 day'`)
	for _, limit := range []struct {
		bucket  string
		maximum int
	}{{"ip:" + hex.EncodeToString(tokenHash(ip)), 20}, {"email:" + hex.EncodeToString(tokenHash(email)), 3}} {
		ok, err := h.rateLimit(r.Context(), limit.bucket, limit.maximum)
		if err != nil || !ok {
			return
		}
	}
	var id string
	if err = h.Store.DB.QueryRow(r.Context(), `SELECT id::text FROM email_contacts WHERE email=$1 AND EXISTS(SELECT 1 FROM email_memberships WHERE contact_id=email_contacts.id)`, email).Scan(&id); err != nil {
		return
	}
	raw := randomToken()
	_, err = h.Store.DB.Exec(r.Context(), `INSERT INTO email_management_tokens(token_hash,contact_id,purpose,expires_at) VALUES($1,$2,'link',now()+interval '30 minutes')`, tokenHash(raw), id)
	if err == nil && h.SendLink != nil {
		// Do not expose contact existence through mailer network latency. The
		// underlying mailer accepts a queued job; a failed request can be retried
		// by requesting another link without consuming the previous one.
		link := strings.TrimRight(h.Origin, "/") + "/email-preferences?token=" + url.QueryEscape(raw)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = h.SendLink(ctx, email, link)
		}()
	}
	// Bounded retention for bearer credentials and rate buckets.
	h.Store.DB.Exec(r.Context(), `DELETE FROM email_management_tokens WHERE expires_at<now(); DELETE FROM email_management_rate_limits WHERE window_start<now()-interval '1 day'`)
}
func (h *Handler) openLink(w http.ResponseWriter, r *http.Request) {
	if !h.sameOrigin(r) {
		http.Error(w, "Invalid origin", 403)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	r.ParseForm()
	raw := r.FormValue("token")
	tx, err := h.Store.DB.Begin(r.Context())
	if err != nil {
		respond(w, nil, err)
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	err = tx.QueryRow(r.Context(), `DELETE FROM email_management_tokens WHERE token_hash=$1 AND purpose='link' AND expires_at>now() RETURNING contact_id::text`, tokenHash(raw)).Scan(&id)
	if err != nil {
		http.Error(w, "This link is invalid or expired. Request a new link.", 410)
		return
	}
	session := randomToken()
	_, err = tx.Exec(r.Context(), `INSERT INTO email_management_tokens(token_hash,contact_id,purpose,expires_at) VALUES($1,$2,'session',now()+interval '30 minutes')`, tokenHash(session), id)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		respond(w, nil, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "email_preferences", Value: session, Path: "/email-preferences", HttpOnly: true, Secure: strings.HasPrefix(h.Origin, "https://"), SameSite: http.SameSiteStrictMode, MaxAge: 1800})
	http.Redirect(w, r, "/email-preferences/manage", http.StatusSeeOther)
}
func (h *Handler) session(r *http.Request) (string, string, error) {
	cookie, err := r.Cookie("email_preferences")
	if err != nil {
		return "", "", err
	}
	var id string
	err = h.Store.DB.QueryRow(r.Context(), `SELECT contact_id::text FROM email_management_tokens WHERE token_hash=$1 AND purpose='session' AND expires_at>now()`, tokenHash(cookie.Value)).Scan(&id)
	return id, hex.EncodeToString(tokenHash("csrf:" + cookie.Value)), err
}
func (h *Handler) manage(w http.ResponseWriter, r *http.Request) {
	id, csrf, err := h.session(r)
	if err != nil {
		http.Error(w, "Request a new management link.", 401)
		return
	}
	c, err := h.Store.Get(r.Context(), id, "")
	if err != nil {
		respond(w, nil, err)
		return
	}
	render(w, page{Contact: c, CSRF: csrf, Key: randomToken()})
}
func (h *Handler) save(w http.ResponseWriter, r *http.Request) {
	id, csrf, err := h.session(r)
	if err != nil {
		http.Error(w, "Request a new management link.", 401)
		return
	}
	h.savePreferences(w, r, id, csrf, "/email-preferences/manage")
}

func (h *Handler) savePreferences(w http.ResponseWriter, r *http.Request, id, csrf, destination string) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	r.ParseForm()
	if !h.sameOrigin(r) || subtle.ConstantTimeCompare([]byte(csrf), []byte(r.FormValue("csrf"))) != 1 {
		http.Error(w, "Invalid request", 403)
		return
	}
	version, err := strconv.ParseInt(r.FormValue("version"), 10, 64)
	if err != nil {
		respond(w, nil, ErrInput)
		return
	}
	subscribed, err := strconv.ParseBool(r.FormValue("subscribed"))
	if err != nil {
		respond(w, nil, ErrInput)
		return
	}
	service, list := r.FormValue("service"), r.FormValue("newsletter")
	// Management links can only change memberships already visible to this contact.
	c, err := h.Store.Get(r.Context(), id, service)
	if err != nil {
		respond(w, nil, err)
		return
	}
	var current *Membership
	for i := range c.Memberships {
		if c.Memberships[i].Newsletter == list {
			current = &c.Memberships[i]
		}
	}
	if current == nil {
		respond(w, nil, ErrInput)
		return
	}
	err = h.Store.Change(r.Context(), id, service, r.FormValue("key"), []Change{{Newsletter: list, Subscribed: subscribed, ExpectedVersion: version, DeliveryMode: current.DeliveryMode, PausedUntil: current.PausedUntil}}, false)
	if err != nil {
		respond(w, nil, err)
		return
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

func (h *Handler) verifiedAccount(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(w, r) {
		return
	}
	var in struct {
		Email   string `json:"email"`
		ProofID string `json:"proof_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	account, err := h.Store.EnsureVerifiedAccount(r.Context(), in.Email, r.PathValue("service"), in.ProofID)
	respond(w, account, err)
}
