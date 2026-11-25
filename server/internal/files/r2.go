package files

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
)

var ErrBudget = store.ErrR2Budget
var ErrR2Credentials = errors.New("R2 credentials rejected")

// R2 uses single-part Standard writes and authenticated byte proxying only.
// No redirects, retries, public URLs or credentials are exposed to callers.
type R2 struct {
	DB                                  *store.DB
	Endpoint, Bucket, AccessKey, Secret string
	HTTP                                *http.Client
	Now                                 func() time.Time
	// Alert delivers an operator warning and reports whether it was accepted.
	Alert func(ctx context.Context, reason string) bool
	mu    sync.RWMutex
}

// AlertReason describes the thresholds named by a store.R2Alert* mask.
func AlertReason(mask int) string {
	var refused, warned []string
	for _, m := range []struct {
		warn, refuse int
		name         string
	}{
		{store.R2AlertStorageWarn, store.R2AlertStorageRefused, "storage"},
		{store.R2AlertClassAWarn, store.R2AlertClassARefused, "Class A operations"},
		{store.R2AlertClassBWarn, store.R2AlertClassBRefused, "Class B operations"},
	} {
		switch {
		case mask&m.refuse != 0:
			refused = append(refused, m.name)
		case mask&m.warn != 0:
			warned = append(warned, m.name)
		}
	}
	var parts []string
	if len(refused) > 0 {
		parts = append(parts, "R2 reached 90% of "+strings.Join(refused, ", ")+": those requests are now refused.")
	}
	if len(warned) > 0 {
		parts = append(parts, "R2 reached 70% of "+strings.Join(warned, ", ")+"; requests stop at 90%.")
	}
	if len(parts) == 0 {
		return "R2 usage needs attention."
	}
	return strings.Join(parts, " ") + " Inspect R2 usage and inventory."
}

