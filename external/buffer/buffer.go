package buffer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"time"
)

const apiURL = "https://api.buffer.com/graphql"

var (
	apiKey            string
	orgID             string
	channels          []Channel
	mu                sync.Mutex
	channelsFetchedAt time.Time
)

type Channel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Service string `json:"service"`
}

type PostResult struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	DueAt string `json:"dueAt"`
}

func Init(key string) {
	apiKey = key
}

func IsConfigured() bool {
	return apiKey != ""
}

type graphqlReq struct {
	Query string `json:"query"`
}

type graphqlResp struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func graphqlRequest(query string) (json.RawMessage, error) {
	body, err := json.Marshal(&graphqlReq{Query: query})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("buffer API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var gResp graphqlResp
	if err := json.Unmarshal(respBody, &gResp); err != nil {
		return nil, fmt.Errorf("failed to parse buffer response: %s", err)
	}

	if len(gResp.Errors) > 0 {
		return nil, fmt.Errorf("buffer API error: %s", gResp.Errors[0].Message)
	}

	return gResp.Data, nil
}

func fetchOrgID() (string, error) {
	if orgID != "" {
		return orgID, nil
	}

	data, err := graphqlRequest(`query { account { organizations { id } } }`)
	if err != nil {
		return "", err
	}

	var result struct {
		Account struct {
			Organizations []struct {
				ID string `json:"id"`
			} `json:"organizations"`
		} `json:"account"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", err
	}

	if len(result.Account.Organizations) == 0 {
		return "", fmt.Errorf("no organizations found in Buffer account")
	}

	orgID = result.Account.Organizations[0].ID
	return orgID, nil
}

func FetchChannels() ([]Channel, error) {
	mu.Lock()
	defer mu.Unlock()

	if len(channels) > 0 && time.Since(channelsFetchedAt) < 5*time.Minute {
		return channels, nil
	}

	oid, err := fetchOrgID()
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`query { channels(input: { organizationId: "%s" }) { id name service } }`, oid)
	data, err := graphqlRequest(query)
	if err != nil {
		return nil, err
	}

	var result struct {
		Channels []Channel `json:"channels"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	channels = result.Channels
	channelsFetchedAt = time.Now()
	return channels, nil
}

// Asset is one ordered image or video attachment.
type Asset struct {
	URL  string `json:"url"`
	Kind string `json:"kind"`
}

func ImageAssets(urls []string) []Asset {
	assets := make([]Asset, 0, len(urls))
	for _, u := range urls {
		assets = append(assets, Asset{URL: u, Kind: "image"})
	}
	return assets
}

func buildAssetsBlock(imageURLs []string) string {
	return buildMediaAssetsBlock(ImageAssets(imageURLs))
}

func buildMediaAssetsBlock(media []Asset) string {
	if len(media) == 0 {
		return ""
	}
	var assets []string
	for _, asset := range media {
		escaped, _ := json.Marshal(asset.URL)
		kind := "image"
		if asset.Kind == "video" {
			kind = "video"
		}
		assets = append(assets, fmt.Sprintf(`{ %s: { url: %s } }`, kind, string(escaped)))
	}
	return fmt.Sprintf(`, assets: [%s]`, strings.Join(assets, ", "))
}

// CreateMediaPost preserves the supplied attachment order.
func CreateMediaPost(channelID, text string, assets []Asset, service string) (*PostResult, error) {
	for _, asset := range assets {
		if asset.Kind != "image" && asset.Kind != "video" {
			return nil, fmt.Errorf("unsupported media kind %q", asset.Kind)
		}
	}
	return createMediaPost(channelID, text, assets, service, nil)
}

func CreatePost(channelID, text string, imageURLs []string, service string) (*PostResult, error) {
	return createPost(channelID, text, imageURLs, service, nil)
}

// CreateScheduledPost creates a post at an exact UTC instant instead of
// placing it in the channel's next configured queue slot.
func CreateScheduledPost(channelID, text string, imageURLs []string, service string, dueAt time.Time) (*PostResult, error) {
	dueAt = dueAt.UTC()
	return createPost(channelID, text, imageURLs, service, &dueAt)
}

func createPost(channelID, text string, imageURLs []string, service string, dueAt *time.Time) (*PostResult, error) {
	return createMediaPost(channelID, text, ImageAssets(imageURLs), service, dueAt)
}

func createMediaPost(channelID, text string, assets []Asset, service string, dueAt *time.Time) (*PostResult, error) {
	query := buildCreateMediaPostMutation(channelID, text, assets, service, dueAt)
	data, err := graphqlRequest(query)
	if err != nil {
		return nil, err
	}

	var result struct {
		CreatePost struct {
			Post    *PostResult `json:"post"`
			Message string      `json:"message"`
		} `json:"createPost"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	if result.CreatePost.Message != "" {
		return nil, fmt.Errorf("buffer post error: %s", result.CreatePost.Message)
	}

	if result.CreatePost.Post == nil || result.CreatePost.Post.ID == "" {
		return nil, fmt.Errorf("buffer returned no created post")
	}
	return result.CreatePost.Post, nil
}

// EditScheduledPost updates a previously created Buffer post after an admin
// changes the corresponding YouTube schedule or release copy.
func EditScheduledPost(postID, text string, imageURLs []string, service string, dueAt time.Time) (*PostResult, error) {
	query := buildEditPostMutation(postID, text, imageURLs, service, dueAt.UTC())
	data, err := graphqlRequest(query)
	if err != nil {
		return nil, err
	}
	var result struct {
		EditPost struct {
			Post    *PostResult `json:"post"`
			Message string      `json:"message"`
		} `json:"editPost"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if result.EditPost.Message != "" {
		return nil, fmt.Errorf("buffer post error: %s", result.EditPost.Message)
	}
	return result.EditPost.Post, nil
}

func buildCreatePostMutation(channelID, text string, imageURLs []string, service string, dueAt *time.Time) string {
	return buildCreateMediaPostMutation(channelID, text, ImageAssets(imageURLs), service, dueAt)
}

func buildCreateMediaPostMutation(channelID, text string, assets []Asset, service string, dueAt *time.Time) string {
	textEscaped, _ := json.Marshal(text)

	assetsBlock := buildMediaAssetsBlock(assets)

	var metadataBlock string
	if service == "instagram" {
		igType := "post"
		if len(assets) > 1 {
			igType = "carousel"
		} else if len(assets) == 1 && assets[0].Kind == "video" {
			igType = "reel"
		}
		metadataBlock = fmt.Sprintf(`, metadata: { instagram: { type: %s, shouldShareToFeed: true } }`, igType)
	}
	modeBlock := "mode: addToQueue"
	if dueAt != nil {
		dueEscaped, _ := json.Marshal(dueAt.UTC().Format(time.RFC3339Nano))
		modeBlock = fmt.Sprintf("mode: customScheduled, dueAt: %s", string(dueEscaped))
	}

	return fmt.Sprintf(`mutation {
		createPost(input: {
			text: %s,
			channelId: "%s",
			schedulingType: automatic,
			%s
			%s
			%s
		}) {
			... on PostActionSuccess {
				post { id text dueAt }
			}
			... on MutationError {
				message
			}
		}
	}`, string(textEscaped), channelID, modeBlock, assetsBlock, metadataBlock)
}

func buildEditPostMutation(postID, text string, imageURLs []string, service string, dueAt time.Time) string {
	textEscaped, _ := json.Marshal(text)
	idEscaped, _ := json.Marshal(postID)
	dueEscaped, _ := json.Marshal(dueAt.UTC().Format(time.RFC3339Nano))
	assetsBlock := buildAssetsBlock(imageURLs)
	var metadataBlock string
	if service == "instagram" {
		igType := "post"
		if len(imageURLs) > 1 {
			igType = "carousel"
		}
		metadataBlock = fmt.Sprintf(`, metadata: { instagram: { type: %s, shouldShareToFeed: true } }`, igType)
	}
	return fmt.Sprintf(`mutation {
		editPost(input: {
			id: %s,
			text: %s,
			schedulingType: automatic,
			mode: customScheduled,
			dueAt: %s
			%s
			%s
		}) {
			... on PostActionSuccess {
				post { id text dueAt }
			}
			... on MutationError {
				message
			}
		}
	}`, string(idEscaped), string(textEscaped), string(dueEscaped), assetsBlock, metadataBlock)
}
