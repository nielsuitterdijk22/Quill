package ciworker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	instance "github.com/scaleway/scaleway-sdk-go/api/instance/v1"
	"github.com/scaleway/scaleway-sdk-go/scw"
)

// metadataBase is Scaleway's AWS-compatible instance metadata endpoint.
const metadataBase = "http://169.254.169.254/latest"

// OwnInstanceID resolves the worker's own instance id — from the metadata
// service on a real ephemeral CI box, or SCW_INSTANCE_ID when the metadata
// service is unreachable (local tests).
func OwnInstanceID(ctx context.Context) (string, error) {
	if id := strings.TrimSpace(os.Getenv("SCW_INSTANCE_ID")); id != "" {
		return id, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataBase+"/meta-data/instance-id", nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("instance metadata unreachable: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(b))
	if id == "" {
		return "", fmt.Errorf("instance metadata returned an empty id")
	}
	return id, nil
}

// TerminateInstance self-destructs the ephemeral CI instance via the Scaleway
// API: power off, poll until stopped, delete. Credentials come from the
// standard SCW_* environment injected by the broker's cloud-init; the worker's
// IAM key is scoped to deleting CI-tagged servers only.
func TerminateInstance(ctx context.Context, instanceID, zone string) error {
	client, err := scw.NewClient(
		scw.WithEnv(),
		scw.WithHTTPClient(&http.Client{Timeout: 60 * time.Second}),
	)
	if err != nil {
		return fmt.Errorf("scaleway client: %w", err)
	}
	api := instance.NewAPI(client)
	z, err := scw.ParseZone(zone)
	if err != nil {
		return fmt.Errorf("zone: %w", err)
	}

	// Best-effort power off (the box may already be stopping), then poll to
	// stopped so the delete is accepted.
	_, _ = api.ServerAction(&instance.ServerActionRequest{
		Zone:     z,
		ServerID: instanceID,
		Action:   instance.ServerActionPoweroff,
	})
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		srv, err := api.GetServer(&instance.GetServerRequest{Zone: z, ServerID: instanceID})
		if err != nil {
			break // fall through to delete; the broker TTL-killer is the backstop
		}
		if srv.Server != nil && srv.Server.State == instance.ServerStateStopped {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return api.DeleteServer(&instance.DeleteServerRequest{Zone: z, ServerID: instanceID})
}

// ScalewayTerminator builds the Terminator used by cmd/ciworker: resolves the
// instance id, then self-destructs in the configured zone.
func ScalewayTerminator(zone string) Terminator {
	return func(ctx context.Context) error {
		id, err := OwnInstanceID(ctx)
		if err != nil {
			return err
		}
		return TerminateInstance(ctx, id, zone)
	}
}
