package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"officerdashboard/internal/store"
	"os"
	"strings"
	"testing"
	"time"
)

// Published S3 SigV4 examples; these are public example keys, never credentials.
// https://docs.aws.amazon.com/AmazonS3/latest/developerguide/sig-v4-header-based-auth.html
func TestSigV4AWSVectors(t *testing.T) {
	for _, v := range []struct {
		method, path, body, signature string
		headers                       map[string]string
	}{
		{"GET", "/test.txt", "", "f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41", map[string]string{"Range": "bytes=0-9"}},
		{"PUT", "/test%24file.text", "Welcome to Amazon S3.", "98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd", map[string]string{"Date": "Fri, 24 May 2013 00:00:00 GMT", "X-Amz-Storage-Class": "REDUCED_REDUNDANCY"}},
		{"GET", "/?lifecycle", "", "fea454ca298b7da1c68078a5d1bdbfbbe0d65c699e0f91ac7a200a0136783543", nil},
		{"GET", "/?max-keys=2&prefix=J", "", "34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7", nil},
	} {
		t.Run(v.method+v.path, func(t *testing.T) {
			req, _ := http.NewRequest(v.method, "https://examplebucket.s3.amazonaws.com"+v.path, nil)
			for k, h := range v.headers {
				req.Header.Set(k, h)
			}
			signV4(req, []byte(v.body), "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1", time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
			if !strings.HasSuffix(req.Header.Get("Authorization"), "Signature="+v.signature) {
				t.Fatalf("signature mismatch: %s", req.Header.Get("Authorization"))
			}
		})
	}
}

func testR2(t *testing.T) *R2 {
	t.Helper()
	db, err := store.Open(t.TempDir() + "/budget.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	r, err := NewR2(db, strings.Repeat("a", 32), "receipts", "test-access", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	r.Now = func() time.Time { return time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC) }
	return r
}

func TestR2AmbiguousFailureRetainsReservation(t *testing.T) {
	r := testR2(t)
	ctx := context.Background()
	r.DB.ReconcileR2(ctx, r.Now(), 0, nil)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(503) }))
	defer fake.Close()
	r.Endpoint = fake.URL
	in := PutInput{UserID: "alice", MimeType: "application/pdf", Body: []byte("%PDF-x")}
	if _, err := r.Put(ctx, in); err == nil {
		t.Fatal("expected failure")
	}
	u, err := r.DB.R2Usage(ctx, r.Now())
	if err != nil || u.ClassA != 1 || u.Bytes != 6 {
		t.Fatalf("reservation lost: %+v %v", u, err)
	}
	for _, mime := range []string{"text/html", "image/svg+xml"} {
		in.MimeType = mime
		if _, err := r.Put(ctx, in); !errors.Is(err, ErrInvalidUpload) {
			t.Fatal(err)
		}
	}
	in.MimeType = "application/pdf"
	in.Body = []byte("<html>")
	if _, err := r.Put(ctx, in); !errors.Is(err, ErrInvalidUpload) {
		t.Fatal(err)
	}
}

func TestR2ReconcileFailsClosed(t *testing.T) {
	r := testR2(t)
	ctx := context.Background()
	r.DB.ReconcileR2(ctx, r.Now(), 0, nil)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Has("uploads") {
			io.WriteString(w, `<ListMultipartUploadsResult><Upload><Key>x</Key></Upload></ListMultipartUploadsResult>`)
			return
		}
		io.WriteString(w, `<ListBucketResult><Contents><Size>42</Size><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>`)
	}))
	defer fake.Close()
	r.Endpoint = fake.URL
	if err := r.Reconcile(ctx); err == nil {
		t.Fatal("multipart must close uploads")
	}
	if _, err := r.Put(ctx, PutInput{UserID: "alice", MimeType: "application/pdf", Body: []byte("%PDF-x")}); !errors.Is(err, ErrBudget) {
		t.Fatal(err)
	}
}

