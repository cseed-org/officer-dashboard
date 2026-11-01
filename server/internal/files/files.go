// Package files stores receipts: Google Drive (shared drive) in production, local disk in dev.
package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"officerdashboard/internal/auth"
)

const MaxBytes = 10 * 1024 * 1024

// AllowedMIME are the receipt types accepted by POST /api/requests/:id/attachments.
var AllowedMIME = []string{"application/pdf", "image/png", "image/jpeg", "image/heic", "image/webp"}

// ErrUnavailable means no receipt storage is configured (the API answers 503).
var ErrUnavailable = errors.New("Receipt storage requires Google Drive credentials and DRIVE_ROOT_FOLDER_ID")

// ErrInvalidUpload is returned by ValidateUpload; the API answers 400.
var ErrInvalidUpload = errors.New("invalid upload")

// ValidateUpload checks size and MIME type.
func ValidateUpload(mimeType string, size int64) error {
	if size > MaxBytes || size == 0 {
		return wrap("File must be 1 byte to 10 MB")
	}
	ok := false
	for _, m := range AllowedMIME {
		if m == mimeType {
			ok = true
		}
	}
	if !ok {
		return wrap("Unsupported receipt MIME type")
	}
	return nil
}

type uploadError string

func wrap(msg string) error { return uploadError(msg) }

func (e uploadError) Error() string        { return string(e) }
func (e uploadError) Is(target error) bool { return target == ErrInvalidUpload }

// PutInput is one file to store under a logical folder path, e.g. ["FY2027", "<request id>"].
type PutInput struct {
	Folder   []string
	Filename string
	MimeType string
	Body     []byte
}

// Stored describes a stored file.
type Stored struct {
	Storage     string // "drive" | "local"
	Key         string
	WebViewLink string // "" when there is none
	SizeBytes   int64
}

// File is a downloaded file.
type File struct {
	Body     []byte
	MimeType string
}

// Store is the receipt storage port.
type Store interface {
	Put(ctx context.Context, in PutInput) (Stored, error)
	// Get returns nil, nil when the key is unknown.
	Get(ctx context.Context, key string) (*File, error)
}

// Local keeps receipts on disk: <root>/<uuid> plus <uuid>.json with the MIME type.
type Local struct{ Root string }

var keyRE = regexp.MustCompile(`^[a-f0-9-]{36}$`)

func (l Local) Put(_ context.Context, in PutInput) (Stored, error) {
	if err := ValidateUpload(in.MimeType, int64(len(in.Body))); err != nil {
		return Stored{}, err
	}
	if err := os.MkdirAll(l.Root, 0o750); err != nil {
		return Stored{}, err
	}
	key := auth.UUID()
	if err := os.WriteFile(filepath.Join(l.Root, key), in.Body, 0o640); err != nil {
		return Stored{}, err
	}
	meta := `{"mimeType":"` + strings.ReplaceAll(in.MimeType, `"`, "") + `"}`
	if err := os.WriteFile(filepath.Join(l.Root, key+".json"), []byte(meta), 0o640); err != nil {
		return Stored{}, err
	}
	return Stored{Storage: "local", Key: key, SizeBytes: int64(len(in.Body))}, nil
}

func (l Local) Get(_ context.Context, key string) (*File, error) {
	if !keyRE.MatchString(key) {
		return nil, nil
	}
	body, err := os.ReadFile(filepath.Join(l.Root, key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	meta, err := os.ReadFile(filepath.Join(l.Root, key+".json"))
	if err != nil {
		return nil, err
	}
	mt := "application/octet-stream"
	if m := regexp.MustCompile(`"mimeType":"([^"]*)"`).FindSubmatch(meta); m != nil {
		mt = string(m[1])
	}
	return &File{Body: body, MimeType: mt}, nil
}

// Unavailable is the store used when nothing is configured: every call fails with ErrUnavailable.
type Unavailable struct{}

func (Unavailable) Put(context.Context, PutInput) (Stored, error) { return Stored{}, ErrUnavailable }
func (Unavailable) Get(context.Context, string) (*File, error)    { return nil, ErrUnavailable }
