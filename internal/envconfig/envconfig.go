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
	config := &types.EnvConfig{
		Port:              os.Getenv("PORT"),
		Prod:              envBool("PROD", true),
		Host:              os.Getenv("HOST"),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		MailerSecret:      os.Getenv("MAILER_SECRET"),
		MailEndpoint:      os.Getenv("MAILER_ENDPOINT"),
		MailOff:           envBool("MAILER_OFF", defaultMailOff),
		MailerJob:         envInt("MAILER_JOB_SEC", 60),
		StripeKey:         os.Getenv("STRIPE_KEY"),
		StripeEndpointSec: os.Getenv("STRIPE_END_SECRET"),
		RegistryPin:       os.Getenv("REGISTRY_PIN"),
		LogFile:           os.Getenv("LOG_FILE"),
		BufferAPI:         os.Getenv("BUFFER_KEY"),
		CacheTTLSec:       envInt("CACHE_TTL_SEC", 0),
		OpenNode: types.OpenNodeConfig{
			Key:      os.Getenv("OPENNODE_KEY"),
			Endpoint: os.Getenv("OPENNODE_ENDPOINT"),
		},
		Spaces: types.SpacesConfig{
			Endpoint: os.Getenv("SPACES_ENDPOINT"),
			Region:   os.Getenv("SPACES_REGION"),
			Bucket:   os.Getenv("SPACES_BUCKET"),
			Key:      os.Getenv("SPACES_KEY"),
			Secret:   os.Getenv("SPACES_SECRET"),
		},
		YouTube: types.YouTubeConfig{
			ClientID:     os.Getenv("YOUTUBE_CLIENT_ID"),
			ClientSecret: os.Getenv("YOUTUBE_CLIENT_SECRET"),
			RedirectURL:  os.Getenv("YOUTUBE_REDIRECT_URL"),
		},
		Recordings: types.RecordingsConfig{
			AutopublishEnabled: envBool("RECORDINGS_AUTOPUBLISH_ENABLED", false),
			PollSec:            envInt("RECORDINGS_AUTOPUBLISH_POLL_SEC", 0),
			NotifyEmail:        os.Getenv("RECORDINGS_NOTIFY_EMAIL"),
			EncryptionKey:      firstNonEmpty(os.Getenv("SOCIAL_STATE_KEY"), os.Getenv("X_PROFILE_ARCHIVE_KEY")),
			YouTubeTokenObject: os.Getenv("YOUTUBE_TOKEN_OBJECT"),
			X: types.XUploaderConfig{
				Enabled:        envBool("X_UPLOADER_ENABLED", false),
				ProfileObject:  os.Getenv("X_PROFILE_ARCHIVE_OBJECT"),
				Headed:         envBool("X_BROWSER_HEADED", false),
				LoginUsername:  os.Getenv("X_LOGIN_USERNAME"),
				LoginPassword:  os.Getenv("X_LOGIN_PASSWORD"),
				PostTimeoutSec: envInt("X_POST_TIMEOUT_SEC", 0),
				AuthWaitSec:    envInt("X_AUTH_WAIT_SEC", 0),
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
