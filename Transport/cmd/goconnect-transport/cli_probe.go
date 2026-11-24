package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"
)

// Fixed, unauthenticated HEAD requests. A vendor's HTTP error is still a
// completed TLS exchange, and the status is returned separately to the UI.
func cliProbeURL(host string) string {
	switch host {
	case "cloudcode-pa.googleapis.com", "daily-cloudcode-pa.sandbox.googleapis.com":
		return "https://" + host + "/v1internal:loadCodeAssist"
	case "chatgpt.com":
		return "https://" + host + "/backend-api/codex/models"
	case "api.openai.com", "api.anthropic.com":
		return "https://" + host + "/v1/models"
	default:
		return ""
	}
}

func cliProbeCommand(ctx context.Context) error {
	var request struct {
		Port  uint16
		Token string
		Host  string
	}
	if err := json.NewDecoder(io.LimitReader(os.Stdin, 4096)).Decode(&request); err != nil {
		return err
	}
	url := cliProbeURL(request.Host)
	if url == "" {
		return errors.New("unknown CLI service")
	}
	dial, err := sessionSOCKSDialer(request.Port, request.Token)
	if err != nil {
		return err
	}
	client := newPathProbeClient(dial)
	client.Timeout = 10 * time.Second
	defer client.CloseIdleConnections()
	result := checkProbeEndpoint(ctx, client, pathProbeEndpoint{url: url, provider: request.Host}, false)
	emit(result)
	return nil
}
