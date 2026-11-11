// spike-pr0 is the PR-0 go/no-go probe from deploy/scaleway/serverless-roadmap.md.
//
// It answers, from wherever it runs (locally and inside a Scaleway Serverless
// Job), the spike questions that can be probed programmatically:
//
//  1. Does Scaleway Object Storage honor S3 conditional writes (If-Match /
//     If-None-Match on PUT), and are ETags stable across reads?
//  2. Does it honor Range requests on GET (the later streaming/partial
//     hydration premise for the bucket layout)?
//  3. Runtime facts: work-dir writability and size, git availability, egress
//     to the production api.
//
// Stdlib only — deliberately no S3 client dependency; AWS SigV4 is implemented
// inline so the probe can send exact conditional headers. The runtime client
// choice (aws-sdk-go-v2 vs minio-go) is a Phase B1 decision, not a spike one.
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type finding struct {
	Name   string         `json:"name"`
	Pass   bool           `json:"pass"`
	Detail string         `json:"detail"`
	Extra  map[string]any `json:"extra,omitempty"`
	Error  string         `json:"error,omitempty"`
}

type report struct {
	Where     string    `json:"where"` // "local" or "job"
	Timestamp string    `json:"timestamp"`
	Findings  []finding `json:"findings"`
}

var findings []finding

func record(f finding) {
	findings = append(findings, f)
	b, _ := json.Marshal(f)
	fmt.Println(string(b))
}

// --- AWS SigV4 (service s3, path-style) --------------------------------------

type s3 struct {
	endpoint  string // e.g. https://s3.fr-par.scw.cloud
	bucket    string
	region    string
	accessKey string
	secretKey string
	client    *http.Client
}

func (s *s3) do(method, key string, body []byte, headers map[string]string) (*http.Response, []byte, error) {
	host := strings.TrimPrefix(s.endpoint, "https://")
	u := fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, key)
	bodyHash := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(bodyHash[:])
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	canonicalHeaders := fmt.Sprintf("host:%s\nx-amz-content-sha256:%s\nx-amz-date:%s\n", host, payloadHash, amzDate)
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalURI := "/" + s.bucket + "/" + key
	canonicalRequest := strings.Join([]string{method, canonicalURI, "", canonicalHeaders, signedHeaders, payloadHash}, "\n")

	scope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, s.region)
	crHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hex.EncodeToString(crHash[:]),
	}, "\n")

	mac := hmac.New(sha256.New, []byte("AWS4"+s.secretKey))
	mac.Write([]byte(dateStamp))
	kDate := mac.Sum(nil)
	mac = hmac.New(sha256.New, kDate)
	mac.Write([]byte(s.region))
	kRegion := mac.Sum(nil)
	mac = hmac.New(sha256.New, kRegion)
	mac.Write([]byte("s3"))
	kService := mac.Sum(nil)
	mac = hmac.New(sha256.New, kService)
	mac.Write([]byte("aws4_request"))
	kSigning := mac.Sum(nil)
	mac = hmac.New(sha256.New, kSigning)
	mac.Write([]byte(stringToSign))
	signature := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest(method, u, strings.NewReader(string(body)))
	if err != nil {
		return nil, nil, err
	}
	req.Host = host
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.accessKey, scope, signedHeaders, signature))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if len(body) > 0 {
		req.ContentLength = int64(len(body))
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp, nil, err
	}
	return resp, b, nil
}

// --- probes --------------------------------------------------------------------

const spikePrefix = "spike-pr0"

