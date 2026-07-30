// Package update checks whether a newer wallfacer release exists on GitHub and
// renders a one-off notice for the CLI and the TUI.
//
// Design constraints, in priority order:
//
//   - It must never break a command. Every failure path (no network, GitHub
//     down, rate limited, malformed JSON, unwritable cache) yields "no notice"
//     rather than an error.
//   - It must never make wallfacer feel slow. The HTTP call happens in a
//     goroutine started before the command's real work, and the result is
//     collected afterwards with a bounded grace period.
//   - It must not hammer the API. The answer is cached in the data dir and
//     re-fetched at most once per Interval (default 24h).
package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultRepo is the GitHub "owner/name" wallfacer releases live under.
	DefaultRepo = "pradipta/wallfacer"
	// DefaultAPIBase is GitHub's API root. Overridable for tests and for
	// manual end-to-end checks against a local stub server.
	DefaultAPIBase = "https://api.github.com"
	// DefaultInterval is how long a cached answer is considered fresh.
	DefaultInterval = 24 * time.Hour
	// DefaultTimeout caps the HTTP request itself.
	DefaultTimeout = 3 * time.Second
	// DefaultGrace is how long Result waits for an in-flight check. Commands
	// do disk work (sync) between Start and Result, so a cached-or-fast check
	// is usually done by then and this is rarely reached.
	DefaultGrace = 1500 * time.Millisecond

	// EnvDisable, when set to a non-empty value other than "0"/"false",
	// disables the check entirely. Packagers and CI should set it.
	EnvDisable = "WALLFACER_NO_UPDATE_CHECK"
	// EnvAPIBase overrides DefaultAPIBase (local testing).
	EnvAPIBase = "WALLFACER_UPDATE_API"
	// EnvInterval overrides DefaultInterval, as a Go duration ("0" forces a
	// fetch on every run).
	EnvInterval = "WALLFACER_UPDATE_INTERVAL"

	cacheFile = "update-check.json"
)

// Notice describes an available upgrade. A nil *Notice means "up to date, or
// we could not tell" — callers should treat both the same way.
type Notice struct {
	Current string // the running version, e.g. "v0.1.0"
	Latest  string // the newest published release, e.g. "v0.2.0"
	URL     string // release page to send the user to
}

// Line is the single-line form, for the TUI's footer.
func (n *Notice) Line() string {
	if n == nil {
		return ""
	}
	return fmt.Sprintf("update available: %s → %s", n.Current, n.Latest)
}

// Block is the multi-line form, for stderr after a CLI command. It names both
// upgrade paths because wallfacer cannot tell how it was installed.
func (n *Notice) Block() string {
	if n == nil {
		return ""
	}
	return strings.Join([]string{
		fmt.Sprintf("wallfacer %s is available (you have %s)", n.Latest, n.Current),
		"  go install " + ModulePath() + "@latest",
		"  or download from " + n.URL,
	}, "\n")
}

// ModulePath is the go-installable path for DefaultRepo.
func ModulePath() string { return "github.com/" + DefaultRepo }

// Config controls a check. The zero value is usable: everything falls back to
// the Default* constants and the relevant environment overrides.
type Config struct {
	// Current is the running version. "dev", "" or anything unparsable
	// disables the check — dev builds should not nag.
	Current string
	// CacheDir is where the freshness cache is kept (wallfacer's data dir).
	// Empty means no caching: every call fetches.
	CacheDir string
	Repo     string
	APIBase  string
	Interval time.Duration
	Timeout  time.Duration
	Grace    time.Duration
	// Client overrides the HTTP client (tests).
	Client *http.Client
	// Now overrides the clock (tests).
	Now func() time.Time
}

