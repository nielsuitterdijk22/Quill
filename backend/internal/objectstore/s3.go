// Package objectstore is a minimal S3-compatible object store client for the
// Quill git engine (serverless epic, Phase B): SigV4-signed PUT/GET/HEAD/
// DELETE with the conditional-write surface the ref-CAS design depends on
// (If-None-Match create-if-absent, If-Match read-modify-write).
//
// No AWS SDK dependency: the signer was lifted from the PR-0 spike probe
// (deploy/scaleway/scripts/spike-pr0) whose live run proved Scaleway Object
// Storage honours 412 semantics and Range requests; stdlib-only keeps the
// backend dependency set unchanged.
package objectstore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// ErrNotFound means the object does not exist (S3 404/NoSuchKey).
var ErrNotFound = errors.New("objectstore: not found")

// ErrPreconditionFailed maps S3 412: a conditional write raced (manifest
// existed when If-None-Match said create, or the etag moved under If-Match).
// Ref-CAS callers translate this into a lost race.
var ErrPreconditionFailed = errors.New("objectstore: precondition failed")

// Config is the credentials + endpoint of an S3-compatible store.
type Config struct {
	Endpoint  string // e.g. https://s3.fr-par.scw.cloud
	Region    string // e.g. fr-par
	Bucket    string
	AccessKey string
	SecretKey string
}

// ConfigFromEnv builds a Config from <prefix>ENDPOINT, <prefix>REGION,
// <prefix>BUCKET, <prefix>ACCESS_KEY and <prefix>SECRET_KEY — shared by the
// runtime config (QUILL_GIT_S3_*) and the live test harness
// (QUILL_TEST_S3_*).
func ConfigFromEnv(prefix string) Config {
	return Config{
		Endpoint:  getenv(prefix + "ENDPOINT"),
		Region:    getenv(prefix + "REGION"),
		Bucket:    getenv(prefix + "BUCKET"),
		AccessKey: getenv(prefix + "ACCESS_KEY"),
		SecretKey: getenv(prefix + "SECRET_KEY"),
	}
}

func getenv(key string) string { return strings.TrimSpace(os.Getenv(key)) }

// PutOptions carry the conditional-write contract. At most one condition may
// be set; zero options mean an unconditional (overwrite) put.
type PutOptions struct {
	// IfNoneMatch "*" writes only if the object does not exist yet.
	IfNoneMatch string
	// IfMatch writes only when the object currently has this etag.
	IfMatch string
}

// Client talks to one bucket.
type Client struct {
	cfg    Config
	host   string // endpoint without scheme, used as the signed Host header
	client *http.Client
}