func probeConditionalWrites(s *s3) {
	key := spikePrefix + "/manifest.json"

	// 1. Fresh create with If-None-Match: * (the "no manifest exists yet" path).
	resp, body, err := s.do("PUT", key, []byte(`{"version":1,"state":"v1"}`), map[string]string{"If-None-Match": "*"})
	if err != nil {
		record(finding{Name: "s3.put.if-none-match.create", Pass: false, Error: err.Error()})
		return
	}
	etag1 := resp.Header.Get("ETag")
	record(finding{
		Name: "s3.put.if-none-match.create", Pass: resp.StatusCode == 200,
		Detail: fmt.Sprintf("status=%d etag=%s body=%q", resp.StatusCode, etag1, string(body)),
		Extra:  map[string]any{"etag": etag1},
	})

	// 2. Duplicate create: a manifest already exists -> must NOT silently
	// overwrite (the heart of the conditional-write question).
	resp, body, _ = s.do("PUT", key, []byte(`{"version":1,"state":"RACE"}`), map[string]string{"If-None-Match": "*"})
	record(finding{
		Name: "s3.put.if-none-match.duplicate-rejected", Pass: resp.StatusCode == 412,
		Detail: fmt.Sprintf("status=%d body=%q", resp.StatusCode, string(body)),
		Extra:  map[string]any{"status": resp.StatusCode},
	})

	// 3. ETag stability across reads.
	resp, body, _ = s.do("GET", key, nil, nil)
	getEtag := resp.Header.Get("ETag")
	record(finding{
		Name: "s3.etag.stable-across-reads", Pass: getEtag == etag1,
		Detail: fmt.Sprintf("put_etag=%s get_etag=%s same=%v", etag1, getEtag, getEtag == etag1),
	})

	// 4. If-Match with a wrong etag -> must reject.
	resp, body, _ = s.do("PUT", key, []byte(`{"version":1,"state":"lost-race"}`), map[string]string{"If-Match": `"bogus-etag"`})
	record(finding{
		Name: "s3.put.if-match.wrong-rejected", Pass: resp.StatusCode == 412,
		Detail: fmt.Sprintf("status=%d body=%q", resp.StatusCode, string(body)),
		Extra:  map[string]any{"status": resp.StatusCode},
	})

	// 5. If-Match with the current etag -> must succeed.
	resp, body, _ = s.do("PUT", key, []byte(`{"version":1,"state":"v2"}`), map[string]string{"If-Match": getEtag})
	etag2 := resp.Header.Get("ETag")
	record(finding{
		Name: "s3.put.if-match.current-succeeds", Pass: resp.StatusCode == 200,
		Detail: fmt.Sprintf("status=%d etag=%s body=%q", resp.StatusCode, etag2, string(body)),
		Extra:  map[string]any{"etag_new": etag2},
	})

	// 6. Range GET (streaming/partial-hydration premise).
	resp, body, _ = s.do("GET", key, nil, map[string]string{"Range": "bytes=0-3"})
	record(finding{
		Name: "s3.get.range-206-partial", Pass: resp.StatusCode == 206 && string(body) == `{"ve`,
		Detail: fmt.Sprintf("status=%d body=%q (want 4 leading bytes of the manifest)", resp.StatusCode, string(body)),
		Extra:  map[string]any{"content_range": resp.Header.Get("Content-Range")},
	})

	// Cleanup.
	resp, _, _ = s.do("DELETE", key, nil, nil)
	record(finding{
		Name: "s3.delete.cleanup", Pass: resp.StatusCode == 204,
		Detail: fmt.Sprintf("status=%d", resp.StatusCode),
	})
}

func probeRuntime(dirs ...string) {
	// Existence probe for candidate job-volume mount points (where the
	// local-storage-capacity volume actually lands is not obvious from the
	// docs — find it empirically).
	for _, c := range []string{"/mnt", "/mnt/local-data", "/var/tmp", "/data", "/scw", "/workspace", "/home"} {
		if info, err := os.Stat(c); err == nil {
			record(finding{Name: "runtime.path-present", Detail: fmt.Sprintf("%s is_dir=%v", c, info.IsDir()), Pass: true})
		}
	}
	for _, workDir := range dirs {
		f, err := os.CreateTemp(workDir, "spike-*.bin")
		if err != nil {
			record(finding{Name: "runtime.workdir.usable", Detail: workDir, Pass: false, Error: err.Error()})
			continue
		}
		buf := make([]byte, 1024*1024)
		written := 0
		start := time.Now()
		for written < 700<<20 { // write 700 MiB (past the 512 MiB memory limit):
			// success implies the path is disk-backed (local-storage volume),
			// not a RAM-bound tmpfs.
			n, _ := f.Write(buf)
			if n == 0 {
				break
			}
			written += n
		}
		f.Close()
		os.Remove(f.Name())
		record(finding{
			Name:   "runtime.workdir.usable",
			Pass:   written >= 700<<20,
			Detail: fmt.Sprintf("%s wrote %d MiB in %s (memory limit 512 MiB: >=700 MiB means disk-backed)", workDir, written>>20, time.Since(start).Round(time.Millisecond)),
		})
	}
	if out, err := exec.Command("git", "--version").Output(); err != nil {
		record(finding{Name: "runtime.git.available", Pass: false, Error: err.Error()})
	} else {
		record(finding{Name: "runtime.git.available", Pass: true, Detail: strings.TrimSpace(string(out))})
	}
	// Is /tmp a RAM-backed tmpfs (hydration there would count against the
	// memory limit) while the job volume is a real disk?
	if b, err := os.ReadFile("/proc/mounts"); err == nil {
		record(finding{
			Name:   "runtime.tmp-ramtmpfs",
			Pass:   true,
			Detail: fmt.Sprintf("/tmp in /proc/mounts=%v", strings.Contains(string(b), " /tmp ")),
		})
	}
}

