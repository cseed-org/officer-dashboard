package files

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"officerdashboard/internal/store"
)

// R2Analytics reads this bucket's usage from Cloudflare's free GraphQL
// Analytics API with a read-only Account Analytics token. It is only used to
// re-seed the ledger after a restore; the guard itself never depends on it.
type R2Analytics struct {
	Endpoint, Account, Bucket, Token string
	HTTP                             *http.Client
}

func NewR2Analytics(account, bucket, token string) *R2Analytics {
	return &R2Analytics{Endpoint: "https://api.cloudflare.com/client/v4/graphql", Account: account, Bucket: bucket, Token: token,
		HTTP: &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

var errR2Analytics = errors.New("Cloudflare R2 analytics unavailable")

// A dataset query may span at most 31 days, so the guard's rolling window
// (current UTC day plus the 31 before it) is read as its oldest day plus the rest.
const r2AnalyticsQuery = `query($account: string!, $bucket: string!, $month: Time!, $oldest: Time!, $rolling: Time!, $day: Time!, $now: Time!) {
  viewer { accounts(filter: {accountTag: $account}) {
    month: r2OperationsAdaptiveGroups(limit: 1000, filter: {bucketName: $bucket, datetime_geq: $month, datetime_leq: $now}) { sum { requests } dimensions { actionType } }
    oldest: r2OperationsAdaptiveGroups(limit: 1000, filter: {bucketName: $bucket, datetime_geq: $oldest, datetime_lt: $rolling}) { sum { requests } dimensions { actionType } }
    rolling: r2OperationsAdaptiveGroups(limit: 1000, filter: {bucketName: $bucket, datetime_geq: $rolling, datetime_leq: $now}) { sum { requests } dimensions { actionType } }
    storage: r2StorageAdaptiveGroups(limit: 1, filter: {bucketName: $bucket, datetime_geq: $day, datetime_leq: $now}, orderBy: [datetime_DESC]) { max { payloadSize metadataSize } }
  } }
}`

// Operations Cloudflare does not bill. Class B is the documented read set;
// any other or unknown operation counts as Class A, the smaller allowance.
var r2FreeActions = map[string]bool{"DeleteObject": true, "DeleteBucket": true, "AbortMultipartUpload": true}
var r2ClassBActions = map[string]bool{"HeadBucket": true, "HeadObject": true, "GetObject": true, "UsageSummary": true, "GetBucketEncryption": true, "GetBucketLocation": true, "GetBucketCors": true, "GetBucketLifecycleConfiguration": true}

type r2OpGroup struct {
	Sum        struct{ Requests int64 }
	Dimensions struct{ ActionType string }
}

func r2Classes(groups []r2OpGroup) (a, b int64, err error) {
	if len(groups) >= 1000 {
		return 0, 0, errR2Analytics // possibly truncated
	}
	for _, g := range groups {
		switch n := g.Sum.Requests; {
		case n < 0:
			return 0, 0, errR2Analytics
		case r2FreeActions[g.Dimensions.ActionType]:
		case r2ClassBActions[g.Dimensions.ActionType]:
			b += n
		default:
			a += n
		}
	}
	return a, b, nil
}

// Usage returns this month's and the rolling window's Class A/B counts and
// the latest stored bytes. Errors never include the token or the response.
func (a *R2Analytics) Usage(ctx context.Context, now time.Time) (store.R2Seed, error) {
	now = now.UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	vars := map[string]string{
		"account": a.Account, "bucket": a.Bucket, "now": now.Format(time.RFC3339),
		"month":   time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		"oldest":  day.AddDate(0, 0, -31).Format(time.RFC3339),
		"rolling": day.AddDate(0, 0, -30).Format(time.RFC3339),
		"day":     day.AddDate(0, 0, -1).Format(time.RFC3339),
	}
	body, err := json.Marshal(map[string]any{"query": r2AnalyticsQuery, "variables": vars})
	if err != nil {
		return store.R2Seed{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.Endpoint, bytes.NewReader(body))
	if err != nil {
		return store.R2Seed{}, errR2Analytics
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := a.HTTP.Do(req)
	if err != nil {
		return store.R2Seed{}, errR2Analytics
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return store.R2Seed{}, errR2Analytics
	}
	var out struct {
		Data struct {
			Viewer struct {
				Accounts []struct {
					Month, Oldest, Rolling []r2OpGroup
					Storage                []struct {
						Max struct{ PayloadSize, MetadataSize int64 }
					}
				}
			}
		}
		Errors []json.RawMessage
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out); err != nil || len(out.Errors) > 0 || len(out.Data.Viewer.Accounts) != 1 {
		return store.R2Seed{}, errR2Analytics
	}
	acct := out.Data.Viewer.Accounts[0]
	var seed store.R2Seed
	if seed.MonthA, seed.MonthB, err = r2Classes(acct.Month); err != nil {
		return store.R2Seed{}, err
	}
	if seed.RollingA, seed.RollingB, err = r2Classes(acct.Rolling); err != nil {
		return store.R2Seed{}, err
	}
	oldA, oldB, err := r2Classes(acct.Oldest)
	if err != nil {
		return store.R2Seed{}, err
	}
	seed.RollingA, seed.RollingB = seed.RollingA+oldA, seed.RollingB+oldB
	for _, s := range acct.Storage {
		if s.Max.PayloadSize < 0 || s.Max.MetadataSize < 0 {
			return store.R2Seed{}, errR2Analytics
		}
		seed.Bytes = max(seed.Bytes, s.Max.PayloadSize+s.Max.MetadataSize)
	}
	return seed, nil
}