// New builds a client for cfg.
func New(cfg Config) *Client {
	return &Client{
		cfg:    cfg,
		host:   strings.TrimPrefix(cfg.Endpoint, "https://"),
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

const maxObjectBytes = 64 << 20 // 64 MiB: manifests are small; packs stream via Range

// do signs and runs one request. body may be nil; query carries S3
// subresource/query parameters (List uses it; the object CRUD paths pass nil).
func (c *Client) do(ctx context.Context, method, key string, body []byte, opts PutOptions, extra map[string]string, query map[string]string) (*http.Response, error) {
	u := fmt.Sprintf("%s/%s/%s", c.cfg.Endpoint, c.cfg.Bucket, key)
	if enc := canonicalQuery(query); enc != "" {
		u += "?" + enc
	}
	req, err := http.NewRequestWithContext(ctx, method, u, func() io.Reader {
		if body == nil {
			return nil
		}
		return strings.NewReader(string(body))
	}())
	if err != nil {
		return nil, err
	}
	req.Host = c.host
	if body != nil {
		req.ContentLength = int64(len(body))
	}

	payloadHash := sha256Hex(body)
	req.Header.Set("x-amz-date", time.Now().UTC().Format("20060102T150405Z"))
	req.Header.Set("x-amz-content-sha256", payloadHash)
	// Conditional headers are part of the signed canonical request, so they
	// cannot be stripped or tampered with mid-flight.
	switch {
	case opts.IfNoneMatch != "":
		req.Header.Set("If-None-Match", opts.IfNoneMatch)
	case opts.IfMatch != "":
		req.Header.Set("If-Match", opts.IfMatch)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}

	if err := c.sign(req, method, key, payloadHash, query); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	// The caller owns the body from here (drainClose). do never defers the
	// close: the 2xx path hands the open body to Put/Get/Head/Delete.
	if resp.StatusCode == http.StatusPreconditionFailed {
		drainClose(resp)
		return nil, ErrPreconditionFailed
	}
	if resp.StatusCode == http.StatusNotFound {
		drainClose(resp)
		return nil, ErrNotFound
	}
	if resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		drainClose(resp)
		return nil, fmt.Errorf("objectstore: %s %s: %s: %s", method, key, resp.Status, snippet)
	}
	return resp, nil
}

// Put stores body at key and returns the stored object's etag. With
// PutOptions{IfNoneMatch: "*"} it refuses to overwrite; with IfMatch it
// refuses unless the current etag matches.
func (c *Client) Put(ctx context.Context, key string, body []byte, opts PutOptions) (etag string, err error) {
	resp, err := c.do(ctx, http.MethodPut, key, body, opts, nil, nil)
	if err != nil {
		return "", err
	}
	defer drainClose(resp)
	return strings.Trim(resp.Header.Get("ETag"), `"`), nil
}

// Get returns the object's bytes and etag (header ETag normalized without quotes).
func (c *Client) Get(ctx context.Context, key string) (body []byte, etag string, err error) {
	return c.getRange(ctx, key, 0, 0)
}

// GetRange returns a byte slice of the object; when hi > lo the Range header
// is applied (S3 semantics: inclusive bytes=lo-hi). B2's PackSource streaming
// relies on this being the one fetch primitive. hi == 0 && lo == 0 fetches all.
func (c *Client) getRange(ctx context.Context, key string, lo, hi int64) (body []byte, etag string, err error) {
	extra := map[string]string{}
	if hi > lo && lo >= 0 {
		extra["Range"] = fmt.Sprintf("bytes=%d-%d", lo, hi)
	}
	resp, err := c.do(ctx, http.MethodGet, key, nil, PutOptions{}, extra, nil)
	if err != nil {
		return nil, "", err
	}
	defer drainClose(resp)
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxObjectBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(b)) > maxObjectBytes {
		return nil, "", fmt.Errorf("objectstore: object %s exceeds the %d MiB inline cap; use GetRange", key, maxObjectBytes>>20)
	}
	return b, strings.Trim(resp.Header.Get("ETag"), `"`), nil
}

// GetRange returns the inclusive byte range [lo, hi] of the object.
func (c *Client) GetRange(ctx context.Context, key string, lo, hi int64) (body []byte, err error) {
	body, _, err = c.getRange(ctx, key, lo, hi)
	return body, err
}

// Head returns the object's size and etag without fetching it.
func (c *Client) Head(ctx context.Context, key string) (size int64, etag string, err error) {
	resp, err := c.do(ctx, http.MethodHead, key, nil, PutOptions{}, nil, nil)
	if err != nil {
		return 0, "", err
	}
	defer drainClose(resp)
	return resp.ContentLength, strings.Trim(resp.Header.Get("ETag"), `"`), nil
}

// Delete removes the object; deleting a missing object is ErrNotFound.
func (c *Client) Delete(ctx context.Context, key string) error {
	resp, err := c.do(ctx, http.MethodDelete, key, nil, PutOptions{}, nil, nil)
	if err != nil {
		return err
	}
	drainClose(resp)
	return nil
}

func drainClose(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
}

// canonicalQuery renders query params in SigV4 canonical form: keys sorted,
// values AWS-URI-encoded (RFC3986, '+' NOT accepted as space).
func canonicalQuery(q map[string]string) string {
	if len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, awsURIEncode(k)+"="+awsURIEncode(q[k]))
	}
	return strings.Join(parts, "&")
}

