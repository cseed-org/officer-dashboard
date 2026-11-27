package plan

import (
	"path"
	"strings"
)

// Access destination coverage, from
// https://developers.cloudflare.com/cloudflare-one/access-controls/policies/app-paths/:
//   - "To protect an apex domain and all of the paths under it, leave the Path field empty."
//   - "A path covers everything below it" (docs/deploy/cloudflare.md §5, matching the page's
//     "If no separate, specific rule is set for dashboard.com/eng/exec, it will inherit any
//     rules set for dashboard.com/eng").
//   - "A wildcard in the Subdomain field only matches that specific subdomain level."
//     "It does not cover the apex domain or multiple levels of the subdomain."
//     Examples: `*.example.com` covers alpha.example.com, not example.com or
//     foo.bar.example.com; `*test.example.com` covers test.example.com and alphatest.example.com.
//   - "Using a wildcard in the Path field does not cover the parent path nor the apex
//     domain." "Using a wildcard in the middle of the Path field covers multiple segments
//     of the URL." Examples: `example.com/*` covers example.com; `example.com/alpha/*` does
//     not cover example.com/alpha.
//
// The page does not say which application decides when a wildcard application and a
// specific one cover the same hostname or path. Setup therefore treats any wildcard that
// may cover a destination as a conflict, and path wildcards are matched conservatively:
// everything after the first `*` is assumed to match anything.

// IsWildcard reports whether an Access destination contains a wildcard.
func IsWildcard(dest string) bool { return strings.Contains(dest, "*") }

func splitDest(d string) (host, p string) {
	host, p, _ = strings.Cut(NormalizeDestination(d), "/")
	return host, p
}

// hostMatches reports whether host pattern pat (which may contain `*` within a label)
// matches the literal host. Each label matches one label.
func hostMatches(pat, host string) bool {
	pl, hl := strings.Split(pat, "."), strings.Split(host, ".")
	if len(pl) != len(hl) {
		return false
	}
	for i := range pl {
		if ok, err := path.Match(pl[i], hl[i]); err != nil || !ok {
			return false
		}
	}
	return true
}

// hostsMayMatch reports whether two host patterns can name the same host.
func hostsMayMatch(a, b string) bool { return hostMatches(a, b) || hostMatches(b, a) }

// pathCovers reports whether path pattern pat covers the path p (both without the
// leading slash; "" is the whole host). A pattern with `*` covers anything that starts
// with the text before its first `*`.
func pathCovers(pat, p string) bool {
	if pat == "" {
		return true
	}
	if i := strings.IndexByte(pat, '*'); i >= 0 {
		return strings.HasPrefix(p, pat[:i])
	}
	return p == pat || strings.HasPrefix(p, pat+"/")
}

// DestinationOnHost reports whether an Access destination may cover any page of host,
// including through a wildcard subdomain.
func DestinationOnHost(dest, host string) bool {
	h, _ := splitDest(dest)
	return hostsMayMatch(h, host)
}

// DestinationsOverlap reports whether two Access destinations may decide access to some
// of the same requests: one covers the other, or a wildcard may cover either.
func DestinationsOverlap(a, b string) bool {
	ah, ap := splitDest(a)
	bh, bp := splitDest(b)
	if !hostsMayMatch(ah, bh) {
		return false
	}
	return pathCovers(ap, bp) || pathCovers(bp, ap)
}
