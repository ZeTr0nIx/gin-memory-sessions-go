// Package sessions contains Classes needed to integrate a memory based session storage for https://github.com/gin-gonic/gin.
// It uses a cookie to store the session name which is used to retrieve the session from the store
package sessions_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/zetr0nix/gin-memory-sessions-go/sessions"
)

func TestNewSessionManager(t *testing.T) {
	type args struct {
		opts []sessions.Option
	}

	tests := []struct {
		name string
		args args
	}{
		{
			name: "test default new session manager",
			args: args{make([]sessions.Option, 0)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sessions.NewSessionManager()
			assert.NotNil(t, got)
		})
	}
}
