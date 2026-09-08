package getters

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	OrganizationMembershipPolicyRequest = "request"
	OrganizationMembershipPolicyOpen    = "open"
	OrganizationMembershipPolicyClosed  = "closed"
)

var ErrOrganizationMembershipRequestPending = errors.New("a membership request is already pending")
var ErrOrganizationApplicationPending = errors.New("an organization application with that name is already pending")

func validOrganizationMembershipPolicy(policy string) bool {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case OrganizationMembershipPolicyRequest, OrganizationMembershipPolicyOpen, OrganizationMembershipPolicyClosed:
		return true
	default:
		return false
	}
}

func UpdateOrganizationMembershipPolicy(ctx *config.AppContext, organizationID, policy string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	policy = strings.ToLower(strings.TrimSpace(policy))
	if !validOrganizationMembershipPolicy(policy) {
		return fmt.Errorf("join policy must be request, open, or closed")
	}
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE organizations SET membership_policy = $2 WHERE id = $1::uuid
	`, strings.TrimSpace(organizationID), policy)
	if err != nil {
		return fmt.Errorf("update organization membership policy: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("organization not found")
	}
	return nil
}

func ListOrganizationDirectoryForPerson(ctx *config.AppContext, personID string) ([]*types.OrganizationDirectoryEntry, error) {
	return ListOrganizationDirectoryForPersonFiltered(ctx, personID, "", 0)
}

func ListOrganizationDirectoryForPersonFiltered(ctx *config.AppContext, personID, search string, limit int) ([]*types.OrganizationDirectoryEntry, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	personID = strings.TrimSpace(personID)
	search = strings.TrimSpace(search)
	if limit < 0 {
		return nil, fmt.Errorf("organization directory limit cannot be negative")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT organizations.id::text, organizations.name, organizations.tagline,
			organizations.logo_light_url, organizations.logo_dark_url,
			organizations.website_url, organizations.github_url,
			organizations.membership_policy,
			coalesce(memberships.role, ''), coalesce(memberships.status, ''),
			coalesce(requests.id::text, ''), coalesce(requests.status, '')
		FROM organizations
		LEFT JOIN organization_memberships memberships
		  ON memberships.organization_id = organizations.id
		 AND memberships.person_id = $1::uuid
		 AND memberships.status = 'active'
		LEFT JOIN LATERAL (
			SELECT id, status
			FROM organization_membership_requests
			WHERE organization_id = organizations.id AND person_id = $1::uuid
			ORDER BY created_at DESC LIMIT 1
		) requests ON true
		WHERE NOT organizations.hidden_from_directory
		  AND ($2 = ''
			OR strpos(lower(organizations.name), lower($2)) > 0
			OR strpos(lower(organizations.tagline), lower($2)) > 0)
		ORDER BY lower(organizations.name), organizations.id
		LIMIT NULLIF($3::integer, 0)
	`, personID, search, limit)
	if err != nil {
		return nil, fmt.Errorf("list organization directory: %w", err)
	}
	defer rows.Close()
	var out []*types.OrganizationDirectoryEntry
	for rows.Next() {
		entry := &types.OrganizationDirectoryEntry{Organization: &types.Org{}}
		if err := rows.Scan(
			&entry.Organization.Ref, &entry.Organization.Name, &entry.Organization.Tagline,
			&entry.Organization.LogoLight, &entry.Organization.LogoDark,
			&entry.Organization.Website, &entry.Organization.Github,
			&entry.Organization.MembershipPolicy, &entry.MembershipRole,
			&entry.MembershipStatus, &entry.RequestID, &entry.RequestStatus,
		); err != nil {
			return nil, fmt.Errorf("scan organization directory: %w", err)
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func CreateOrganizationMembershipRequest(ctx *config.AppContext, organizationID, personID, message string) (*types.OrganizationMembershipRequest, bool, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, false, fmt.Errorf("database is not configured")
	}
	organizationID = strings.TrimSpace(organizationID)
	personID = strings.TrimSpace(personID)
	message = strings.TrimSpace(message)
	if organizationID == "" || personID == "" {
		return nil, false, fmt.Errorf("organization and person are required")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return nil, false, fmt.Errorf("begin organization membership request: %w", err)
	}
	defer tx.Rollback(dbctx)
	var policy, organizationName string
	if err := tx.QueryRow(dbctx, `SELECT membership_policy, name FROM organizations WHERE id = $1::uuid FOR UPDATE`, organizationID).Scan(&policy, &organizationName); err != nil {
		return nil, false, fmt.Errorf("organization not found")
	}
	if policy == OrganizationMembershipPolicyClosed {
		return nil, false, fmt.Errorf("this organization is not accepting membership requests")
	}
	var member bool
	if err := tx.QueryRow(dbctx, `SELECT EXISTS (SELECT 1 FROM organization_memberships WHERE organization_id = $1::uuid AND person_id = $2::uuid AND status = 'active')`, organizationID, personID).Scan(&member); err != nil {
		return nil, false, fmt.Errorf("check organization membership: %w", err)
	}
	if member {
		return nil, false, fmt.Errorf("you are already a member of this organization")
	}
	var pending bool
	if err := tx.QueryRow(dbctx, `SELECT EXISTS (SELECT 1 FROM organization_membership_requests WHERE organization_id = $1::uuid AND person_id = $2::uuid AND status = 'pending')`, organizationID, personID).Scan(&pending); err != nil {
		return nil, false, fmt.Errorf("check pending organization membership request: %w", err)
	}
	if pending {
		return nil, false, ErrOrganizationMembershipRequestPending
	}
	autoApproved := policy == OrganizationMembershipPolicyOpen
	status := "pending"
	if autoApproved {
		status = "approved"
		if _, err := tx.Exec(dbctx, `
			INSERT INTO organization_memberships (organization_id, person_id, role, status)
			VALUES ($1::uuid, $2::uuid, 'member', 'active')
			ON CONFLICT (organization_id, person_id) DO UPDATE SET status = 'active', role = 'member', updated_at = now()
		`, organizationID, personID); err != nil {
			return nil, false, fmt.Errorf("activate open organization membership: %w", err)
		}
	}
	request := &types.OrganizationMembershipRequest{OrganizationID: organizationID, OrganizationName: organizationName, PersonID: personID, Message: message, Status: status}
	var reviewedAt pgtype.Timestamptz
	if err := tx.QueryRow(dbctx, `
		INSERT INTO organization_membership_requests (
			organization_id, person_id, message, status, reviewed_at
		) VALUES ($1::uuid, $2::uuid, $3, $4, CASE WHEN $4 = 'approved' THEN now() END)
		RETURNING id::text, reviewed_at, created_at, updated_at
	`, organizationID, personID, message, status).Scan(&request.ID, &reviewedAt, &request.CreatedAt, &request.UpdatedAt); err != nil {
		return nil, false, fmt.Errorf("create organization membership request: %w", err)
	}
	if reviewedAt.Valid {
		value := reviewedAt.Time
		request.ReviewedAt = &value
	}
	if err := tx.QueryRow(dbctx, `
		SELECT people.name, coalesce(contact.email::text, '')
		FROM people
		LEFT JOIN LATERAL (
			SELECT email FROM person_emails
			WHERE person_id = people.id AND verified_at IS NOT NULL
			ORDER BY is_primary DESC, verified_at DESC, created_at, id LIMIT 1
		) contact ON true
		WHERE people.id = $1::uuid
	`, personID).Scan(&request.PersonName, &request.PersonEmail); err != nil {
		return nil, false, fmt.Errorf("load membership requester: %w", err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return nil, false, fmt.Errorf("commit organization membership request: %w", err)
	}
	return request, autoApproved, nil
}

func ListPendingOrganizationMembershipRequests(ctx *config.AppContext, organizationID string) ([]*types.OrganizationMembershipRequest, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT requests.id::text, requests.organization_id::text, organizations.name,
			requests.person_id::text, people.name, coalesce(primary_email.email::text, ''),
			requests.message, requests.status, requests.created_at, requests.updated_at
		FROM organization_membership_requests requests
		JOIN organizations ON organizations.id = requests.organization_id
		JOIN people ON people.id = requests.person_id
		LEFT JOIN LATERAL (
			SELECT email FROM person_emails WHERE person_id = people.id
			ORDER BY is_primary DESC, verified_at DESC NULLS LAST LIMIT 1
		) primary_email ON true
		WHERE requests.organization_id = $1::uuid AND requests.status = 'pending'
		ORDER BY requests.created_at
	`, strings.TrimSpace(organizationID))
	if err != nil {
		return nil, fmt.Errorf("list pending organization membership requests: %w", err)
	}
	defer rows.Close()
	var out []*types.OrganizationMembershipRequest
	for rows.Next() {
		request := &types.OrganizationMembershipRequest{}
		if err := rows.Scan(&request.ID, &request.OrganizationID, &request.OrganizationName,
			&request.PersonID, &request.PersonName, &request.PersonEmail, &request.Message,
			&request.Status, &request.CreatedAt, &request.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan organization membership request: %w", err)
		}
		out = append(out, request)
	}
	return out, rows.Err()
}

func ReviewOrganizationMembershipRequest(ctx *config.AppContext, organizationID, requestID, reviewerPersonID, decision, note string) (*types.OrganizationMembershipRequest, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	decision = strings.ToLower(strings.TrimSpace(decision))
	if decision != "approved" && decision != "denied" {
		return nil, fmt.Errorf("decision must be approved or denied")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return nil, fmt.Errorf("begin membership request review: %w", err)
	}
	defer tx.Rollback(dbctx)
	request := &types.OrganizationMembershipRequest{ID: strings.TrimSpace(requestID), OrganizationID: strings.TrimSpace(organizationID)}
	if err := tx.QueryRow(dbctx, `
		SELECT requests.person_id::text, organizations.name, people.name,
			coalesce(contact.email::text, ''), requests.message, requests.status
		FROM organization_membership_requests requests
		JOIN organizations ON organizations.id = requests.organization_id
		JOIN people ON people.id = requests.person_id
		LEFT JOIN LATERAL (
			SELECT email FROM person_emails
			WHERE person_id = people.id AND verified_at IS NOT NULL
			ORDER BY is_primary DESC, verified_at DESC, created_at, id LIMIT 1
		) contact ON true
		WHERE requests.id = $1::uuid AND requests.organization_id = $2::uuid
		FOR UPDATE OF requests
	`, request.ID, request.OrganizationID).Scan(&request.PersonID, &request.OrganizationName,
		&request.PersonName, &request.PersonEmail, &request.Message, &request.Status); err != nil {
		return nil, fmt.Errorf("pending membership request not found")
	}
	if request.Status != "pending" {
		return nil, fmt.Errorf("that membership request has already been decided")
	}
	if decision == "approved" {
		if _, err := tx.Exec(dbctx, `
			INSERT INTO organization_memberships (organization_id, person_id, role, status, invited_by_person_id)
			VALUES ($1::uuid, $2::uuid, 'member', 'active', $3::uuid)
			ON CONFLICT (organization_id, person_id) DO UPDATE SET status = 'active', role = 'member', invited_by_person_id = $3::uuid, updated_at = now()
		`, request.OrganizationID, request.PersonID, reviewerPersonID); err != nil {
			return nil, fmt.Errorf("approve organization membership: %w", err)
		}
	}
	var reviewedAt pgtype.Timestamptz
	if err := tx.QueryRow(dbctx, `
		UPDATE organization_membership_requests
		SET status = $3, reviewed_by_person_id = $4::uuid, review_note = $5, reviewed_at = now()
		WHERE id = $1::uuid AND organization_id = $2::uuid
		RETURNING status, review_note, reviewed_at, updated_at
	`, request.ID, request.OrganizationID, decision, reviewerPersonID, strings.TrimSpace(note)).Scan(&request.Status, &request.ReviewNote, &reviewedAt, &request.UpdatedAt); err != nil {
		return nil, fmt.Errorf("record organization membership decision: %w", err)
	}
	if reviewedAt.Valid {
		value := reviewedAt.Time
		request.ReviewedAt = &value
	}
	if err := tx.Commit(dbctx); err != nil {
		return nil, fmt.Errorf("commit organization membership decision: %w", err)
	}
	return request, nil
}

// ListOrganizationManagerRecipients returns active owners and managers at a
// currently verified address. It is intentionally narrower than the member
// list used by the UI so notification fan-out never falls back to stale mail.
func ListOrganizationManagerRecipients(ctx *config.AppContext, organizationID string) ([]*types.NotificationRecipient, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT memberships.person_id::text, people.name, contact.email::text
		FROM organization_memberships memberships
		JOIN people ON people.id = memberships.person_id
		JOIN LATERAL (
			SELECT email FROM person_emails
			WHERE person_id = people.id AND verified_at IS NOT NULL
			ORDER BY is_primary DESC, verified_at DESC, created_at, id LIMIT 1
		) contact ON true
		WHERE memberships.organization_id = $1::uuid
		  AND memberships.status = 'active'
		  AND memberships.role IN ('owner', 'manager')
		ORDER BY lower(contact.email::text), memberships.person_id
	`, strings.TrimSpace(organizationID))
	if err != nil {
		return nil, fmt.Errorf("list organization manager recipients: %w", err)
	}
	defer rows.Close()
	var recipients []*types.NotificationRecipient
	for rows.Next() {
		recipient := &types.NotificationRecipient{}
		if err := rows.Scan(&recipient.PersonID, &recipient.Name, &recipient.Email); err != nil {
			return nil, fmt.Errorf("scan organization manager recipient: %w", err)
		}
		recipients = append(recipients, recipient)
	}
	return recipients, rows.Err()
}

func CreateOrganizationApplication(ctx *config.AppContext, application *types.OrganizationApplication) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if application == nil {
		return fmt.Errorf("organization application is required")
	}
	application.Name = strings.TrimSpace(application.Name)
	application.Tagline = strings.TrimSpace(application.Tagline)
	application.ApplicantEmail = strings.ToLower(strings.TrimSpace(application.ApplicantEmail))
	application.ContactEmail = strings.ToLower(strings.TrimSpace(application.ContactEmail))
	application.Website = strings.TrimSpace(application.Website)
	application.Github = strings.TrimSpace(application.Github)
	application.LogoLight = strings.TrimSpace(application.LogoLight)
	application.LogoDark = strings.TrimSpace(application.LogoDark)
	application.Notes = strings.TrimSpace(application.Notes)
	if application.Name == "" || application.SubmittedByPersonID == "" {
		return fmt.Errorf("organization name and applicant are required")
	}
	if application.LogoLight == "" || application.LogoDark == "" {
		return fmt.Errorf("both light- and dark-background organization logos are required")
	}
	if parsed, err := mail.ParseAddress(application.ApplicantEmail); err != nil || !strings.EqualFold(parsed.Address, application.ApplicantEmail) {
		return fmt.Errorf("a valid applicant email is required")
	}
	if application.ContactEmail != "" {
		if parsed, err := mail.ParseAddress(application.ContactEmail); err != nil || !strings.EqualFold(parsed.Address, application.ContactEmail) {
			return fmt.Errorf("contact email must be a valid email address")
		}
	}
	var duplicate bool
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT EXISTS (
			SELECT 1 FROM organizations WHERE lower(name) = lower($1)
			UNION ALL
			SELECT 1 FROM organization_applications WHERE lower(name) = lower($1) AND status = 'pending'
		)
	`, application.Name).Scan(&duplicate); err != nil {
		return fmt.Errorf("check organization application: %w", err)
	}
	if duplicate {
		return ErrOrganizationApplicationPending
	}
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO organization_applications (
			submitted_by_person_id, applicant_email, name, tagline, contact_email,
			website_url, github_url, logo_light_url, logo_dark_url, notes
		) VALUES ($1::uuid, $2::citext, $3, $4, NULLIF($5, '')::citext, $6, $7, $8, $9, $10)
		RETURNING id::text, status, created_at, updated_at
	`, application.SubmittedByPersonID, application.ApplicantEmail, application.Name,
		application.Tagline, application.ContactEmail, application.Website,
		application.Github, application.LogoLight, application.LogoDark,
		application.Notes).Scan(&application.ID, &application.Status,
		&application.CreatedAt, &application.UpdatedAt); err != nil {
		return fmt.Errorf("create organization application: %w", err)
	}
	return nil
}

