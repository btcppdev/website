package main

import (
	"strings"
	"testing"

	"btcpp-web/internal/types"
)

func TestValidateDevLoginEnvIgnoresUnusedIntegrations(t *testing.T) {
	env := &types.EnvConfig{
		Prod:        false,
		Host:        "localhost",
		DatabaseURL: "postgres://localhost/dev",
		Recordings: types.RecordingsConfig{
			X: types.XStudioConfig{Enabled: true},
		},
	}
	if err := validateDevLoginEnv(env); err != nil {
		t.Fatalf("unused X Studio config blocked dev login: %v", err)
	}
}

func TestValidateDevLoginEnvRejectsUnsafeOrIncompleteConfig(t *testing.T) {
	tests := []struct {
		name string
		env  *types.EnvConfig
		want string
	}{
		{name: "nil", want: "nil environment"},
		{name: "production", env: &types.EnvConfig{Prod: true}, want: "PROD=true"},
		{name: "database", env: &types.EnvConfig{Host: "localhost"}, want: "DATABASE_URL"},
		{name: "host", env: &types.EnvConfig{DatabaseURL: "postgres://localhost/dev"}, want: "HOST"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateDevLoginEnv(test.env)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateDevLoginEnv() = %v, want error containing %q", err, test.want)
			}
		})
	}
}
