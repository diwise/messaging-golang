package main

import (
	"context"
	"os"
	"testing"

	"log/slog"

	"github.com/diwise/messaging-golang/pkg/messaging"
)

func TestLoadMockConfigAndInitialize(t *testing.T) {
	os.Setenv("RABBITMQ_DISABLED", "true")

	ctx := context.Background()
	logger := slog.Default()
	conf := messaging.LoadConfiguration(ctx, "messaging-golang-test", logger)

	_, err := messaging.Initialize(ctx, conf)
	if err != nil {
		t.Error(err)
	}

}
