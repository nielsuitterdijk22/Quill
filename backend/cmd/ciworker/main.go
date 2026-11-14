// ciworker is the CI execution worker entrypoint (serverless epic, PR A3).
// It runs on the ephemeral CI instance started by the broker, drains the
// pipeline queue through the api's internal dispatch endpoints, executes jobs
// with the unchanged nektos/act runner against the instance's Docker daemon,
// and self-terminates the instance when the queue stays dry.
//
// Environment:
//
//	QUILL_API_URL                     api base URL (https://api.<host>)
//	QUILL_CIW_TOKEN                   this instance's worker token (isolated CI,
//	                                  2026-11-14): worker id comes from it, the api
//	                                  binds the worker to one repo, retire goes
//	                                  through the api — no secret or cloud key here
//	QUILL_PIPELINE_DISPATCH_SECRET    legacy: shared HMAC secret (local/dev workers)
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
	"github.com/nielsuitterdijk22/quill/internal/workertoken"
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
	token := os.Getenv("QUILL_CIW_TOKEN")
	secret := os.Getenv("QUILL_PIPELINE_DISPATCH_SECRET")
	if apiURL == "" || (token == "" && secret == "") {
		fatal("QUILL_API_URL and QUILL_CIW_TOKEN (or legacy QUILL_PIPELINE_DISPATCH_SECRET) are required")
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	workerID := getenv("QUILL_CIW_WORKER_ID", "ciworker-"+mustHostname())
	client := ciworker.NewClient(apiURL, secret)
	if token != "" {
		claims, err := workertoken.Peek(token)
		if err != nil {
			fatal("QUILL_CIW_TOKEN is not a worker token")
		}
		workerID = claims.Worker // the api ignores any other id for a token
		client = ciworker.NewTokenClient(apiURL, token)
	}
	idleExit, err := time.ParseDuration(getenv("QUILL_CIW_IDLE_EXIT", "2m"))
	if err != nil {
		fatal("invalid QUILL_CIW_IDLE_EXIT: %v", err)
	}

	var terminate ciworker.Terminator
	if getboolEnv("QUILL_CIW_SELF_TERMINATE") {
		if token != "" {
			terminate = ciworker.APIRetirer(client)
		} else {
			terminate = ciworker.ScalewayTerminator(getenv("QUILL_CIW_ZONE", "fr-par-1"))
		}
	}

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