func ListOrganizationApplications(ctx *config.AppContext, status string) ([]*types.OrganizationApplication, error) {
	return queryOrganizationApplications(ctx, "WHERE ($1 = '' OR applications.status = $1)", strings.ToLower(strings.TrimSpace(status)))
}

func ListOrganizationApplicationsForPerson(ctx *config.AppContext, personID string) ([]*types.OrganizationApplication, error) {
	return queryOrganizationApplications(ctx, "WHERE applications.submitted_by_person_id = $1::uuid", strings.TrimSpace(personID))
}

func GetOrganizationApplication(ctx *config.AppContext, applicationID string) (*types.OrganizationApplication, error) {
	applications, err := queryOrganizationApplications(ctx, "WHERE applications.id = $1::uuid", strings.TrimSpace(applicationID))
	if err != nil || len(applications) == 0 {
		return nil, err
	}
	return applications[0], nil
}

func queryOrganizationApplications(ctx *config.AppContext, where string, arg any) ([]*types.OrganizationApplication, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT applications.id::text, applications.submitted_by_person_id::text,
			people.name, applications.applicant_email::text, applications.name,
			applications.tagline, coalesce(applications.contact_email::text, ''),
			applications.website_url, applications.github_url,
			applications.logo_light_url, applications.logo_dark_url, applications.notes,
			applications.status, applications.review_note,
			coalesce(applications.reviewed_by_person_id::text, ''),
			applications.reviewed_at, coalesce(applications.organization_id::text, ''),
			applications.created_at, applications.updated_at
		FROM organization_applications applications
		JOIN people ON people.id = applications.submitted_by_person_id
		`+where+`
		ORDER BY applications.created_at DESC
	`, arg)
	if err != nil {
		return nil, fmt.Errorf("query organization applications: %w", err)
	}
	defer rows.Close()
	var out []*types.OrganizationApplication
	for rows.Next() {
		application := &types.OrganizationApplication{}
		var reviewedAt pgtype.Timestamptz
		if err := rows.Scan(
			&application.ID, &application.SubmittedByPersonID, &application.ApplicantName,
			&application.ApplicantEmail, &application.Name, &application.Tagline,
			&application.ContactEmail, &application.Website, &application.Github,
			&application.LogoLight, &application.LogoDark, &application.Notes,
			&application.Status, &application.ReviewNote,
			&application.ReviewedByPersonID, &reviewedAt, &application.OrganizationID,
			&application.CreatedAt, &application.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan organization application: %w", err)
		}
		if reviewedAt.Valid {
			value := reviewedAt.Time
			application.ReviewedAt = &value
		}
		out = append(out, application)
	}
	return out, rows.Err()
}

func ReviewOrganizationApplication(ctx *config.AppContext, applicationID, reviewerPersonID, decision string, edited *types.OrganizationApplication) (*types.OrganizationApplication, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	decision = strings.ToLower(strings.TrimSpace(decision))
	if decision != "approved" && decision != "denied" {
		return nil, fmt.Errorf("decision must be approved or denied")
	}
	if edited == nil || strings.TrimSpace(edited.Name) == "" {
		return nil, fmt.Errorf("organization name is required")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return nil, fmt.Errorf("begin organization application review: %w", err)
	}
	defer tx.Rollback(dbctx)
	application := &types.OrganizationApplication{ID: strings.TrimSpace(applicationID)}
	if err := tx.QueryRow(dbctx, `
		SELECT submitted_by_person_id::text, applicant_email::text, status,
			logo_light_url, logo_dark_url
		FROM organization_applications WHERE id = $1::uuid FOR UPDATE
	`, application.ID).Scan(&application.SubmittedByPersonID, &application.ApplicantEmail,
		&application.Status, &application.LogoLight, &application.LogoDark); err != nil {
		return nil, fmt.Errorf("pending organization application not found")
	}
	if application.Status != "pending" {
		return nil, fmt.Errorf("that organization application has already been decided")
	}
	application.Name = strings.TrimSpace(edited.Name)
	application.Tagline = strings.TrimSpace(edited.Tagline)
	application.ContactEmail = strings.ToLower(strings.TrimSpace(edited.ContactEmail))
	application.Website = strings.TrimSpace(edited.Website)
	application.Github = strings.TrimSpace(edited.Github)
	application.Notes = strings.TrimSpace(edited.Notes)
	application.ReviewNote = strings.TrimSpace(edited.ReviewNote)
	if application.ContactEmail != "" {
		if parsed, err := mail.ParseAddress(application.ContactEmail); err != nil || !strings.EqualFold(parsed.Address, application.ContactEmail) {
			return nil, fmt.Errorf("contact email must be a valid email address")
		}
	}
	if decision == "approved" {
		var exists bool
		if err := tx.QueryRow(dbctx, `SELECT EXISTS (SELECT 1 FROM organizations WHERE lower(name) = lower($1))`, application.Name).Scan(&exists); err != nil {
			return nil, fmt.Errorf("check approved organization name: %w", err)
		}
		if exists {
			return nil, fmt.Errorf("an organization with that name already exists")
		}
		if err := tx.QueryRow(dbctx, `
			INSERT INTO organizations (name, tagline, email, website_url, github_url, logo_light_url, logo_dark_url, notes, membership_policy)
			VALUES ($1, $2, NULLIF($3, '')::citext, $4, $5, $6, $7, $8, 'request')
			RETURNING id::text
		`, application.Name, application.Tagline, application.ContactEmail,
			application.Website, application.Github, application.LogoLight,
			application.LogoDark, application.Notes).Scan(&application.OrganizationID); err != nil {
			return nil, fmt.Errorf("create approved organization: %w", err)
		}
		if _, err := tx.Exec(dbctx, `
			INSERT INTO organization_memberships (organization_id, person_id, role, status, invited_by_person_id)
			VALUES ($1::uuid, $2::uuid, 'owner', 'active', $3::uuid)
		`, application.OrganizationID, application.SubmittedByPersonID, reviewerPersonID); err != nil {
			return nil, fmt.Errorf("make organization applicant owner: %w", err)
		}
	}
	var reviewedAt pgtype.Timestamptz
	if err := tx.QueryRow(dbctx, `
		UPDATE organization_applications SET
			name = $2, tagline = $3, contact_email = NULLIF($4, '')::citext,
			website_url = $5, github_url = $6, logo_light_url = $7,
			logo_dark_url = $8, notes = $9, status = $10,
			review_note = $11, reviewed_by_person_id = $12::uuid,
			reviewed_at = now(), organization_id = NULLIF($13, '')::uuid
		WHERE id = $1::uuid
		RETURNING status, reviewed_at, updated_at
	`, application.ID, application.Name, application.Tagline, application.ContactEmail,
		application.Website, application.Github, application.LogoLight,
		application.LogoDark, application.Notes, decision, application.ReviewNote,
		reviewerPersonID, application.OrganizationID).Scan(
		&application.Status, &reviewedAt, &application.UpdatedAt); err != nil {
		return nil, fmt.Errorf("record organization application decision: %w", err)
	}
	if reviewedAt.Valid {
		value := reviewedAt.Time
		application.ReviewedAt = &value
	}
	if err := tx.Commit(dbctx); err != nil {
		return nil, fmt.Errorf("commit organization application decision: %w", err)
	}
	return application, nil
}
