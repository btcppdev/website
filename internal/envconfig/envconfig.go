package envconfig

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"btcpp-web/internal/types"
)

// LoadDotEnv loads KEY=VALUE pairs from path without overwriting variables
// already present in the process environment.
func LoadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		if err := os.Setenv(key, val); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func Load(path string) (*types.EnvConfig, error) {
	defaultMailOff := false
	if err := LoadDotEnv(path); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	} else {
		defaultMailOff = true
	}
	return fromEnv(defaultMailOff), nil
}

func FromEnv() *types.EnvConfig {
	return fromEnv(false)
}

func fromEnv(defaultMailOff bool) *types.EnvConfig {
	prod := envBool("PROD", true)
	config := &types.EnvConfig{
		Port:                 os.Getenv("PORT"),
		Prod:                 prod,
		Host:                 os.Getenv("HOST"),
		LocalExternal:        os.Getenv("LOCAL_EXTERNAL"),
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		MailerSecret:         os.Getenv("MAILER_SECRET"),
		MailEndpoint:         os.Getenv("MAILER_ENDPOINT"),
		DevEmailOverride:     os.Getenv("DEV_EMAIL_OVERRIDE"),
		MailOff:              envBool("MAILER_OFF", defaultMailOff),
		MailerJob:            envInt("MAILER_JOB_SEC", 60),
		MailerJobEnabled:     envBool("MAILER_JOB_ENABLED", !defaultMailOff),
		StripeKey:            os.Getenv("STRIPE_KEY"),
		StripeEndpointSec:    os.Getenv("STRIPE_END_SECRET"),
		RegistryPin:          os.Getenv("REGISTRY_PIN"),
		LogFile:              os.Getenv("LOG_FILE"),
		BufferAPI:            os.Getenv("BUFFER_KEY"),
		CacheTTLSec:          envInt("CACHE_TTL_SEC", 0),
		TaxFormEncryptionKey: os.Getenv("TAX_FORM_ENCRYPTION_KEY"),
		OpenNode: types.OpenNodeConfig{
			Key:      os.Getenv("OPENNODE_KEY"),
			Endpoint: os.Getenv("OPENNODE_ENDPOINT"),
		},
		Easyship: types.EasyshipConfig{
			APIKey:        os.Getenv("EASYSHIP_API_KEY"),
			Endpoint:      firstNonEmpty(os.Getenv("EASYSHIP_ENDPOINT"), "https://public-api.easyship.com"),
			APIVersion:    firstNonEmpty(os.Getenv("EASYSHIP_API_VERSION"), "2024-09"),
			WebhookSecret: os.Getenv("EASYSHIP_WEBHOOK_SECRET"),
		},
		Spaces: types.SpacesConfig{
			Endpoint: os.Getenv("SPACES_ENDPOINT"),
			Region:   os.Getenv("SPACES_REGION"),
			Bucket:   os.Getenv("SPACES_BUCKET"),
			Key:      os.Getenv("SPACES_KEY"),
			Secret:   os.Getenv("SPACES_SECRET"),
		},
		YouTube: types.YouTubeConfig{
			ClientID:       os.Getenv("YOUTUBE_CLIENT_ID"),
			ClientSecret:   os.Getenv("YOUTUBE_CLIENT_SECRET"),
			RedirectURL:    os.Getenv("YOUTUBE_REDIRECT_URL"),
			UpdatesEnabled: envBool("YOUTUBE_UPDATES_ENABLED", prod),
		},
		OAuth: types.OAuthConfig{
			GitHub: types.OAuthProviderConfig{
				ClientID:     os.Getenv("AUTH_GITHUB_CLIENT_ID"),
				ClientSecret: os.Getenv("AUTH_GITHUB_CLIENT_SECRET"),
			},
			Discord: types.OAuthProviderConfig{
				ClientID:     os.Getenv("AUTH_DISCORD_CLIENT_ID"),
				ClientSecret: os.Getenv("AUTH_DISCORD_CLIENT_SECRET"),
			},
			GitLab: types.OAuthProviderConfig{
				ClientID:     os.Getenv("AUTH_GITLAB_CLIENT_ID"),
				ClientSecret: os.Getenv("AUTH_GITLAB_CLIENT_SECRET"),
			},
			MLH: types.OAuthProviderConfig{
				ClientID:     os.Getenv("AUTH_MLH_CLIENT_ID"),
				ClientSecret: os.Getenv("AUTH_MLH_CLIENT_SECRET"),
			},
		},
		Recordings: types.RecordingsConfig{
			AutopublishEnabled: envBool("RECORDINGS_AUTOPUBLISH_ENABLED", false),
			PollSec:            envInt("RECORDINGS_AUTOPUBLISH_POLL_SEC", 0),
			NotifyEmail:        os.Getenv("RECORDINGS_NOTIFY_EMAIL"),
			EncryptionKey:      os.Getenv("SOCIAL_STATE_KEY"),
			YouTubeTokenObject: os.Getenv("YOUTUBE_TOKEN_OBJECT"),
			X: types.XStudioConfig{
				Enabled:   envBool("X_STUDIO_ENABLED", false),
				Cookie:    os.Getenv("X_STUDIO_COOKIE"),
				UserAgent: os.Getenv("X_STUDIO_USER_AGENT"),
				IngestID:  os.Getenv("X_STUDIO_INGEST_ID"),
			},
		},
	}
	config.ApplyDefaults()
	return config
}

func envBool(name string, fallback bool) bool {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}

func envInt(name string, fallback int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
