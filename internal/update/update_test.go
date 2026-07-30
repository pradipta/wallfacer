package update_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pradipta/wallfacer/internal/update"
)

// stubGitHub serves a /releases/latest payload and counts requests so tests can
// assert the cache is doing its job.
func stubGitHub(t *testing.T, body map[string]any) (base string, hits *int32) {
	t.Helper()
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		if got, want := r.URL.Path, "/repos/pradipta/wallfacer/releases/latest"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &n
}

func release(tag string) map[string]any {
	return map[string]any{
		"tag_name": tag,
		"html_url": "https://github.com/pradipta/wallfacer/releases/tag/" + tag,
	}
}

// cfg builds a Config wired to a stub server with a temp cache dir.
func cfg(t *testing.T, current, base string) update.Config {
	t.Helper()
	// Make sure a developer's own environment cannot flip these tests.
	t.Setenv(update.EnvDisable, "")
	t.Setenv(update.EnvAPIBase, "")
	t.Setenv(update.EnvInterval, "")
	return update.Config{
		Current:  current,
		CacheDir: t.TempDir(),
		APIBase:  base,
	}
}

// check runs a full check the way the browser does: resolve from the cache,
// then look GitHub up and wait for the answer.
func check(t *testing.T, c update.Config) *update.Notice {
	t.Helper()
	chk := update.Start(c)
	chk.Refresh()
	return chk.Await()
}

func TestNoticeWhenNewerReleaseExists(t *testing.T) {
	base, _ := stubGitHub(t, release("v0.2.0"))
	n := check(t, cfg(t, "v0.1.0", base))
	if n == nil {
		t.Fatal("expected a notice, got nil")
	}
	if n.Latest != "v0.2.0" || n.Current != "v0.1.0" {
		t.Errorf("notice = %+v", n)
	}
	if n.URL == "" {
		t.Error("notice URL is empty")
	}
	if want := "update available: v0.1.0 → v0.2.0"; n.Line() != want {
		t.Errorf("Line() = %q, want %q", n.Line(), want)
	}
}

func TestNoNoticeWhenUpToDateOrAhead(t *testing.T) {
	for _, current := range []string{"v0.2.0", "v0.3.0", "v1.0.0"} {
		base, _ := stubGitHub(t, release("v0.2.0"))
		if n := check(t, cfg(t, current, base)); n != nil {
			t.Errorf("current %s: unexpected notice %+v", current, n)
		}
	}
}

// A pre-release build should be told about the stable release that supersedes it.
func TestPrereleaseCurrentGetsNotified(t *testing.T) {
	base, _ := stubGitHub(t, release("v0.2.0"))
	if n := check(t, cfg(t, "v0.2.0-rc.1", base)); n == nil {
		t.Fatal("expected rc build to be told about v0.2.0")
	}
}

// Dev and pseudo-version builds must stay quiet, and must not even hit the API.
// `git describe` versions matter most here: v1.1.0-3-gabc1234 is *ahead* of
// v1.1.0 in reality but behind it in semver, so comparing it would tell a
// contributor to "upgrade" to an older release.
func TestDevBuildsAreQuiet(t *testing.T) {
	for _, current := range []string{
		"dev", "", "garbage",
		"v0.0.0-20240101120000-abcdef123456", // go install of an untagged commit
		"v1.1.0-3-gabc1234",                  // make build, 3 commits past the tag
		"v1.1.0-3-gabc1234-dirty",            // …with uncommitted changes
		"v1.1.0-dirty",                       // on the tag, uncommitted changes
		"v1.2.0-rc.1-dirty",                  // on an rc tag, uncommitted changes
	} {
		base, hits := stubGitHub(t, release("v9.9.9"))
		if n := check(t, cfg(t, current, base)); n != nil {
			t.Errorf("current %q: unexpected notice %+v", current, n)
		}
		if *hits != 0 {
			t.Errorf("current %q: made %d API calls, want 0", current, *hits)
		}
	}
}

// A clean pre-release tag is a real, installable version, so it still checks.
func TestCleanPrereleaseTagStillChecks(t *testing.T) {
	base, hits := stubGitHub(t, release("v9.9.9"))
	if n := check(t, cfg(t, "v1.2.0-rc.1", base)); n == nil {
		t.Error("expected a clean rc build to be told about v9.9.9")
	}
	if *hits != 1 {
		t.Errorf("made %d API calls, want 1", *hits)
	}
}

func TestDisableEnvSkipsCheck(t *testing.T) {
	base, hits := stubGitHub(t, release("v9.9.9"))
	c := cfg(t, "v0.1.0", base)
	t.Setenv(update.EnvDisable, "1")
	if n := check(t, c); n != nil {
		t.Errorf("unexpected notice %+v", n)
	}
	if *hits != 0 {
		t.Errorf("made %d API calls, want 0", *hits)
	}
}