func probeEgress(target string) {
	start := time.Now()
	resp, err := http.Get(target)
	if err != nil {
		record(finding{Name: "egress.api.reachable", Pass: false, Error: err.Error()})
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	record(finding{
		Name:   "egress.api.reachable",
		Pass:   resp.StatusCode < 500,
		Detail: fmt.Sprintf("GET %s -> %d in %s", target, resp.StatusCode, time.Since(start).Round(time.Millisecond)),
	})
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	if len(os.Args) > 2 && os.Args[1] == "del" { // del <key>: DELETE a bucket object
		s := &s3{
			endpoint:  envOr("S3_ENDPOINT", "https://s3.fr-par.scw.cloud"),
			bucket:    os.Getenv("S3_BUCKET"),
			region:    envOr("S3_REGION", "fr-par"),
			accessKey: os.Getenv("AWS_ACCESS_KEY_ID"),
			secretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			client:    &http.Client{Timeout: 30 * time.Second},
		}
		resp, _, err := s.do("DELETE", os.Args[2], nil, nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("DELETE %s status=%d\n", os.Args[2], resp.StatusCode)
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "fetch" { // fetch <key>: GET a bucket object and print it
		s := &s3{
			endpoint:  envOr("S3_ENDPOINT", "https://s3.fr-par.scw.cloud"),
			bucket:    os.Getenv("S3_BUCKET"),
			region:    envOr("S3_REGION", "fr-par"),
			accessKey: os.Getenv("AWS_ACCESS_KEY_ID"),
			secretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			client:    &http.Client{Timeout: 30 * time.Second},
		}
		resp, body, err := s.do("GET", os.Args[2], nil, nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("status=%d\n%s\n", resp.StatusCode, string(body))
		return
	}

	where := envOr("SPIKE_WHERE", "local")
	fmt.Printf(`{"name":"spike.start","detail":"spike-pr0 probe starting","where":"%s","time":"%s"}`+"\n", where, time.Now().UTC().Format(time.RFC3339))

	var s *s3
	access := os.Getenv("AWS_ACCESS_KEY_ID")
	secret := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if access == "" || secret == "" {
		record(finding{Name: "s3.credentials", Pass: false, Error: "AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY not set"})
	} else {
		s = &s3{
			endpoint:  envOr("S3_ENDPOINT", "https://s3.fr-par.scw.cloud"),
			bucket:    os.Getenv("S3_BUCKET"),
			region:    envOr("S3_REGION", "fr-par"),
			accessKey: access,
			secretKey: secret,
			client:    &http.Client{Timeout: 30 * time.Second},
		}
		probeConditionalWrites(s)
	}

	dirs := strings.Split(envOr("SPIKE_WORKDIRS", "/tmp,/mnt"), ",")
	probeRuntime(dirs...)
	probeEgress(envOr("SPIKE_EGRESS_TARGET", "https://api.black-tulip.nl/readyz"))

	out, _ := json.Marshal(report{
		Where:     where,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Findings:  findings,
	})
	fmt.Println("SPIKE_REPORT_JSON=" + string(out))

	// Self-collect: push the report to the bucket so the run is observable
	// without job-log plumbing (the same pattern the CI worker will use for
	// streamed pipeline logs).
	if s != nil && s.bucket != "" {
		key := fmt.Sprintf("%s/report-%s.json", spikePrefix, where)
		if resp, _, err := s.do("PUT", key, out, nil); err != nil {
			fmt.Printf("report upload failed: %v\n", err)
		} else {
			fmt.Printf("report uploaded to s3://%s/%s status=%d\n", s.bucket, key, resp.StatusCode)
		}
	}
}
