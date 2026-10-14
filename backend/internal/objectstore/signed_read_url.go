package objectstore

import (
	"net/netip"
	"net/url"
	"strings"
)

// SignedReadVersion checks the transport envelope of a versioned object URL.
// This does not authenticate its signature or authorize access to the object.
func SignedReadVersion(raw string) (string, bool) {
	if len(raw) == 0 || len(raw) > 16384 || strings.ContainsAny(raw, "\r\n\x00") {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", false
	}
	if parsed.Scheme != "https" {
		if parsed.Scheme != "http" {
			return "", false
		}
		host := parsed.Hostname()
		address, parseErr := netip.ParseAddr(host)
		if host != "localhost" && (parseErr != nil || !address.IsLoopback()) {
			return "", false
		}
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return "", false
	}
	versions := query["versionId"]
	if len(versions) != 1 || versions[0] == "" || versions[0] == "null" {
		return "", false
	}
	return versions[0], true
}

func ValidSignedReadURL(raw, version string) bool {
	actual, ok := SignedReadVersion(raw)
	return ok && version != "" && version != "null" && actual == version
}