func NewR2(db *store.DB, account, bucket, access, secret string) (*R2, error) {
	if db == nil || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(account) || !regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`).MatchString(bucket) || access == "" || secret == "" {
		return nil, errors.New("R2 requires account ID, bucket and separate access key/secret")
	}
	r := &R2{DB: db, Endpoint: "https://" + account + ".r2.cloudflarestorage.com", Bucket: bucket, AccessKey: access, Secret: secret, Now: time.Now, HTTP: &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if err := db.BindR2(context.Background(), account+"/"+bucket); err != nil {
		return nil, err
	}
	return r, nil
}

func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func mac(key []byte, s string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(s))
	return h.Sum(nil)
}

// signV4 implements the S3 header signing algorithm, including every x-amz
// header. URI paths are never normalized (S3 keys are opaque).
func signV4(req *http.Request, body []byte, access, secret, region string, now time.Time) {
	stamp := now.UTC().Format("20060102T150405Z")
	day := stamp[:8]
	req.Header.Set("X-Amz-Date", stamp)
	req.Header.Set("X-Amz-Content-Sha256", digest(body))
	headers := map[string]string{"host": req.URL.Host}
	for k, v := range req.Header {
		lower := strings.ToLower(k)
		if strings.HasPrefix(lower, "x-amz-") || lower == "range" || lower == "content-type" || lower == "date" || lower == "content-md5" {
			headers[lower] = strings.Join(strings.Fields(strings.Join(v, ",")), " ")
		}
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var canonical strings.Builder
	for _, k := range keys {
		canonical.WriteString(k + ":" + headers[k] + "\n")
	}
	signed := strings.Join(keys, ";")
	query := strings.ReplaceAll(req.URL.Query().Encode(), "+", "%20")
	path := strings.ReplaceAll(url.QueryEscape(req.URL.Path), "+", "%20")
	path = strings.ReplaceAll(path, "%2F", "/")
	request := strings.Join([]string{req.Method, path, query, canonical.String(), signed, digest(body)}, "\n")
	scope := day + "/" + region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + stamp + "\n" + scope + "\n" + digest([]byte(request))
	k := mac([]byte("AWS4"+secret), day)
	k = mac(k, region)
	k = mac(k, "s3")
	k = mac(k, "aws4_request")
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+access+"/"+scope+", SignedHeaders="+signed+", Signature="+hex.EncodeToString(mac(k, toSign)))
}

func (r *R2) reserve(ctx context.Context, class, key, user string, size int64) error {
	now := r.Now().UTC()
	alert, err := r.DB.ReserveR2(ctx, now, class, key, user, size)
	if alert && r.Alert != nil {
		warning, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		mask, claimErr := r.DB.R2AlertClaim(warning, now)
		if claimErr != nil {
			mask = 0
		}
		delivered := r.Alert(warning, AlertReason(mask))
		cancel()
		if delivered {
			_ = r.DB.AckR2Alert(context.WithoutCancel(ctx), now)
		}
	}
	return err
}

func (r *R2) request(ctx context.Context, method, key string, query url.Values, body []byte, mime string) (*http.Response, error) {
	u := r.Endpoint + "/" + r.Bucket + "/" + key
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("invalid R2 request")
	}
	if mime != "" {
		req.Header.Set("Content-Type", mime)
	}
	if method == http.MethodPut {
		req.Header.Set("X-Amz-Storage-Class", "STANDARD")
	}
	signV4(req, body, r.AccessKey, r.Secret, "auto", r.Now())
	res, err := r.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("R2 request failed (budget retained)")
	}
	if res.StatusCode == 401 || res.StatusCode == 403 {
		res.Body.Close()
		return nil, ErrR2Credentials
	}
	return res, nil
}

func (r *R2) Put(ctx context.Context, in PutInput) (Stored, error) {
	if err := ValidateUpload(in.MimeType, int64(len(in.Body))); err != nil {
		return Stored{}, err
	}
	if err := CheckContent(in.MimeType, in.Body); err != nil {
		return Stored{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	key := "files/" + auth.UUID()
	if in.MigrationID != "" {
		var uploaded bool
		var err error
		key, uploaded, err = r.DB.R2MigrationKey(ctx, in.MigrationID, key)
		if err != nil {
			return Stored{}, err
		}
		if uploaded {
			return Stored{Storage: "r2", Key: key, SizeBytes: int64(len(in.Body))}, nil
		}
	}
	if err := r.reserve(ctx, "put", key, in.UserID, int64(len(in.Body))); err != nil {
		return Stored{}, err
	}
	res, err := r.request(ctx, http.MethodPut, key, nil, in.Body, in.MimeType)
	if err != nil {
		if errors.Is(err, ErrR2Credentials) {
			_ = r.DB.DeletedR2(context.WithoutCancel(ctx), key)
		}
		return Stored{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		// A completed failure response can be cleaned up safely. Transport
		// timeouts stay reserved until an inventory taken after
		// store.R2PendingWindow proves the key absent (see ReconcileR2).
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		_ = r.deleteLocked(cleanup, key)
		cancel()
		return Stored{}, fmt.Errorf("R2 upload failed (%d)", res.StatusCode)
	}
	if in.MigrationID != "" {
		if err := r.DB.R2MigrationUploaded(ctx, in.MigrationID); err != nil {
			return Stored{}, err
		}
	}
	return Stored{Storage: "r2", Key: key, SizeBytes: int64(len(in.Body))}, nil
}

func r2Key(key string) bool {
	return strings.HasPrefix(key, "files/") && keyRE.MatchString(strings.TrimPrefix(key, "files/"))
}

func (r *R2) Get(ctx context.Context, key string) (*File, error) {
	if !r2Key(key) {
		return nil, nil
	}
	if err := r.reserve(ctx, "get", "", "", 0); err != nil {
		return nil, err
	}
	res, err := r.request(ctx, http.MethodGet, key, nil, nil, "")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return nil, nil
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("R2 download failed (%d)", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, MaxBytes+1))
	if err != nil {
		return nil, errors.New("R2 download failed")
	}
	mime := res.Header.Get("Content-Type")
	if err := ValidateUpload(mime, int64(len(body))); err != nil {
		return nil, err
	}
	if err := CheckContent(mime, body); err != nil {
		return nil, err
	}
	return &File{Body: body, MimeType: mime}, nil
}

func (r *R2) Delete(ctx context.Context, key string) error {
	if !r2Key(key) {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.deleteLocked(ctx, key)
}

func (r *R2) deleteLocked(ctx context.Context, key string) error {
	if err := r.reserve(ctx, "delete", "", "", 0); err != nil {
		return err
	}
	res, err := r.request(ctx, http.MethodDelete, key, nil, nil, "")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 204 && res.StatusCode != 200 && res.StatusCode != 404 {
		return fmt.Errorf("R2 delete failed (%d)", res.StatusCode)
	}
	return r.DB.DeletedR2(ctx, key)
}

// Reconcile lists every page, charged as Class A. Multipart is unsupported; any
// unfinished upload or non-Standard object fails closed until the operator fixes it.
func (r *R2) Reconcile(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.DB.InvalidateR2(ctx); err != nil {
		return err
	}
	var total int64
	listed := map[string]bool{}
	query := url.Values{"list-type": {"2"}, "max-keys": {"1000"}}
	for {
		if err := r.reserve(ctx, "list", "", "", 0); err != nil {
			return err
		}
		res, err := r.request(ctx, http.MethodGet, "", query, nil, "")
		if err != nil {
			return err
		}
		var page struct {
			Contents []struct {
				Key          string
				Size         int64
				StorageClass string
			}
			IsTruncated           bool
			NextContinuationToken string
		}
		err = xml.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&page)
		res.Body.Close()
		if res.StatusCode != 200 || err != nil {
			return errors.New("R2 inventory failed")
		}
		for _, o := range page.Contents {
			if o.Size < 0 || o.Size > store.R2StorageLimit || o.StorageClass != "STANDARD" {
				return errors.New("R2 inventory contains invalid or non-Standard objects")
			}
			total += o.Size
			listed[o.Key] = true
			if total > store.R2StorageLimit {
				return ErrBudget
			}
		}
		if !page.IsTruncated {
			break
		}
		if page.NextContinuationToken == "" || page.NextContinuationToken == query.Get("continuation-token") {
			return errors.New("invalid R2 inventory pagination")
		}
		query.Set("continuation-token", page.NextContinuationToken)
	}
	if err := r.reserve(ctx, "list", "", "", 0); err != nil {
		return err
	}
	res, err := r.request(ctx, http.MethodGet, "", url.Values{"uploads": {""}, "max-uploads": {"1"}}, nil, "")
	if err != nil {
		return err
	}
	var multipart struct {
		Upload      []struct{ Key string }
		IsTruncated bool
	}
	err = xml.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&multipart)
	res.Body.Close()
	if res.StatusCode != 200 || err != nil || len(multipart.Upload) > 0 || multipart.IsTruncated {
		return errors.New("R2 unfinished multipart upload or inventory failure; uploads disabled")
	}
	if err := r.DB.ReconcileR2(ctx, r.Now(), total, listed); err != nil {
		return err
	}
	// Check the reconciled bytes immediately, including the 70% notification.
	return r.reserve(ctx, "check", "", "", 0)
}

// Daily runs the first inventory immediately, so startup never waits on R2,
// then every 24 hours. A failure keeps uploads closed, retries every five
// minutes, and alerts once per failure streak (re-sent until Discord accepts).
func (r *R2) Daily(ctx context.Context, onError func(error)) {
	t := time.NewTimer(0)
	defer t.Stop()
	alerted := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if until, err := r.DB.R2LockedUntil(ctx); err == nil && until.After(r.Now()) {
				t.Reset(until.Sub(r.Now()) + time.Second)
				continue
			}
			scan, cancel := context.WithTimeout(ctx, 2*time.Minute)
			err := r.Reconcile(scan)
			cancel()
			if err != nil {
				onError(err)
				if !alerted && r.Alert != nil && ctx.Err() == nil {
					reason := "R2 inventory scan failed; uploads stay closed until it succeeds (retrying every 5 minutes)."
					if errors.Is(err, ErrR2Credentials) {
						reason = "R2 rejected the configured access key; uploads stay closed and downloads fail until the R2 token is fixed."
					}
					warning, stop := context.WithTimeout(ctx, 5*time.Second)
					alerted = r.Alert(warning, reason)
					stop()
				}
				t.Reset(5 * time.Minute)
			} else {
				alerted = false
				t.Reset(24 * time.Hour)
			}
		}
	}
}