func TestCacheAvoidsSecondFetch(t *testing.T) {
	base, hits := stubGitHub(t, release("v0.2.0"))
	c := cfg(t, "v0.1.0", base)
	for i := 0; i < 3; i++ {
		if n := check(t, c); n == nil {
			t.Fatalf("run %d: expected a notice", i)
		}
	}
	if *hits != 1 {
		t.Errorf("made %d API calls, want 1 (cache should absorb the rest)", *hits)
	}
	if _, err := os.Stat(filepath.Join(c.CacheDir, "update-check.json")); err != nil {
		t.Errorf("cache file not written: %v", err)
	}
}

func TestStaleCacheRefetches(t *testing.T) {
	base, hits := stubGitHub(t, release("v0.2.0"))
	c := cfg(t, "v0.1.0", base)
	if check(t, c) == nil {
		t.Fatal("expected a notice")
	}
	// Pretend the cached answer is two days old.
	c.Now = func() time.Time { return time.Now().Add(48 * time.Hour) }
	if check(t, c) == nil {
		t.Fatal("expected a notice after cache expiry")
	}
	if *hits != 2 {
		t.Errorf("made %d API calls, want 2", *hits)
	}
}

// Interval 0 (WALLFACER_UPDATE_INTERVAL=0) is the "check every run" mode used
// for manual testing.
func TestZeroIntervalAlwaysFetches(t *testing.T) {
	base, hits := stubGitHub(t, release("v0.2.0"))
	c := cfg(t, "v0.1.0", base)
	t.Setenv(update.EnvInterval, "0")
	c.Interval = 0 // force the env override to be consulted
	for i := 0; i < 2; i++ {
		if check(t, c) == nil {
			t.Fatalf("run %d: expected a notice", i)
		}
	}
	if *hits != 2 {
		t.Errorf("made %d API calls, want 2", *hits)
	}
}

func TestPrereleaseAndDraftPayloadsIgnored(t *testing.T) {
	for _, field := range []string{"prerelease", "draft"} {
		body := release("v9.9.9")
		body[field] = true
		base, _ := stubGitHub(t, body)
		if n := check(t, cfg(t, "v0.1.0", base)); n != nil {
			t.Errorf("%s payload produced a notice: %+v", field, n)
		}
	}
}

// Every server-side failure mode must degrade to "no notice", never an error or
// a hang.
func TestServerFailuresAreSilent(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"rate limited": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "rate limit exceeded", http.StatusForbidden)
		},
		"not found": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		},
		"malformed json": func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("<html>not json</html>"))
		},
		"empty tag": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"tag_name": ""})
		},
	}
	for name, h := range cases {
		srv := httptest.NewServer(h)
		if n := check(t, cfg(t, "v0.1.0", srv.URL)); n != nil {
			t.Errorf("%s: unexpected notice %+v", name, n)
		}
		srv.Close()
	}
}

func TestUnreachableServerIsSilent(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close() // nothing is listening now
	c := cfg(t, "v0.1.0", base)
	c.Timeout = 200 * time.Millisecond
	if n := check(t, c); n != nil {
		t.Errorf("unexpected notice %+v", n)
	}
}

// Nothing on a subcommand's path may touch the network: Start resolves from the
// cache alone, so a server that never answers costs nothing and is never even
// contacted.
func TestStartNeverTouchesTheNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Start made an HTTP request")
		time.Sleep(10 * time.Second)
	}))
	defer srv.Close()

	start := time.Now()
	chk := update.Start(cfg(t, "v0.1.0", srv.URL))
	if n := chk.Result(); n != nil {
		t.Errorf("Result returned %+v with a cold cache", n)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Errorf("Start+Result took %s, want ~instant", elapsed)
	}
}

// Even with a Refresh in flight, Result must not wait for it.
func TestResultNeverWaitsForRefresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second)
	}))
	defer srv.Close()
	chk := update.Start(cfg(t, "v0.1.0", srv.URL))
	chk.Refresh()
	start := time.Now()
	if n := chk.Result(); n != nil {
		t.Errorf("Result returned %+v before the lookup could finish", n)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Errorf("Result took %s, want ~instant", elapsed)
	}
}

// The point of the cache: a run that does no lookup still shows the answer a
// previous run left behind, instantly.
func TestFreshCacheAnswersImmediately(t *testing.T) {
	base, hits := stubGitHub(t, release("v0.2.0"))
	c := cfg(t, "v0.1.0", base)
	if check(t, c) == nil {
		t.Fatal("first run: expected a notice")
	}

	// Point at a dead server: only the cache can answer now.
	c.APIBase = "http://127.0.0.1:1"
	start := time.Now()
	n := update.Start(c).Result() // no Refresh: exactly what a subcommand does
	if n == nil {
		t.Fatal("second run: Result did not see the cached notice")
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Errorf("cached answer took %s, want ~instant", elapsed)
	}
	if *hits != 1 {
		t.Errorf("made %d API calls, want 1", *hits)
	}
}

