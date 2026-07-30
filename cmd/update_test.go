package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pradipta/wallfacer/internal/update"
)

var testNotice = &update.Notice{
	Current: "v0.1.0",
	Latest:  "v0.2.0",
	URL:     "https://github.com/pradipta/wallfacer/releases/tag/v0.2.0",
}

func TestPrintUpdateNoticeOnTTY(t *testing.T) {
	var buf bytes.Buffer
	printUpdateNotice(&buf, true, testNotice)
	out := buf.String()
	for _, want := range []string{
		"v0.2.0", "v0.1.0",
		"go install github.com/pradipta/wallfacer@latest",
		testNotice.URL,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("notice missing %q:\n%s", want, out)
		}
	}
}

// Scripts and pipes must see nothing, so `wallfacer list --json 2>&1 | jq` and
// friends stay parseable.
func TestPrintUpdateNoticeSilentWhenNotTTY(t *testing.T) {
	var buf bytes.Buffer
	printUpdateNotice(&buf, false, testNotice)
	if buf.Len() != 0 {
		t.Errorf("wrote %q to a non-terminal", buf.String())
	}
}

func TestPrintUpdateNoticeSilentWithoutNotice(t *testing.T) {
	var buf bytes.Buffer
	printUpdateNotice(&buf, true, nil)
	if buf.Len() != 0 {
		t.Errorf("wrote %q with no notice", buf.String())
	}
}

// resolveVersion feeds the check; a dev build must not produce a notice at all.
func TestDevVersionProducesNoCheck(t *testing.T) {
	if n := update.Start(update.Config{Current: "dev", CacheDir: t.TempDir()}).Result(); n != nil {
		t.Errorf("dev build got a notice: %+v", n)
	}
}