func TestR2MigrationResume(t *testing.T) {
	r := testR2(t)
	ctx := context.Background()
	r.DB.ReconcileR2(ctx, r.Now(), 0, nil)
	calls := 0
	var keys []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "DELETE" {
			w.WriteHeader(503)
			return
		}
		calls++
		keys = append(keys, req.URL.Path)
		if calls == 1 {
			w.WriteHeader(503)
		}
	}))
	defer fake.Close()
	r.Endpoint = fake.URL
	in := PutInput{UserID: "operator-migration", MigrationID: "internal-source-hash", MimeType: "application/pdf", Body: []byte("%PDF-x")}
	if _, err := r.Put(ctx, in); err == nil {
		t.Fatal("expected ambiguous failure")
	}
	saved, err := r.Put(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	again, err := r.Put(ctx, in)
	if err != nil || saved.Key != again.Key || calls != 2 || keys[0] != keys[1] {
		t.Fatal("migration replay changed key or sent another request", again, err, calls, keys)
	}
	u, _ := r.DB.R2Usage(ctx, r.Now())
	if u.Bytes != 6 || u.ClassA != 2 {
		t.Fatal("migration retry miscounted", u)
	}
}

func TestR2WarningRetriesUntilDelivered(t *testing.T) {
	r := testR2(t)
	ctx := context.Background()
	r.DB.ReconcileR2(ctx, r.Now(), 7_000_000_000, nil)
	calls := 0
	r.Alert = func(context.Context, string) bool { calls++; return calls > 1 }
	now := r.Now()
	r.Now = func() time.Time { return now }
	for i := 0; i < 4; i++ {
		if err := r.reserve(ctx, "check", "", "", 0); err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			now = now.Add(16 * time.Minute)
		}
	}
	if calls != 2 {
		t.Fatal("warning was lost or repeated after acceptance", calls)
	}
}

func TestR2RequestKeepsContextCancellation(t *testing.T) {
	r := testR2(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.request(ctx, "GET", "files/test", nil, nil, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// Run against a real local S3-compatible service: see scripts/test-r2.ps1.
func TestR2Minio(t *testing.T) {
	endpoint := os.Getenv("R2_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("local MinIO not requested")
	}
	r := testR2(t)
	r.Endpoint = endpoint
	r.Bucket = "receipts"
	r.AccessKey = "r2-test-access"
	r.Secret = "r2-test-secret-only"
	// Only the fake's setup creates a bucket; production never gets bucket-admin permission.
	req, _ := http.NewRequest("PUT", endpoint+"/receipts", nil)
	// MinIO enforces clock skew, so the integration uses the current clock.
	r.Now = time.Now
	signV4(req, nil, r.AccessKey, r.Secret, "auto", r.Now())
	res, err := r.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 && res.StatusCode != 409 {
		t.Fatal(res.Status)
	}
	ctx := context.Background()
	if err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	before, _ := r.DB.R2Usage(ctx, r.Now())
	in := PutInput{UserID: "alice", MimeType: "application/pdf", Body: []byte("%PDF-1.4\nlocal test")}
	saved, err := r.Put(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	// The listed key must match the ledger key, or the object would count twice.
	if err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if listed, _ := r.DB.R2Usage(ctx, r.Now()); listed.Bytes != before.Bytes+int64(len(in.Body)) {
		t.Fatal("inventory did not match the reserved key", listed, before)
	}
	f, err := r.Get(ctx, saved.Key)
	if err != nil || f == nil || string(f.Body) != string(in.Body) {
		t.Fatalf("round trip: %v %v", f, err)
	}
	if err := r.Delete(ctx, saved.Key); err != nil {
		t.Fatal(err)
	}
	after, err := r.DB.R2Usage(ctx, r.Now())
	// One PUT plus the extra scan's object page and multipart check.
	if err != nil || after.Bytes != before.Bytes || after.ClassA != before.ClassA+3 || after.ClassB != before.ClassB+1 {
		t.Fatal(fmt.Sprint(after, err))
	}
}

// N1: a PUT whose outcome is unknown (timeout or client hang-up) stays reserved
// while it could still land, then is released by an inventory that proves the
// key absent once the pending window has passed.
func TestR2AmbiguousPutReleasedByLaterInventory(t *testing.T) {
	r := testR2(t)
	ctx := context.Background()
	now := r.Now()
	r.Now = func() time.Time { return now }
	r.DB.ReconcileR2(ctx, now, 0, nil)
	release := make(chan struct{})
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodPut:
			select {
			case <-release:
			case <-req.Context().Done():
			}
		case req.URL.Query().Has("uploads"):
			io.WriteString(w, `<ListMultipartUploadsResult></ListMultipartUploadsResult>`)
		default:
			io.WriteString(w, `<ListBucketResult></ListBucketResult>`)
		}
	}))
	defer fake.Close()
	defer close(release)
	r.Endpoint = fake.URL
	r.HTTP.Timeout = 50 * time.Millisecond
	if _, err := r.Put(ctx, PutInput{UserID: "alice", MimeType: "application/pdf", Body: []byte("%PDF-x")}); err == nil {
		t.Fatal("expected transport timeout")
	}
	r.HTTP.Timeout = 5 * time.Second
	cancelled, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := r.Put(cancelled, PutInput{UserID: "alice", MimeType: "application/pdf", Body: []byte("%PDF-y")}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("expected client hang-up", err)
	}
	if u, _ := r.DB.R2Usage(ctx, now); u.Bytes != 12 {
		t.Fatal("ambiguous PUTs must stay reserved", u)
	}
	now = now.Add(10 * time.Minute)
	if err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if u, _ := r.DB.R2Usage(ctx, now); u.Bytes != 12 {
		t.Fatal("inventory inside the pending window released a possibly landing PUT", u)
	}
	now = now.Add(store.R2PendingWindow)
	if err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if u, _ := r.DB.R2Usage(ctx, now); u.Bytes != 0 {
		t.Fatal("stale reservations were never released", u)
	}
}

