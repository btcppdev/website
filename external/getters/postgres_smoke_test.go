package getters

import (
	"testing"

	"btcpp-web/internal/config"
)

func postgresSmokeContext(t *testing.T) *config.AppContext {
	return databaseSmokeContext(t)
}

func postgresSmokeSuffix() string {
	return databaseSmokeSuffix()
}
