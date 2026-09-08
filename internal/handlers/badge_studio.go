package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var badgeStudioHTTPClient = &http.Client{Timeout: 4 * time.Second}

func loadBadgeStudioProfile(ctx context.Context, baseURL, personID string) (*WhoIsBadgeProfile, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/public/btcpp/people/"+url.PathEscape(personID)+"/badges", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := badgeStudioHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request Badge Studio profile: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("Badge Studio profile returned status %d", response.StatusCode)
	}
	var profile WhoIsBadgeProfile
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&profile); err != nil {
		return nil, fmt.Errorf("decode Badge Studio profile: %w", err)
	}
	for index := range profile.Issued {
		award := &profile.Issued[index]
		if len(award.Award.Recipients) == 1 {
			award.CredentialURL = baseURL + "/credentials/" + url.PathEscape(award.Award.EventID) + "/" + url.PathEscape(award.Award.Recipients[0])
		}
	}
	return &profile, nil
}
