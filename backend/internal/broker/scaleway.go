package broker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	block "github.com/scaleway/scaleway-sdk-go/api/block/v1"
	instance "github.com/scaleway/scaleway-sdk-go/api/instance/v1"
	"github.com/scaleway/scaleway-sdk-go/scw"

	"github.com/nielsuitterdijk22/quill/internal/workertoken"
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
	// CloudInitFor renders an instance's user-data around its own worker
	// token (minted per instance with TokenSecret).
	CloudInitFor func(workerToken string) string
	// TokenSecret signs worker tokens (the dispatch secret: the api verifies).
	TokenSecret string
	// MaxInstances caps concurrently alive CI instances (default 3).
	MaxInstances int
	// BootGrace: an instance younger than this has probably not claimed (and
	// bound) yet, so it still counts toward the repos that need a worker.
	BootGrace time.Duration
}

// ScalewayOrchestrator manages the ephemeral CI instance through the Scaleway
// Instance API. It relies on the caller's SCW_* credentials (an IAM key scoped
// to this orchestration, provisioned by terraform).
type ScalewayOrchestrator struct {
	cfg    ScalewayConfig
	logger *slog.Logger
	client *scw.Client
	api    *instance.API
	block  *block.API
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
	if cfg.CloudInitFor == nil || cfg.TokenSecret == "" {
		return nil, fmt.Errorf("cloud-init renderer and token secret are required")
	}
	if cfg.MaxInstances <= 0 {
		cfg.MaxInstances = 3
	}
	if cfg.BootGrace == 0 {
		cfg.BootGrace = 6 * time.Minute
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
		api: instance.NewAPI(client), block: block.NewAPI(client), zone: zone,
	}, nil
}

// scan lists CI instances, TTL-kills stuck ones and reaps stopped ones; it
// returns the instances that are running or starting.
func (o *ScalewayOrchestrator) scan(ctx context.Context) ([]*instance.Server, error) {
	o.reapOrphanVolumes(ctx)
	resp, err := o.api.ListServers(&instance.ListServersRequest{
		Zone: o.zone, Tags: []string{o.cfg.InstanceTag},
	}, scw.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("list ci instances: %w", err)
	}
	var alive []*instance.Server
	for i := range resp.Servers {
		srv := resp.Servers[i]
		switch srv.State {
		case instance.ServerStateRunning, instance.ServerStateStarting:
			// TTL kill: a worker alive past its TTL is stuck (wedged act job,
			// lost retire) — terminate it. Cost safety beats job safety.
			if time.Since(created(srv)) > o.cfg.TTL {
				o.logger.Warn("TTL-killing stuck CI instance", "id", srv.ID, "age", time.Since(created(srv)).Round(time.Minute))
				// terminate = power off + delete server, server-side; a bare
				// poweroff would leave a billed volume.
				if _, err := o.api.ServerAction(&instance.ServerActionRequest{
					Zone: o.zone, ServerID: srv.ID, Action: instance.ServerActionTerminate,
				}, scw.WithContext(ctx)); err != nil {
					o.logger.Warn("terminate stuck CI instance", "id", srv.ID, "error", err)
				}
				continue
			}
			alive = append(alive, srv)
		case instance.ServerStateStopped, instance.ServerStateStoppedInPlace:
			// A worker that halted without being retired: reap it with its
			// volumes so nothing keeps billing.
			o.reapStopped(ctx, srv)
		}
	}
	return alive, nil
}

func created(srv *instance.Server) time.Time {
	if srv.CreationDate != nil {
		return *srv.CreationDate
	}
	return time.Time{}
}

// EnsureWorker implements Orchestrator (count-less wake from an older api):
// with no running/starting CI instance it boots one.
func (o *ScalewayOrchestrator) EnsureWorker(ctx context.Context) (string, bool, error) {
	alive, err := o.scan(ctx)
	if err != nil {
		return "", false, err
	}
	if len(alive) > 0 {
		return alive[0].ID, false, nil
	}
	id, err := o.createInstance(ctx)
	return id, err == nil, err
}

