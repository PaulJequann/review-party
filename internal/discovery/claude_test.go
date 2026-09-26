package discovery

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestClaudeLoggedInRequiresManualModelEntry(t *testing.T) {
	runner := &scriptedRunner{runs: map[string]RunResult{
		"auth status --json": {Stdout: []byte(`{"loggedIn":true,"authMethod":"claude.ai"}`)},
		"--version":          {Stdout: []byte("2.1.283 (Claude Code)\n")},
	}}
	result := NewService(Options{Adapters: []Adapter{NewClaudeAdapter(runner)}}).Discover(context.Background(), "claude")
	requireDiscoveryStatus(t, result, StatusUnsupported, AuthConfigured)
	if result.HarnessVersion != "2.1.283" || len(result.Models) != 0 {
		t.Fatalf("result = %#v", result)
	}
	if !reflect.DeepEqual(runner.args[0], []string{"claude", "auth", "status", "--json"}) {
		t.Fatalf("command = %#v", runner.args)
	}
}

func TestClaudeLoggedOutRequiresSignIn(t *testing.T) {
	runner := &scriptedRunner{runs: map[string]RunResult{
		"auth status --json": {Err: errors.New("exit status 1"), Stdout: []byte(`{"loggedIn":false,"authMethod":"none"}`)},
	}}
	result := NewService(Options{Adapters: []Adapter{NewClaudeAdapter(runner)}}).Discover(context.Background(), "claude")
	requireDiscoveryStatus(t, result, StatusAuthenticationRequired, AuthRequired)
	if result.Authentication.SignIn == nil || !reflect.DeepEqual(result.Authentication.SignIn.Command, []string{"claude", "auth", "login"}) {
		t.Fatalf("sign-in = %#v", result.Authentication.SignIn)
	}
}

func TestClaudeUnrecognizedAuthStatusIsNotConfigured(t *testing.T) {
	runner := &scriptedRunner{runs: map[string]RunResult{
		"auth status --json": {Stdout: []byte("Logged in as someone")},
	}}
	result := NewService(Options{Adapters: []Adapter{NewClaudeAdapter(runner)}}).Discover(context.Background(), "claude")
	requireDiscoveryStatus(t, result, StatusUnsupported, AuthUnknown)
}

func TestClaudeFailedAuthStatusIsUnavailable(t *testing.T) {
	runner := &scriptedRunner{runs: map[string]RunResult{
		"auth status --json": {Err: errors.New("executable file not found")},
	}}
	result := NewService(Options{Adapters: []Adapter{NewClaudeAdapter(runner)}}).Discover(context.Background(), "claude")
	requireDiscoveryStatus(t, result, StatusUnavailable, AuthUnavailable)
}