// Startup no longer touches R2. The background scan alerts on rejected
// credentials and keeps uploads closed instead of stopping the dashboard.
func TestR2DailyAlertsOnRejectedCredentials(t *testing.T) {
	r := testR2(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.DB.ReconcileR2(ctx, r.Now(), 0, nil)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(403) }))
	defer fake.Close()
	r.Endpoint = fake.URL
	reasons := make(chan string, 4)
	r.Alert = func(_ context.Context, reason string) bool { reasons <- reason; return true }
	errs := make(chan error, 4)
	done := make(chan struct{})
	go func() { r.Daily(ctx, func(err error) { errs <- err }); close(done) }()
	select {
	case err := <-errs:
		if !errors.Is(err, ErrR2Credentials) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first inventory did not run immediately")
	}
	select {
	case reason := <-reasons:
		if !strings.Contains(reason, "rejected") {
			t.Fatal(reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("credential failure was not alerted")
	}
	cancel()
	<-done
	if _, err := r.Put(context.Background(), PutInput{UserID: "alice", MimeType: "application/pdf", Body: []byte("%PDF-x")}); !errors.Is(err, ErrBudget) {
		t.Fatal("failed inventory left uploads open", err)
	}
}

func TestR2AlertReasonNamesRefusals(t *testing.T) {
	r := testR2(t)
	ctx := context.Background()
	now := r.Now()
	r.DB.ReconcileR2(ctx, now, 0, nil)
	r.DB.W.Exec(`INSERT INTO r2_day VALUES(?,900000,0)`, now.Format("2006-01-02"))
	var reason string
	r.Alert = func(_ context.Context, got string) bool { reason = got; return true }
	if _, err := r.Put(ctx, PutInput{UserID: "alice", MimeType: "application/pdf", Body: []byte("%PDF-x")}); !errors.Is(err, ErrBudget) {
		t.Fatal(err)
	}
	if !strings.Contains(reason, "90% of Class A operations") || !strings.Contains(reason, "refused") {
		t.Fatal("90% refusal was not alerted", reason)
	}
	if got := AlertReason(store.R2AlertStorageWarn | store.R2AlertClassBWarn); !strings.Contains(got, "70% of storage, Class B operations") {
		t.Fatal(got)
	}
}
