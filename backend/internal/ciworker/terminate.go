package ciworker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	instance "github.com/scaleway/scaleway-sdk-go/api/instance/v1"
	"github.com/scaleway/scaleway-sdk-go/scw"
)

// metadataBase is Scaleway's instance metadata service (/conf). Not the
// AWS-style 169.254.169.254 — that address answers nothing on Scaleway (the
// first live run never resolved its own id). A var so tests can point it at
// an httptest server.
var metadataBase = "http://169.254.42.42"

// OwnInstanceID resolves the worker's own instance id — from the metadata
// service on a real ephemeral CI box, or SCW_INSTANCE_ID when the metadata
// service is unreachable (local tests).
func OwnInstanceID(ctx context.Context) (string, error) {
	if id := strings.TrimSpace(os.Getenv("SCW_INSTANCE_ID")); id != "" {
		return id, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataBase+"/conf?format=json", nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("instance metadata unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("instance metadata: HTTP %d", resp.StatusCode)
	}
	var conf struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&conf); err != nil {
		return "", fmt.Errorf("instance metadata: %w", err)
	}
	id := strings.TrimSpace(conf.ID)
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

	// One server-side action: power off, delete the server AND its volumes.
	// A client-side poweroff → poll → delete sequence cannot work from inside
	// the instance being powered off (this process dies with the box before
	// it can issue the delete, stranding a billed volume).
	_, err = api.ServerAction(&instance.ServerActionRequest{
		Zone:     z,
		ServerID: instanceID,
		Action:   instance.ServerActionTerminate,
	}, scw.WithContext(ctx))
	return err
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