func (c Config) withDefaults() Config {
	if c.Repo == "" {
		c.Repo = DefaultRepo
	}
	if c.APIBase == "" {
		c.APIBase = firstNonEmpty(os.Getenv(EnvAPIBase), DefaultAPIBase)
	}
	if c.Interval == 0 {
		c.Interval = DefaultInterval
		if d, err := time.ParseDuration(os.Getenv(EnvInterval)); err == nil {
			c.Interval = d
		}
	}
	if c.Timeout == 0 {
		c.Timeout = DefaultTimeout
	}
	if c.Grace == 0 {
		c.Grace = DefaultGrace
	}
	if c.Client == nil {
		c.Client = &http.Client{Timeout: c.Timeout}
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

// Check is an in-flight (or already finished) update check.
type Check struct {
	res chan *Notice
	// grace bounds how long Result blocks.
	grace time.Duration
}

// Start kicks off a check in the background and returns immediately. The
// returned *Check is always usable; a disabled or skipped check simply yields
// no notice.
func Start(cfg Config) *Check {
	cfg = cfg.withDefaults()
	c := &Check{res: make(chan *Notice, 1), grace: cfg.Grace}
	if Disabled() || !isReleaseVersion(cfg.Current) {
		close(c.res)
		return c
	}
	go func() {
		defer close(c.res)
		if n := lookup(cfg); n != nil {
			c.res <- n
		}
	}()
	return c
}

// Result returns the notice, waiting at most the configured grace period for
// an in-flight check. It returns nil when there is no upgrade to report, and
// is safe to call once; later calls return nil.
func (c *Check) Result() *Notice {
	if c == nil {
		return nil
	}
	select {
	case n := <-c.res:
		return n
	case <-time.After(c.grace):
		return nil
	}
}

// Disabled reports whether the user (or a packager) turned the check off.
func Disabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(EnvDisable)))
	return v != "" && v != "0" && v != "false"
}

// lookup resolves the latest release (from cache when fresh) and compares.
func lookup(cfg Config) *Notice {
	rel, ok := readCache(cfg)
	if !ok {
		var err error
		rel, err = fetchLatest(cfg)
		if err != nil {
			return nil
		}
		writeCache(cfg, rel)
	}
	if rel.Latest == "" || Compare(rel.Latest, cfg.Current) <= 0 {
		return nil
	}
	return &Notice{
		Current: normalize(cfg.Current),
		Latest:  normalize(rel.Latest),
		URL:     firstNonEmpty(rel.URL, "https://github.com/"+cfg.Repo+"/releases/latest"),
	}
}

// cached is the on-disk shape of the freshness cache.
type cached struct {
	CheckedAt int64  `json:"checked_at"`
	Latest    string `json:"latest"`
	URL       string `json:"url"`
}

func cachePath(cfg Config) string {
	if cfg.CacheDir == "" {
		return ""
	}
	return filepath.Join(cfg.CacheDir, cacheFile)
}

// readCache returns the cached release when it is still within Interval.
func readCache(cfg Config) (cached, bool) {
	p := cachePath(cfg)
	if p == "" || cfg.Interval <= 0 {
		return cached{}, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return cached{}, false
	}
	var c cached
	if err := json.Unmarshal(b, &c); err != nil {
		return cached{}, false
	}
	age := cfg.Now().Sub(time.Unix(c.CheckedAt, 0))
	// A clock that moved backwards (negative age) counts as stale, not fresh
	// forever.
	if age < 0 || age > cfg.Interval {
		return cached{}, false
	}
	return c, true
}

// writeCache records the answer. Failures are ignored: the cache is an
// optimisation, never a requirement.
func writeCache(cfg Config, rel cached) {
	p := cachePath(cfg)
	if p == "" {
		return
	}
	rel.CheckedAt = cfg.Now().Unix()
	b, err := json.Marshal(rel)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
	}
}

