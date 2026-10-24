//go:build unix

package deepsearch

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"swar/internal/catalog"
)

// A fake yt-dlp (a shell script whose path has a space and non-ASCII
// characters) starts a helper process and hangs. Stopping the run must
// kill both, and arguments must arrive unchanged.
func TestRealProcessGroupIsKilledAndArgsArePassedAsIs(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	dir := filepath.Join(t.TempDir(), "yt dlp ñ")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(dir, "helper.pid")
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > '" + argsFile + "'\n" +
		"sleep 30 &\n" +
		"echo $! > '" + pidFile + "'\n" +
		"wait\n"
	bin := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Binary: bin, Timeout: time.Minute})
	errc := make(chan error, 1)
	go func() {
		_, err := s.Search(context.Background(), `it's "quoted"; $(echo no)`, 2)
		errc <- err
	}()
	var helper int
	deadline := time.Now().Add(5 * time.Second)
	for helper == 0 && time.Now().Before(deadline) {
		if b, err := os.ReadFile(pidFile); err == nil {
			helper, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if helper == 0 {
		t.Fatal("fake yt-dlp did not start")
	}
	s.Close(3 * time.Second)
	if err := <-errc; catalog.KindOf(err) != catalog.KindUnavailable {
		t.Fatalf("want shutdown error, got %v", err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for syscall.Kill(helper, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if syscall.Kill(helper, 0) == nil {
		syscall.Kill(helper, syscall.SIGKILL)
		t.Fatal("helper process outlived the cancelled run")
	}
	args, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(args), "ytsearch2:it's \"quoted\"; $(echo no)\n") {
		t.Fatalf("query should reach yt-dlp as one untouched argument:\n%s", args)
	}
}
