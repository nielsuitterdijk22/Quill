// ciworker is the CI execution worker entrypoint (serverless epic, PR A3).
// It runs on the ephemeral CI instance started by the broker, drains the
// pipeline queue through the api's internal dispatch endpoints, executes jobs
// with the unchanged nektos/act runner against the instance's Docker daemon,
// and self-terminates the instance when the queue stays dry.
//
// Environment:
//
//	QUILL_API_URL                     api base URL (https://api.<host>)
//	QUILL_PIPELINE_DISPATCH_SECRET    shared HMAC secret for the dispatch endpoints
//	QUILL_CIW_ZONE                    Scaleway zone of the ephemeral instance
//	QUILL_CIW_WORKER_ID               worker identity (default: ciworker-<hostname>)
//	QUILL_CIW_IDLE_EXIT               dry-queue time before retiring (default 2m)
//	QUILL_CIW_SELF_TERMINATE          "true"/"1": delete own Scaleway instance on exit
//	SCW_ACCESS_KEY / SCW_SECRET_KEY   (when self-terminating) scoped worker IAM key
//	SCW_INSTANCE_ID                   instance id override when metadata is unreachable
//
// The act runner reads its own env as before: QUILL_PIPELINE_UBUNTU_IMAGE,
// QUILL_PIPELINE_TIMEOUT, QUILL_PIPELINE_ACTION_CACHE (pin under /mnt — that
// volume is disk-backed; /tmp on serverless runtimes is RAM).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/nielsuitterdijk22/quill/internal/ciworker"
	"github.com/nielsuitterdijk22/quill/internal/pipeline"
)

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getboolEnv(key string) bool {
	switch getenv(key, "") {
	case "1", "t", "true", "yes", "on":
		return true
	}
	return false
}

func main() {
	apiURL := getenv("QUILL_API_URL", "")
	secret := os.Getenv("QUILL_PIPELINE_DISPATCH_SECRET")
	if apiURL == "" || secret == "" {
		fatal("QUILL_API_URL and QUILL_PIPELINE_DISPATCH_SECRET are required")
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	workerID := getenv("QUILL_CIW_WORKER_ID", "ciworker-"+mustHostname())
	idleExit, err := time.ParseDuration(getenv("QUILL_CIW_IDLE_EXIT", "2m"))
	if err != nil {
		fatal("invalid QUILL_CIW_IDLE_EXIT: %v", err)
	}

	var terminate ciworker.Terminator
	if getboolEnv("QUILL_CIW_SELF_TERMINATE") {
		terminate = ciworker.ScalewayTerminator(getenv("QUILL_CIW_ZONE", "fr-par-1"))
	}

	client := ciworker.NewClient(apiURL, secret)
	runner := pipeline.NewActRunner()
	logger.Info("ciworker draining queue", "worker", workerID, "idle_exit", idleExit)
	processed, err := ciworker.Drain(context.Background(), client, workerID, runner, idleExit, terminate,
		func(msg string, kv ...any) { logger.Info(msg, kv...) })
	if err != nil {
		fatal("drain: %v", err)
	}
	logger.Info("drain complete", "processed", processed)
}

func mustHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ciworker: "+format+"\n", args...)
	os.Exit(1)
}