// fetchLatest asks GitHub for the repo's latest release. /releases/latest
// excludes drafts and pre-releases, which is exactly what the release workflow
// arranges with make_latest, so RC tags never trigger a notice.
func fetchLatest(cfg Config) (cached, error) {
	url := strings.TrimSuffix(cfg.APIBase, "/") + "/repos/" + cfg.Repo + "/releases/latest"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return cached{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "wallfacer/"+cfg.Current)
	resp, err := cfg.Client.Do(req)
	if err != nil {
		return cached{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return cached{}, fmt.Errorf("github: %s", resp.Status)
	}
	var body struct {
		TagName    string `json:"tag_name"`
		HTMLURL    string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	// Cap the read: a wrong APIBase should not stream forever.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return cached{}, err
	}
	if body.Draft || body.Prerelease {
		return cached{}, nil
	}
	return cached{Latest: body.TagName, URL: body.HTMLURL}, nil
}

// isReleaseVersion reports whether v looks like a published release we can
// compare against. Development builds are excluded so working copies never nag:
//
//   - "dev" and anything unparsable (a bare `go build`),
//   - Go pseudo-versions from untagged commits,
//   - `git describe` output ("v1.1.0-3-gabc1234", "v1.1.0-dirty"), which sorts
//     *below* v1.1.0 in semver but is actually ahead of it — exactly the case
//     that would otherwise tell a contributor to downgrade.
func isReleaseVersion(v string) bool {
	nums, pre, ok := parse(v)
	if !ok {
		return false
	}
	// A pseudo-version like v0.0.0-20240101120000-abcdef123456 means "built
	// from an untagged commit"; nagging about it is noise.
	if nums == [3]int{} && strings.Count(pre, "-") >= 1 {
		return false
	}
	return !gitDescribeSuffix.MatchString(pre)
}

// gitDescribeSuffix matches the pre-release part `git describe --tags --dirty`
// appends: a commit count plus abbreviated sha, and/or a -dirty marker. A dirty
// tree always means a working build, even on top of a clean rc tag
// ("v1.2.0-rc.1-dirty").
var gitDescribeSuffix = regexp.MustCompile(
	`(^|[.-])dirty$|(^|\.)[0-9]+-g[0-9a-f]{4,}$`)

// Compare orders two semver strings: -1 if a < b, 0 if equal, +1 if a > b.
// Unparsable versions sort below parsable ones, so a "dev" current version can
// never look newer than a real release.
func Compare(a, b string) int {
	na, prea, oka := parse(a)
	nb, preb, okb := parse(b)
	switch {
	case !oka && !okb:
		return 0
	case !oka:
		return -1
	case !okb:
		return 1
	}
	for i := range na {
		if na[i] != nb[i] {
			return sign(na[i] - nb[i])
		}
	}
	return comparePre(prea, preb)
}

// comparePre implements semver §11 precedence for pre-release parts: a version
// with a pre-release ranks below the same version without one, and identifiers
// are compared left to right (numeric numerically, and below alphanumerics).
func comparePre(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		an, aNum := toInt(as[i])
		bn, bNum := toInt(bs[i])
		switch {
		case aNum && bNum:
			return sign(an - bn)
		case aNum:
			return -1 // numeric identifiers rank below alphanumeric ones
		case bNum:
			return 1
		default:
			return strings.Compare(as[i], bs[i])
		}
	}
	return sign(len(as) - len(bs))
}

// parse splits "v1.2.3-rc.1+build" into its numeric triple and pre-release
// part. Missing minor/patch default to 0, so "v1" and "v1.2" parse.
func parse(v string) (nums [3]int, pre string, ok bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	if v == "" {
		return nums, "", false
	}
	if i := strings.IndexByte(v, '+'); i >= 0 { // build metadata is ignored
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		pre, v = v[i+1:], v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) > 3 {
		return nums, "", false
	}
	for i, p := range parts {
		n, isNum := toInt(p)
		if !isNum {
			return [3]int{}, "", false
		}
		nums[i] = n
	}
	return nums, pre, true
}

// normalize renders a version the way we want to display it: always v-prefixed.
func normalize(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

func toInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}
