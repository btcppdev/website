package types

import "time"

type EasyshipSettings struct {
	ContactName   string
	CompanyName   string
	Email         string
	Phone         string
	Line1         string
	Line2         string
	City          string
	Region        string
	PostalCode    string
	CountryAlpha2 string
	UpdatedBy     string
	CreatedAt     *time.Time
	UpdatedAt     *time.Time
}

type EasyshipWebhookEvent struct {
	ID                 string
	EventType          string
	ResourceID         string
	EasyshipShipmentID string
	Status             string
	Attempts           int
	LastError          string
	ReceivedAt         time.Time
	ProcessedAt        *time.Time
}

func (s *EasyshipSettings) IsConfigured() bool {
	if s == nil || s.ContactName == "" || s.Line1 == "" || s.City == "" ||
		s.Region == "" || s.PostalCode == "" || len(s.CountryAlpha2) != 2 {
		return false
	}
	return s.CountryAlpha2[0] >= 'A' && s.CountryAlpha2[0] <= 'Z' &&
		s.CountryAlpha2[1] >= 'A' && s.CountryAlpha2[1] <= 'Z'
}
