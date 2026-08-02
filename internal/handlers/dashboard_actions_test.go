package handlers

import (
	"testing"

	"btcpp-web/internal/auth"
)

func TestCanSearchSpeakers(t *testing.T) {
	tests := []struct {
		name  string
		roles []auth.Role
		want  bool
	}{
		{name: "signed out"},
		{name: "roleless", roles: []auth.Role{}},
		{name: "conference staff", roles: []auth.Role{{Scope: "toronto", Name: auth.RoleStaff}}},
		{name: "global volunteer coordinator", roles: []auth.Role{{Scope: auth.GlobalScope, Name: auth.RoleVolcoord}}},
		{name: "conference admin", roles: []auth.Role{{Scope: "toronto", Name: auth.RoleAdmin}}, want: true},
		{name: "global admin", roles: []auth.Role{{Scope: auth.GlobalScope, Name: auth.RoleAdmin}}, want: true},
		{name: "conference hackathon manager", roles: []auth.Role{{Scope: "toronto", Name: auth.RoleHackathon}}, want: true},
		{name: "global hackathon manager", roles: []auth.Role{{Scope: auth.GlobalScope, Name: auth.RoleHackathon}}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var id *auth.Identity
			if tt.roles != nil {
				id = &auth.Identity{Roles: tt.roles}
			}
			if got := canSearchSpeakers(id); got != tt.want {
				t.Fatalf("canSearchSpeakers() = %v, want %v", got, tt.want)
			}
		})
	}
}
