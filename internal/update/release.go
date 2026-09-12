package update

// releaseRepo is the GitHub repository this build takes its releases from.
//
// The default is the fork that carries the Windows port, because upstream
// publishes no Windows assets and `clother update` would otherwise 404 there.
// The release workflow overrides it with
//
//	-ldflags "-X github.com/jolehuit/clother/internal/update.releaseRepo=$GITHUB_REPOSITORY"
//
// so that a fork of the fork fetches from itself rather than from here, and a
// local `go build` still lands on the right place without any flags. The
// runtime overrides CLOTHER_RELEASE_BASE_URL and CLOTHER_UPDATE_URL win over
// all of it.
var releaseRepo = "starbuck100/clother"

// releasesBaseURL is the root of this repository's releases on GitHub.
func releasesBaseURL() string {
	return "https://github.com/" + releaseRepo + "/releases"
}
