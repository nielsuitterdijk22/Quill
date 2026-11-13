// broker is the CI control plane entrypoint (serverless epic, PR A2): a
// min_scale-0 Serverless Container that receives wake-ups from the api and
// the cron sweeper and ensures an ephemeral CI instance is running.
//
// Environment:
//
//	QUILL_PIPELINE_DISPATCH_SECRET   shared HMAC secret (X-Quill-Dispatch-Signature)
//	QUILL_BROKER_ADDR               listen address (default :8090)
//	QUILL_CIB_ZONE                  Scaleway zone of the ephemeral CI instance (default fr-par-1)
//	QUILL_CIB_PROJECT_ID            Scaleway project the instances live in
//	QUILL_CIB_INSTANCE_TAG          tag identifying CI workers (default quill-ci-worker)
//	QUILL_CIB_INSTANCE_TYPE         commercial type (default DEV1-S)
//	QUILL_CIB_TTL                   max worker lifetime before TTL-kill (default 3h)
//	QUILL_CIB_API_URL               api base URL handed to the ciworker via cloud-init
//	QUILL_CIB_WORKER_IMAGE          registry image of the ciworker container
//	QUILL_CIB_REGISTRY_USER/_PASSWORD  registry pull credentials for cloud-init's docker login
//	SCW_ACCESS_KEY / SCW_SECRET_KEY    IAM key scoped to the CI instance orchestration
//	QUILL_PIPELINE_UBUNTU_IMAGE        act's ubuntu-latest image on the worker (optional)
package main

import (
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nielsuitterdijk22/quill/internal/broker"
)

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	secret := os.Getenv("QUILL_PIPELINE_DISPATCH_SECRET")
	projectID := os.Getenv("QUILL_CIB_PROJECT_ID")
	apiURL := os.Getenv("QUILL_CIB_API_URL")
	workerImage := os.Getenv("QUILL_CIB_WORKER_IMAGE")
	if secret == "" || projectID == "" || apiURL == "" || workerImage == "" {
		logger.Error("broker requires QUILL_PIPELINE_DISPATCH_SECRET, QUILL_CIB_PROJECT_ID, QUILL_CIB_API_URL and QUILL_CIB_WORKER_IMAGE")
		os.Exit(1)
	}
	zone := getenv("QUILL_CIB_ZONE", "fr-par-1")
	tag := getenv("QUILL_CIB_INSTANCE_TAG", "quill-ci-worker")
	ttl, err := time.ParseDuration(getenv("QUILL_CIB_TTL", "3h"))
	if err != nil {
		logger.Error("invalid QUILL_CIB_TTL", "error", err)
		os.Exit(1)
	}

	cloudInit := broker.CloudInit(broker.CloudInitOpts{
		APIURL:           strings.TrimRight(apiURL, "/"),
		DispatchSecret:   secret,
		WorkerImage:      workerImage,
		Zone:             zone,
		InstanceTag:      tag,
		RegistryUser:     os.Getenv("QUILL_CIB_REGISTRY_USER"),
		RegistryPassword: os.Getenv("QUILL_CIB_REGISTRY_PASSWORD"),
		// The worker self-terminates with the broker's own instance key
		// (Scaleway IAM cannot scope a key to tagged servers, so a separate
		// worker key would carry the same rights).
		SCWAccessKey: os.Getenv("SCW_ACCESS_KEY"),
		SCWSecretKey: os.Getenv("SCW_SECRET_KEY"),
		SCWProjectID: projectID,
		UbuntuImage:  os.Getenv("QUILL_PIPELINE_UBUNTU_IMAGE"),
	})

	orch, err := broker.NewScalewayOrchestrator(broker.ScalewayConfig{
		Zone:           zone,
		ProjectID:      projectID,
		InstanceTag:    tag,
		CommercialType: getenv("QUILL_CIB_INSTANCE_TYPE", "DEV1-S"),
		TTL:            ttl,
		CloudInit:      cloudInit,
	}, logger)
	if err != nil {
		logger.Error("orchestrator init failed", "error", err)
		os.Exit(1)
	}

	srv := broker.NewServer(broker.Config{Secret: secret, Orchestrator: orch, Logger: logger})
	addr := getenv("QUILL_BROKER_ADDR", ":8090")
	logger.Info("broker listening", "addr", addr)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		logger.Error("broker stopped", "error", err)
		os.Exit(1)
	}
}
