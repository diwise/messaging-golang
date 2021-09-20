package main

import (
	"os"
	"testing"

	"github.com/rs/zerolog/log"

	"github.com/diwise/messaging-golang/pkg/messaging"
)

func TestLoadMockConfigAndInitialize(t *testing.T) {
	os.Setenv("RABBITMQ_DISABLED", "true")

	logger := log.With().Logger()
	conf := messaging.LoadConfiguration("messaging-golang-test", logger)

	_, err := messaging.Initialize(conf)
	if err != nil {
		t.Error(err)
	}

}
