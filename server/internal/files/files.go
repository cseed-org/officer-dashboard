// Package files stores receipts: Google Drive (shared drive) in production, local disk in dev.
package files

import (
	"bytes"
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
	// Delete removes a stored file (used to clean up an upload whose database write failed).
	// Deleting an unknown key is not an error.
	Delete(ctx context.Context, key string) error
}

// Local keeps receipts on disk: <root>/<uuid> plus <uuid>.json with the MIME type.
type Local struct{ Root string }

var (
	keyRE  = regexp.MustCompile(`^[a-f0-9-]{36}$`)
	mimeRE = regexp.MustCompile(`"mimeType":"([^"]*)"`)
)

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
	if m := mimeRE.FindSubmatch(meta); m != nil {
		mt = string(m[1])
	}
	return &File{Body: body, MimeType: mt}, nil
}

func (l Local) Delete(_ context.Context, key string) error {
	if !keyRE.MatchString(key) {
		return nil
	}
	for _, name := range []string{key, key + ".json"} {
		if err := os.Remove(filepath.Join(l.Root, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// CheckContent verifies that the bytes look like the declared MIME type (magic numbers), so
// an HTML payload labelled application/pdf is refused instead of stored.
func CheckContent(mimeType string, body []byte) error {
	ok := false
	switch mimeType {
	case "application/pdf":
		ok = bytes.HasPrefix(body, []byte("%PDF-"))
	case "image/png":
		ok = bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n"))
	case "image/jpeg":
		ok = bytes.HasPrefix(body, []byte{0xff, 0xd8, 0xff})
	case "image/webp":
		ok = len(body) >= 12 && string(body[:4]) == "RIFF" && string(body[8:12]) == "WEBP"
	case "image/heic":
		if len(body) >= 12 && string(body[4:8]) == "ftyp" {
			switch string(body[8:12]) {
			case "heic", "heix", "hevc", "hevx", "heim", "heis", "mif1", "msf1":
				ok = true
			}
		}
	}
	if !ok {
		return wrap("File content does not match its type")
	}
	return nil
}

// Unavailable is the store used when nothing is configured: every call fails with ErrUnavailable.
type Unavailable struct{}

func (Unavailable) Put(context.Context, PutInput) (Stored, error) { return Stored{}, ErrUnavailable }
func (Unavailable) Get(context.Context, string) (*File, error)    { return nil, ErrUnavailable }
func (Unavailable) Delete(context.Context, string) error          { return ErrUnavailable }