// The only thing a subcommand pays for is Start's small file read. Keep it
// negligible — this is the guard against anyone reintroducing work there.
func TestStartCostIsNegligible(t *testing.T) {
	base, _ := stubGitHub(t, release("v0.2.0"))
	c := cfg(t, "v0.1.0", base)
	if check(t, c) == nil { // warm the cache: the common case
		t.Fatal("expected a notice")
	}
	const runs = 200
	start := time.Now()
	for i := 0; i < runs; i++ {
		update.Start(c).Result()
	}
	per := time.Since(start) / runs
	t.Logf("Start+Result: %s per call (warm cache)", per)
	if per > 500*time.Microsecond {
		t.Errorf("Start+Result costs %s per call, want well under a millisecond", per)
	}
}

// Await must not hang when nothing is coming — no Refresh, no answer, no wait.
func TestAwaitWithoutRefreshReturnsImmediately(t *testing.T) {
	base, _ := stubGitHub(t, release("v0.2.0"))
	start := time.Now()
	if n := update.Start(cfg(t, "v0.1.0", base)).Await(); n != nil {
		t.Errorf("Await returned %+v without a Refresh", n)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Errorf("Await took %s, want ~instant", elapsed)
	}
}

// Reopening the browser after an agent session must not re-fetch.
func TestRefreshIsIdempotent(t *testing.T) {
	base, hits := stubGitHub(t, release("v0.2.0"))
	chk := update.Start(cfg(t, "v0.1.0", base))
	chk.Refresh()
	chk.Refresh()
	chk.Refresh()
	if chk.Await() == nil {
		t.Fatal("expected a notice")
	}
	if *hits != 1 {
		t.Errorf("made %d API calls, want 1", *hits)
	}
}

// An unwritable cache dir must not stop the notice.
func TestUnwritableCacheStillNotifies(t *testing.T) {
	base, _ := stubGitHub(t, release("v0.2.0"))
	c := cfg(t, "v0.1.0", base)
	c.CacheDir = filepath.Join(c.CacheDir, "denied")
	if err := os.Mkdir(c.CacheDir, 0o500); err != nil {
		t.Fatal(err)
	}
	if check(t, c) == nil {
		t.Error("expected a notice despite an unwritable cache dir")
	}
}

// A corrupt cache file is treated as a miss, not a crash.
func TestCorruptCacheIsIgnored(t *testing.T) {
	base, _ := stubGitHub(t, release("v0.2.0"))
	c := cfg(t, "v0.1.0", base)
	if err := os.WriteFile(filepath.Join(c.CacheDir, "update-check.json"),
		[]byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if check(t, c) == nil {
		t.Error("expected a notice with a corrupt cache present")
	}
}

// The notice must be consumable exactly once: the browser takes it for its
// footer, and that is precisely what keeps Execute from also printing it to
// stderr.
func TestResultYieldsOnlyOnce(t *testing.T) {
	base, _ := stubGitHub(t, release("v0.2.0"))
	chk := update.Start(cfg(t, "v0.1.0", base))
	chk.Refresh()
	if chk.Await() == nil {
		t.Fatal("first collection: expected a notice")
	}
	if n := chk.Await(); n != nil {
		t.Errorf("second collection returned %+v, want nil", n)
	}
	if n := chk.Result(); n != nil {
		t.Errorf("third collection returned %+v, want nil", n)
	}
}

// browseLoop chains these calls, so the nil path has to survive it.
func TestLineOnEmptyResultIsEmpty(t *testing.T) {
	base, _ := stubGitHub(t, release("v0.1.0")) // same as current: no notice
	if line := check(t, cfg(t, "v0.1.0", base)).Line(); line != "" {
		t.Errorf("Line() = %q, want empty", line)
	}
}

func TestNilCheckAndNilNoticeAreSafe(t *testing.T) {
	var c *update.Check
	if n := c.Result(); n != nil {
		t.Errorf("nil Check.Result returned %+v", n)
	}
	if n := c.Await(); n != nil {
		t.Errorf("nil Check.Await returned %+v", n)
	}
	var n *update.Notice
	if n.Line() != "" || n.Block() != "" {
		t.Error("nil Notice should render empty strings")
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"1.0.0", "v1.0.0", 0},
		{"v1.0.1", "v1.0.0", 1},
		{"v1.1.0", "v1.0.9", 1},
		{"v2.0.0", "v1.99.99", 1},
		{"v0.9.0", "v0.10.0", -1}, // numeric, not lexical
		{"v1.0.0", "v1.0.0-rc.1", 1},
		{"v1.0.0-rc.1", "v1.0.0", -1},
		{"v1.0.0-rc.2", "v1.0.0-rc.1", 1},
		{"v1.0.0-rc.10", "v1.0.0-rc.9", 1},
		{"v1.0.0-beta", "v1.0.0-alpha", 1},
		{"v1.0.0-alpha", "v1.0.0-alpha.1", -1},
		{"v1.0.0+build9", "v1.0.0+build1", 0}, // build metadata ignored
		{"v1.2", "v1.2.0", 0},
		{"v1.0.0", "dev", 1},
		{"dev", "v1.0.0", -1},
		{"dev", "garbage", 0},
	}
	for _, c := range cases {
		if got := update.Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
