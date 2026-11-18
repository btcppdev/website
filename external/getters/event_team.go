package getters

import (
	"btcpp-web/internal/config"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/mail"
	"strings"
	"time"
)

type EventTeamMember struct {
	ID             string     `json:"id"`
	Email          string     `json:"email"`
	Name           string     `json:"name"`
	Responsibility string     `json:"responsibility"`
	Role           string     `json:"role"`
	PersonID       string     `json:"person_id"`
	AcceptedAt     *time.Time `json:"accepted_at"`
	ExpiresAt      *time.Time `json:"expires_at"`
}

func ListEventTeam(ctx *config.AppContext, confID string) ([]EventTeamMember, error) {
	rows, e := ctx.DB.Query(ctx.DatabaseContext(), `SELECT id::text,coalesce(email::text,''),name,responsibility,role,coalesce(person_id::text,''),accepted_at,expires_at FROM event_team_members WHERE conference_id=$1 ORDER BY role,name,email`, confID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []EventTeamMember{}
	for rows.Next() {
		var m EventTeamMember
		if e = rows.Scan(&m.ID, &m.Email, &m.Name, &m.Responsibility, &m.Role, &m.PersonID, &m.AcceptedAt, &m.ExpiresAt); e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func EventTeamCanRead(ctx *config.AppContext, confID, personID string) bool {
	var yes bool
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `SELECT EXISTS(SELECT 1 FROM event_team_members WHERE conference_id=$1 AND person_id=$2 AND accepted_at IS NOT NULL)`, confID, personID).Scan(&yes)
	return err == nil && yes
}
func CreateEventTeamInvite(ctx *config.AppContext, confID, actor string, m EventTeamMember) (string, error) {
	m.Email = strings.ToLower(strings.TrimSpace(m.Email))
	addr, e := mail.ParseAddress(m.Email)
	if e != nil || addr.Address != m.Email {
		return "", fmt.Errorf("enter a valid email address")
	}
	switch m.Role {
	case "steering", "staff", "volcoord", "admin":
	default:
		return "", fmt.Errorf("choose a valid event role")
	}
	tokenBytes := make([]byte, 32)
	if _, e = rand.Read(tokenBytes); e != nil {
		return "", e
	}
	token := hex.EncodeToString(tokenBytes)
	hash := sha256.Sum256([]byte(token))
	tag, e := ctx.DB.Exec(ctx.DatabaseContext(), `INSERT INTO event_team_members(conference_id,email,name,responsibility,role,invited_by,token_hash,expires_at) VALUES($1,$2,$3,$4,$5,$6::uuid,$7,now()+interval '14 days') ON CONFLICT(conference_id,email) DO UPDATE SET name=excluded.name,responsibility=excluded.responsibility,role=excluded.role,invited_by=excluded.invited_by,token_hash=excluded.token_hash,expires_at=excluded.expires_at WHERE event_team_members.accepted_at IS NULL`, confID, m.Email, m.Name, m.Responsibility, m.Role, nullableUUID(actor), hex.EncodeToString(hash[:]))
	if e != nil {
		return "", e
	}
	if tag.RowsAffected() == 0 {
		return "", fmt.Errorf("this person already joined the team")
	}
	return token, nil
}
func AcceptEventTeamInvite(ctx *config.AppContext, token, personID string) (string, error) {
	hash := sha256.Sum256([]byte(token))
	c := ctx.DatabaseContext()
	tx, e := ctx.DB.Begin(c)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(c)
	var id, tag, role, email string
	e = tx.QueryRow(c, `SELECT m.id::text,c.tag,m.role,m.email::text FROM event_team_members m JOIN conferences c ON c.id=m.conference_id WHERE token_hash=$1 AND expires_at>now() AND accepted_at IS NULL FOR UPDATE OF m`, hex.EncodeToString(hash[:])).Scan(&id, &tag, &role, &email)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", fmt.Errorf("invitation is expired or already accepted")
	}
	if e != nil {
		return "", e
	}
	var owns bool
	if e = tx.QueryRow(c, `SELECT EXISTS(SELECT 1 FROM person_emails WHERE person_id=$1 AND email=$2 AND verified_at IS NOT NULL)`, personID, email).Scan(&owns); e != nil {
		return "", e
	}
	if !owns {
		return "", fmt.Errorf("sign in with the email address this invitation was sent to")
	}
	if _, e = tx.Exec(c, `UPDATE event_team_members SET person_id=$2,accepted_at=now(),token_hash='' WHERE id=$1`, id, personID); e != nil {
		return "", e
	}
	if role != "steering" {
		grant, err := tx.Exec(c, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, personID, tag, role)
		if err != nil {
			return "", err
		}
		if _, err = tx.Exec(c, `UPDATE event_team_members SET role_granted=$2 WHERE id=$1`, id, grant.RowsAffected() > 0); err != nil {
			return "", err
		}
	}
	return tag, tx.Commit(c)
}

// RevokeEventTeamInvite removes only access granted by this invitation.
func RevokeEventTeamInvite(ctx *config.AppContext, confID, id string) error {
	c := ctx.DatabaseContext()
	tx, e := ctx.DB.Begin(c)
	if e != nil {
		return e
	}
	defer tx.Rollback(c)
	var person, tag, role string
	var granted bool
	e = tx.QueryRow(c, `SELECT coalesce(m.person_id::text,''),c.tag,m.role,m.role_granted FROM event_team_members m JOIN conferences c ON c.id=m.conference_id WHERE m.id=$1 AND m.conference_id=$2 FOR UPDATE OF m`, id, confID).Scan(&person, &tag, &role, &granted)
	if e != nil {
		return fmt.Errorf("team member not found in this event")
	}
	if granted && person != "" {
		if _, e = tx.Exec(c, `DELETE FROM people_roles WHERE person_id=$1 AND scope=$2 AND position=$3`, person, tag, role); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(c, `DELETE FROM event_team_members WHERE id=$1`, id); e != nil {
		return e
	}
	return tx.Commit(c)
}

// AddEventTeamMember assigns an existing account directly. Identity fields are
// always loaded from the account, never from the submitted display text.
func AddEventTeamMember(app *config.AppContext, confID, actor string, m EventTeamMember) error {
	if _, e := uuid.Parse(m.PersonID); e != nil {
		return fmt.Errorf("choose an existing account from the search results")
	}
	switch m.Role {
	case "steering", "staff", "volcoord", "admin":
	default:
		return fmt.Errorf("choose a valid event role")
	}
	m.Responsibility = strings.TrimSpace(m.Responsibility)
	if len(m.Responsibility) > 2000 {
		return fmt.Errorf("keep responsibilities under 2,000 characters")
	}
	c := app.DatabaseContext()
	tx, e := app.DB.Begin(c)
	if e != nil {
		return e
	}
	defer tx.Rollback(c)
	// Serializing additions also keeps retries from adding duplicate members.
	var tag string
	if e = tx.QueryRow(c, `SELECT tag FROM conferences WHERE id=$1 FOR UPDATE`, confID).Scan(&tag); e != nil {
		return e
	}
	var name, email string
	if e = tx.QueryRow(c, `SELECT p.name,coalesce((SELECT email::text FROM person_emails WHERE person_id=p.id ORDER BY is_primary DESC,created_at,id LIMIT 1),'') FROM people p WHERE p.id=$1 AND NOT p.is_deleted_account FOR SHARE OF p`, m.PersonID).Scan(&name, &email); e != nil {
		return fmt.Errorf("this account is no longer available")
	}
	var exists bool
	if e = tx.QueryRow(c, `SELECT EXISTS(SELECT 1 FROM event_team_members WHERE conference_id=$1 AND person_id=$2)`, confID, m.PersonID).Scan(&exists); e != nil {
		return e
	}
	if exists {
		return fmt.Errorf("this person is already on the team")
	}
	var memberID string
	e = tx.QueryRow(c, `INSERT INTO event_team_members(conference_id,person_id,email,name,responsibility,role,invited_by,accepted_at) VALUES($1,$2,NULLIF($3,'')::citext,$4,$5,$6,$7,now()) ON CONFLICT(conference_id,email) DO UPDATE SET person_id=excluded.person_id,name=excluded.name,responsibility=excluded.responsibility,role=excluded.role,invited_by=excluded.invited_by,accepted_at=now(),token_hash='',expires_at=NULL WHERE event_team_members.accepted_at IS NULL RETURNING id::text`, confID, m.PersonID, email, name, m.Responsibility, m.Role, nullableUUID(actor)).Scan(&memberID)
	if errors.Is(e, pgx.ErrNoRows) {
		return fmt.Errorf("this email already belongs to a team member")
	}
	if e != nil {
		return e
	}
	if m.Role != "steering" {
		result, e := tx.Exec(c, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, m.PersonID, tag, m.Role)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(c, `UPDATE event_team_members SET role_granted=$2 WHERE id=$1`, memberID, result.RowsAffected() > 0); e != nil {
			return e
		}
	}
	return tx.Commit(c)
}
