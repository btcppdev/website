package types

import "time"

type PersonEmail struct {
	ID                 string
	PersonID           string
	Email              string
	IsPrimary          bool
	VerifiedAt         time.Time
	OriginMergeEventID string
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

func (resolution *PersonEmailResolution) IsConflict() bool {
	return resolution != nil && len(resolution.ConflictPersonIDs) > 1
}
