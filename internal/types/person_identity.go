package types

import "time"

type PersonEmail struct {
	ID                 string
	PersonID           string
	Email              string
	IsPrimary          bool
	VerifiedAt         time.Time
	OriginMergeEventID string
	RemovalLocked      bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type PersonEmailConflict struct {
	Email      string
	PersonID   string
	PersonName string
	DetectedAt time.Time
}

type PersonEmailResolution struct {
	Email             string
	Person            *Speaker
	Alias             *PersonEmail
	ConflictPersonIDs []string
}

type PersonMergeRequest struct {
	ID                    string
	RequesterPersonID     string
	RequesterName         string
	RequesterEmail        string
	TargetPersonID        string
	TargetName            string
	TargetEmail           string
	Status                string
	ReviewedByPersonID    string
	ReviewedByName        string
	MergeEventID          string
	ReviewNote            string
	ConfirmationExpiresAt *time.Time
	ConfirmedAt           *time.Time
	CreatedAt             time.Time
	ReviewedAt            *time.Time
}

func (resolution *PersonEmailResolution) IsConflict() bool {
	return resolution != nil && len(resolution.ConflictPersonIDs) > 1
}
