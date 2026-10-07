//go:build unix

package main

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestConfigurationHubContextCancelsOnHangup(t *testing.T) {
	ctx, stop := configurationHubContext(context.Background())
	defer stop()

	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("context was not cancelled by SIGHUP")
	}
}
