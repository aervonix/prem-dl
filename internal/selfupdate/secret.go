package selfupdate

// buildSecret returns the build secret of a release build. secret_release.go, which the release
// scripts write for the build (go run ./cmd/buildsecret) and delete after it, sets it; it is
// gitignored and never committed. A copy built from source has none, and doesn't check for
// updates.
var buildSecret func() string

// BuildSecret is this build's secret, or "" when it was built from source.
func BuildSecret() string {
	if buildSecret == nil {
		return ""
	}
	return buildSecret()
}