// EnsureWorkers implements IsolatingOrchestrator: one instance per repo that
// waits for a worker, minus instances still booting (they will bind to one
// of those repos), capped at MaxInstances alive.
func (o *ScalewayOrchestrator) EnsureWorkers(ctx context.Context, need int) ([]string, error) {
	alive, err := o.scan(ctx)
	if err != nil {
		return nil, err
	}
	booting := 0
	for _, srv := range alive {
		if time.Since(created(srv)) < o.cfg.BootGrace {
			booting++
		}
	}
	n := need - booting
	if room := o.cfg.MaxInstances - len(alive); n > room {
		n = room
	}
	var ids []string
	for i := 0; i < n; i++ {
		id, err := o.createInstance(ctx)
		if err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Retire implements IsolatingOrchestrator: terminates one CI instance —
// refusing anything that does not carry the CI tag.
func (o *ScalewayOrchestrator) Retire(ctx context.Context, instanceID string) error {
	got, err := o.api.GetServer(&instance.GetServerRequest{Zone: o.zone, ServerID: instanceID}, scw.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("get instance: %w", err)
	}
	tagged := false
	for _, t := range got.Server.Tags {
		tagged = tagged || t == o.cfg.InstanceTag
	}
	if !tagged {
		return fmt.Errorf("instance %s is not a CI instance", instanceID)
	}
	if _, err := o.api.ServerAction(&instance.ServerActionRequest{
		Zone: o.zone, ServerID: instanceID, Action: instance.ServerActionTerminate,
	}, scw.WithContext(ctx)); err != nil {
		return fmt.Errorf("terminate: %w", err)
	}
	return nil
}

// createInstance boots one CI instance with its own worker token.
func (o *ScalewayOrchestrator) createInstance(ctx context.Context) (string, error) {
	imageID, err := o.marketplaceImage(ctx)
	if err != nil {
		return "", err
	}
	createdSrv, err := o.api.CreateServer(&instance.CreateServerRequest{
		Zone:              o.zone,
		Name:              fmt.Sprintf("%s-%x", o.cfg.InstanceTag, time.Now().UnixNano()>>25),
		CommercialType:    o.cfg.CommercialType,
		Image:             scw.StringPtr(imageID),
		Project:           &o.cfg.ProjectID,
		Tags:              []string{o.cfg.InstanceTag},
		DynamicIPRequired: scw.BoolPtr(true),
	}, scw.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("create ci instance: %w", err)
	}
	id := createdSrv.Server.ID
	// Tag the SBS root volume: the server-side terminate action deletes the
	// server but NOT its SBS volumes, so the reaper finds orphans by this tag.
	for _, v := range createdSrv.Server.Volumes {
		if v != nil && v.VolumeType == instance.VolumeServerVolumeTypeSbsVolume {
			tags := []string{o.cfg.InstanceTag}
			if _, err := o.block.UpdateVolume(&block.UpdateVolumeRequest{
				Zone: o.zone, VolumeID: v.ID, Tags: &tags,
			}, scw.WithContext(ctx)); err != nil {
				o.logger.Warn("tag CI root volume", "volume", v.ID, "error", err)
			}
		}
	}
	token, err := workertoken.Mint(o.cfg.TokenSecret, workertoken.Claims{
		Worker:   "ciw-" + id,
		Instance: id,
		Expires:  time.Now().Add(o.cfg.TTL + 30*time.Minute).Unix(),
	})
	if err != nil {
		return "", err
	}
	// cloud-init travels as user data (this SDK generation dropped the
	// CreateServerRequest.CloudInit field): set before power-on, key is
	// literally "cloud-init".
	if err := o.api.SetServerUserData(&instance.SetServerUserDataRequest{
		Zone:     o.zone,
		ServerID: id,
		Key:      "cloud-init",
		Content:  strings.NewReader(o.cfg.CloudInitFor(token)),
	}, scw.WithContext(ctx)); err != nil {
		return "", fmt.Errorf("set cloud-init: %w", err)
	}
	if _, err := o.api.ServerAction(&instance.ServerActionRequest{
		Zone: o.zone, ServerID: id, Action: instance.ServerActionPoweron,
	}, scw.WithContext(ctx)); err != nil {
		return "", fmt.Errorf("power on ci instance: %w", err)
	}
	o.logger.Info("booted ephemeral CI instance", "id", id, "type", o.cfg.CommercialType)
	return id, nil
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

// reapStopped deletes a stopped CI instance and then its volumes (DeleteServer
// alone leaves volumes behind; the terminate action requires a running server).
func (o *ScalewayOrchestrator) reapStopped(ctx context.Context, srv *instance.Server) {
	if srv.State == instance.ServerStateStoppedInPlace {
		// stopped-in-place still holds the hypervisor slot: power off fully
		// first; the next wake finishes the reap once it reports stopped.
		_, _ = o.api.ServerAction(&instance.ServerActionRequest{
			Zone: o.zone, ServerID: srv.ID, Action: instance.ServerActionPoweroff,
		}, scw.WithContext(ctx))
		return
	}
	if err := o.api.DeleteServer(&instance.DeleteServerRequest{Zone: o.zone, ServerID: srv.ID}, scw.WithContext(ctx)); err != nil {
		o.logger.Warn("reap stopped CI instance", "id", srv.ID, "error", err)
		return
	}
	for _, v := range srv.Volumes {
		if v == nil {
			continue
		}
		var err error
		if v.VolumeType == instance.VolumeServerVolumeTypeSbsVolume {
			// SBS root volumes (the DEV1-S default) live in the Block API.
			err = o.block.DeleteVolume(&block.DeleteVolumeRequest{Zone: o.zone, VolumeID: v.ID}, scw.WithContext(ctx))
		} else {
			err = o.api.DeleteVolume(&instance.DeleteVolumeRequest{Zone: o.zone, VolumeID: v.ID}, scw.WithContext(ctx))
		}
		if err != nil {
			o.logger.Warn("reap CI volume", "volume", v.ID, "error", err)
		}
	}
	o.logger.Info("reaped stopped CI instance", "id", srv.ID)
}

// reapOrphanVolumes deletes detached SBS volumes carrying the CI tag — the
// leftovers of workers that terminated themselves (terminate keeps SBS).
func (o *ScalewayOrchestrator) reapOrphanVolumes(ctx context.Context) {
	resp, err := o.block.ListVolumes(&block.ListVolumesRequest{
		Zone: o.zone, ProjectID: &o.cfg.ProjectID, Tags: []string{o.cfg.InstanceTag},
	}, scw.WithAllPages(), scw.WithContext(ctx))
	if err != nil {
		o.logger.Warn("list CI volumes", "error", err)
		return
	}
	for _, v := range resp.Volumes {
		if v.Status != block.VolumeStatusAvailable || len(v.References) > 0 {
			continue // attached or transitioning
		}
		if err := o.block.DeleteVolume(&block.DeleteVolumeRequest{Zone: o.zone, VolumeID: v.ID}, scw.WithContext(ctx)); err != nil {
			o.logger.Warn("reap CI volume", "volume", v.ID, "error", err)
			continue
		}
		o.logger.Info("reaped orphan CI volume", "volume", v.ID)
	}
}