// awsURIEncode percent-encodes everything except unreserved bytes, the way
// SigV4 demands (spaces as %20, '/' encoded).
func awsURIEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= 'A' && ch <= 'Z', ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9',
			ch == '-', ch == '_', ch == '.', ch == '~':
			b.WriteByte(ch)
		default:
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

// List returns every object key under prefix, paginating internally.
// Keys are bucket-relative.
func (c *Client) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	token := ""
	for {
		q := map[string]string{"list-type": "2", "prefix": prefix, "max-keys": "1000"}
		if token != "" {
			q["continuation-token"] = token
		}
		resp, err := c.do(ctx, http.MethodGet, "", nil, PutOptions{}, nil, q)
		if err != nil {
			return nil, err
		}
		var out struct {
			IsTruncated           bool   `xml:"IsTruncated"`
			NextContinuationToken string `xml:"NextContinuationToken"`
			Contents              []struct {
				Key string `xml:"Key"`
			} `xml:"Contents"`
		}
		err = xml.NewDecoder(resp.Body).Decode(&out)
		drainClose(resp)
		if err != nil {
			return nil, fmt.Errorf("objectstore: list %s: %w", prefix, err)
		}
		for _, o := range out.Contents {
			keys = append(keys, o.Key)
		}
		if !out.IsTruncated || out.NextContinuationToken == "" {
			return keys, nil
		}
		token = out.NextContinuationToken
	}
}

// sign adds the SigV4 Authorization header (AWS4-HMAC-SHA256) for one request.
func (c *Client) sign(req *http.Request, method, key, payloadHash string, query map[string]string) error {
	amzDate := req.Header.Get("x-amz-date")
	dateStamp := amzDate[:8]
	region := c.cfg.Region
	if region == "" {
		return errors.New("objectstore: region required")
	}

	// Canonical headers: host + x-amz-* (sorted, lowercase). Conditional
	// headers (If-Match/If-None-Match) are optional; include them when present.
	type vh struct{ name, value string }
	var headers []vh
	add := func(name string) {
		if v := req.Header.Get(name); v != "" {
			headers = append(headers, vh{name: strings.ToLower(name), value: v})
		}
	}
	headers = append(headers,
		vh{name: "host", value: req.Host},
		vh{name: "x-amz-content-sha256", value: payloadHash},
		vh{name: "x-amz-date", value: amzDate},
	)
	add("If-Match")
	add("If-None-Match")
	add("Range")
	// sorted by name
	for i := 1; i < len(headers); i++ {
		for j := i; j > 0 && headers[j-1].name > headers[j].name; j-- {
			headers[j-1], headers[j] = headers[j], headers[j-1]
		}
	}
	var ch, sh strings.Builder
	for _, h := range headers {
		ch.WriteString(h.name + ":" + h.value + "\n")
		sh.WriteString(h.name + ";")
	}
	signedHeaders := strings.TrimSuffix(sh.String(), ";")

	canonicalRequest := strings.Join([]string{
		method,
		"/" + c.cfg.Bucket + "/" + key,
		canonicalQuery(query),
		ch.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, region)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256", amzDate, scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	mac := hmac.New(sha256.New, []byte("AWS4"+c.cfg.SecretKey))
	mac.Write([]byte(dateStamp))
	kDate := mac.Sum(nil)
	mac = hmac.New(sha256.New, kDate)
	mac.Write([]byte(region))
	kRegion := mac.Sum(nil)
	mac = hmac.New(sha256.New, kRegion)
	mac.Write([]byte("s3"))
	kService := mac.Sum(nil)
	mac = hmac.New(sha256.New, kService)
	mac.Write([]byte("aws4_request"))
	kSigning := mac.Sum(nil)
	mac = hmac.New(sha256.New, kSigning)
	mac.Write([]byte(stringToSign))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.cfg.AccessKey, scope, signedHeaders, hex.EncodeToString(mac.Sum(nil))))
	return nil
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
