package api

// Optional defense in depth behind Cloudflare Access. Application sessions and
// bearer authorization remain mandatory; an Access identity never becomes a user.
import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

var accessDomain = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.cloudflareaccess\.com$`)

// AccessVerifier verifies only RS256 JWTs issued by the configured Access team.
// Signing keys come from a fixed HTTPS endpoint, never from a token's jku/x5u.
type AccessVerifier struct {
	issuer, audience string
	client           *http.Client
	mu               sync.Mutex
	keys             map[string]*rsa.PublicKey
	fetched          time.Time
	attempted        time.Time
}

func NewAccessVerifier(domain, audience string) (*AccessVerifier, error) {
	if domain == "" && audience == "" {
		return nil, nil
	}
	if !accessDomain.MatchString(domain) || audience == "" {
		return nil, fmt.Errorf("CLOUDFLARE_ACCESS_TEAM_DOMAIN must be a team.cloudflareaccess.com hostname and CLOUDFLARE_ACCESS_AUD must be set together")
	}
	return &AccessVerifier{issuer: "https://" + domain, audience: audience,
		client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (v *AccessVerifier) key(ctx context.Context, kid string, now time.Time) *rsa.PublicKey {
	v.mu.Lock()
	defer v.mu.Unlock()
	if now.Sub(v.fetched) < time.Hour && v.keys[kid] != nil {
		return v.keys[kid]
	}
	// Unknown key IDs cannot turn requests into an unbounded JWKS fetch flood.
	if !v.attempted.IsZero() && now.Sub(v.attempted) < time.Minute {
		return nil
	}
	v.attempted = now
	req, err := http.NewRequestWithContext(ctx, "GET", v.issuer+"/cdn-cgi/access/certs", nil)
	if err != nil {
		return nil
	}
	res, err := v.client.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (256<<10)+1))
	if err != nil || len(raw) > 256<<10 {
		return nil
	}
	var doc struct {
		Keys []struct{ Kty, Kid, N, E, Alg, Use string }
	}
	if json.Unmarshal(raw, &doc) != nil || len(doc.Keys) > 32 {
		return nil
	}
	keys := map[string]*rsa.PublicKey{}
	for _, j := range doc.Keys {
		if j.Kty != "RSA" || (j.Alg != "" && j.Alg != "RS256") || (j.Use != "" && j.Use != "sig") {
			continue
		}
		n, e1 := base64.RawURLEncoding.DecodeString(j.N)
		e, e2 := base64.RawURLEncoding.DecodeString(j.E)
		if e1 != nil || e2 != nil || len(n) < 256 || len(n) > 1024 || len(e) > 4 {
			continue
		}
		exponent := new(big.Int).SetBytes(e).Int64()
		if exponent < 3 || exponent > 1<<31-1 || exponent%2 == 0 {
			continue
		}
		keys[j.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(exponent)}
	}
	v.keys, v.fetched = keys, now
	return keys[kid]
}

func (v *AccessVerifier) verify(ctx context.Context, token string, now time.Time) error {
	invalid := errors.New("invalid Access assertion")
	if len(token) > 16<<10 {
		return invalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return invalid
	}
	decode := func(s string, out any) error {
		b, err := base64.RawURLEncoding.DecodeString(s)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, out)
	}
	var header struct {
		Alg, Kid string
		Crit     []string
	}
	if decode(parts[0], &header) != nil || header.Alg != "RS256" || header.Kid == "" || len(header.Crit) > 0 {
		return invalid
	}
	var claims struct {
		Iss string
		Aud json.RawMessage
		Exp int64
		Nbf int64
		Iat int64
	}
	if decode(parts[1], &claims) != nil || claims.Iss != v.issuer || claims.Exp <= now.Unix() || claims.Nbf > now.Unix() || claims.Iat > now.Unix() {
		return invalid
	}
	var audiences []string
	if json.Unmarshal(claims.Aud, &audiences) != nil {
		var aud string
		if json.Unmarshal(claims.Aud, &aud) != nil {
			return invalid
		}
		audiences = []string{aud}
	}
	match := false
	for _, aud := range audiences {
		if aud == v.audience {
			match = true
		}
	}
	if !match {
		return invalid
	}
	key := v.key(ctx, header.Kid, now)
	if key == nil {
		return invalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return invalid
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) != nil {
		return invalid
	}
	return nil
}

// Access bypasses match the agent paths in docs/deploy/cloudflare.md. Health is
// also exempt for the local container probe. Prefix lookalikes are never exempt.
func accessBypass(path string) bool {
	p := strings.TrimSuffix(path, "/")
	switch p {
	case "/api/health", "/mcp", "/oauth/register", "/oauth/token", "/.well-known/oauth-authorization-server", "/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp":
		return true
	}
	return false
}
func (s *Server) withAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := s.cfg.Access; v != nil && !accessBypass(r.URL.Path) {
			if err := v.verify(r.Context(), r.Header.Get("Cf-Access-Jwt-Assertion"), s.now()); err != nil {
				w.Header().Set("Cache-Control", "no-store")
				writeError(w, fail(401, "unauthenticated", "Cloudflare Access authentication required"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
