package broker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	instance "github.com/scaleway/scaleway-sdk-go/api/instance/v1"
	"github.com/scaleway/scaleway-sdk-go/scw"
)

// ScalewayConfig for the ephemeral CI instance.
type ScalewayConfig struct {
	Zone        string // e.g. fr-par-1
	ProjectID   string
	InstanceTag string // tag identifying CI workers (e.g. quill-ci-worker)
	// CommercialType is the instance type (default DEV1-S: enough for act's
	// job containers at dogfooding scale; the VM-wedge lesson said never share
	// the box — this one is CI's alone and dies with the queue).
	CommercialType string
	// Image is the base image label, resolved against the public marketplace
	// list at boot time (default ubuntu_noble).
	Image string
	// TTL bounds a worker's life even if ciworker's self-termination fails —
	// the wedge lesson: a wedged CI box must not cost money forever.
	TTL time.Duration
	// CloudInit is the rendered user-data script that installs Docker and
	// runs the ciworker container (built by Config.CloudInitFor).
	CloudInit string
}

// ScalewayOrchestrator manages the ephemeral CI instance through the Scaleway
// Instance API. It relies on the caller's SCW_* credentials (an IAM key scoped
// to this orchestration, provisioned by terraform).
type ScalewayOrchestrator struct {
	cfg    ScalewayConfig
	logger *slog.Logger
	client *scw.Client
	api    *instance.API
	zone   scw.Zone
}

// NewScalewayOrchestrator connects to the Scaleway Instance API.
func NewScalewayOrchestrator(cfg ScalewayConfig, logger *slog.Logger) (*ScalewayOrchestrator, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.CommercialType == "" {
		cfg.CommercialType = "DEV1-S"
	}
	if cfg.Image == "" {
		cfg.Image = "ubuntu_noble"
	}
	if cfg.TTL == 0 {
		cfg.TTL = 3 * time.Hour
	}
	if cfg.InstanceTag == "" {
		return nil, fmt.Errorf("instance tag is required")
	}
	if cfg.CloudInit == "" {
		return nil, fmt.Errorf("cloud-init is required")
	}
	client, err := scw.NewClient(scw.WithEnv())
	if err != nil {
		return nil, fmt.Errorf("scaleway client: %w", err)
	}
	zone, err := scw.ParseZone(cfg.Zone)
	if err != nil {
		return nil, fmt.Errorf("zone: %w", err)
	}
	return &ScalewayOrchestrator{
		cfg: cfg, logger: logger, client: client,
		api: instance.NewAPI(client), zone: zone,
	}, nil
}

// EnsureWorker implements Orchestrator: with no running/starting CI instance
// it boots one; it also TTL-kills strays created by a previous broker run
// whose ciworker never self-terminated.
func (o *ScalewayOrchestrator) EnsureWorker(ctx context.Context) (string, bool, error) {
	resp, err := o.api.ListServers(&instance.ListServersRequest{
		Zone: o.zone, Tags: []string{o.cfg.InstanceTag},
	}, scw.WithContext(ctx))
	if err != nil {
		return "", false, fmt.Errorf("list ci instances: %w", err)
	}

	var alive *instance.Server
	for i := range resp.Servers {
		srv := resp.Servers[i]
		switch srv.State {
		case instance.ServerStateRunning, instance.ServerStateStarting:
			// TTL kill: a worker alive past its TTL is stuck (wedged act job,
			// lost self-termination) — power it off and let the create below
			// replace it. Cost safety beats job safety for a CI box.
			created := time.Time{}
			if srv.CreationDate != nil {
				created = *srv.CreationDate
			}
			if time.Since(created) > o.cfg.TTL {
				o.logger.Warn("TTL-killing stuck CI instance", "id", srv.ID, "age", time.Since(created).Round(time.Minute))
				_, _ = o.api.ServerAction(&instance.ServerActionRequest{
					Zone: o.zone, ServerID: srv.ID, Action: instance.ServerActionPoweroff,
				})
				continue
			}
			if alive == nil {
				alive = srv
			}
		}
	}
	if alive != nil {
		return alive.ID, false, nil
	}

	imageID, err := o.marketplaceImage(ctx)
	if err != nil {
		return "", false, err
	}
	created, err := o.api.CreateServer(&instance.CreateServerRequest{
		Zone:              o.zone,
		Name:              fmt.Sprintf("%s-%x", o.cfg.InstanceTag, time.Now().UnixNano()>>25),
		CommercialType:    o.cfg.CommercialType,
		Image:             scw.StringPtr(imageID),
		Project:           &o.cfg.ProjectID,
		Tags:              []string{o.cfg.InstanceTag},
		DynamicIPRequired: scw.BoolPtr(true),
	}, scw.WithContext(ctx))
	if err != nil {
		return "", false, fmt.Errorf("create ci instance: %w", err)
	}
	// cloud-init travels as user data (this SDK generation dropped the
	// CreateServerRequest.CloudInit field): set before power-on, key is
	// literally "cloud-init".
	if err := o.api.SetServerUserData(&instance.SetServerUserDataRequest{
		Zone:     o.zone,
		ServerID: created.Server.ID,
		Key:      "cloud-init",
		Content:  strings.NewReader(o.cfg.CloudInit),
	}, scw.WithContext(ctx)); err != nil {
		return "", false, fmt.Errorf("set cloud-init: %w", err)
	}
	_, err = o.api.ServerAction(&instance.ServerActionRequest{
		Zone: o.zone, ServerID: created.Server.ID, Action: instance.ServerActionPoweron,
	}, scw.WithContext(ctx))
	if err != nil {
		return "", false, fmt.Errorf("power on ci instance: %w", err)
	}
	o.logger.Info("booted ephemeral CI instance", "id", created.Server.ID, "type", o.cfg.CommercialType)
	return created.Server.ID, true, nil
}

// marketplaceImage resolves the configured image label through the public
// marketplace image list (freshest version).
func (o *ScalewayOrchestrator) marketplaceImage(ctx context.Context) (string, error) {
	resp, err := o.api.ListImages(&instance.ListImagesRequest{
		Zone:   o.zone,
		Public: scw.BoolPtr(true),
		Name:   scw.StringPtr("Ubuntu 24.04"),
	},
		scw.WithAllPages(),
		scw.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("list marketplace images: %w", err)
	}
	for _, img := range resp.Images {
		if img.Arch == instance.ArchX86_64 {
			return img.ID, nil
		}
	}
	return "", fmt.Errorf("no marketplace image matching %q found", o.cfg.Image)
}
