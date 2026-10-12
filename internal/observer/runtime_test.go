package observer

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// This opt-in contract test runs the full official distribution, with no Zwift
// credentials. It verifies bundled world assets and the real headless entry.
func TestOfficialRuntimeHelp(t *testing.T) {
	executable, entry := os.Getenv("ZWOICE_TEST_SAUCE_EXECUTABLE"), os.Getenv("ZWOICE_TEST_SAUCE_ENTRYPOINT")
	if executable == "" || entry == "" {
		t.Skip("official Sauce runtime not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, entry, "--headless", "--help")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "ELECTRON_RUN_AS_NODE=1"}
	cmd.Dir = filepath.Dir(executable)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("official headless runtime failed: %v\n%s", err, out)
	}
	if !bytes.Contains(out, []byte("--athlete-id")) || bytes.Contains(out, []byte("World data load error")) {
		t.Fatalf("incomplete headless runtime: %s", out)
	}
}
