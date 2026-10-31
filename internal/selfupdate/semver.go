package selfupdate

import (
	"regexp"
	"strings"
)

// Versions compare by Semantic Versioning 2.0.0 precedence, as the release server does:
// a pre-release ranks below its release, and build metadata is ignored.
// testdata/semver-corpus.json is the server's own test corpus.

var semverRe = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

// maxVersion is the server's limit on a version's length.
const maxVersion = 64

// ValidVersion reports whether v is a semantic version ("1.4.0", "1.4.0-beta.2").
func ValidVersion(v string) bool { return len(v) <= maxVersion && semverRe.MatchString(v) }

// Compare returns -1, 0 or 1 as a ranks below, with, or above b. Invalid versions rank below
// every valid one (and equal to each other).
func Compare(a, b string) int {
	var ma, mb []string
	if ValidVersion(a) {
		ma = semverRe.FindStringSubmatch(a)
	}
	if ValidVersion(b) {
		mb = semverRe.FindStringSubmatch(b)
	}
	switch {
	case ma == nil && mb == nil:
		return 0
	case ma == nil:
		return -1
	case mb == nil:
		return 1
	}
	for i := 1; i <= 3; i++ {
		if c := cmpNum(ma[i], mb[i]); c != 0 {
			return c
		}
	}
	pa, pb := ma[4], mb[4]
	switch {
	case pa == "" && pb == "":
		return 0
	case pa == "":
		return 1 // a release ranks above its pre-releases
	case pb == "":
		return -1
	}
	xa, xb := strings.Split(pa, "."), strings.Split(pb, ".")
	for i := 0; i < len(xa) && i < len(xb); i++ {
		na, aNum := isNum(xa[i])
		nb, bNum := isNum(xb[i])
		switch {
		case aNum && bNum:
			if c := cmpNum(na, nb); c != 0 {
				return c
			}
		case aNum:
			return -1 // numeric identifiers rank below alphanumeric ones
		case bNum:
			return 1
		default:
			if c := strings.Compare(xa[i], xb[i]); c != 0 {
				return c
			}
		}
	}
	return cmpInt(len(xa), len(xb))
}

// Newer reports whether latest ranks above current.
func Newer(current, latest string) bool { return ValidVersion(latest) && Compare(latest, current) > 0 }

func isNum(s string) (string, bool) {
	for _, r := range s {
		if r < '0' || r > '9' {
			return s, false
		}
	}
	return s, s != ""
}

// cmpNum compares decimal strings of any length (no leading zeros, as semverRe ensures).
func cmpNum(a, b string) int {
	if c := cmpInt(len(a), len(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
