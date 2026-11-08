package daemon

import (
	"bufio"
	"context"
	"net"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
)

func TestWakeRetriesHeldSpawns(t *testing.T) {
	profile := testProfile(t)
	profile.Windows["arc"] = config.WindowSpec{App: "Zen", AppRE: regexp.MustCompile("Zen"), Spawn: []string{"open", "Zen"}}
	driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: &wm.Frame{W: 5120, H: 2160}}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}}, SpawnNoWindow: true})
	driver.Uptime = 30*time.Second - 500*time.Millisecond
	env := daemonEnv(t)
	env.Nudge = func(context.Context, string) error { return nil }
	sock := filepath.Join(shortDir(t), "retry.sock")
	listener, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	wake := WakeRoutine(profile, driver, env, sock)
	start := time.Now()
	for range 3 {
		if err := wake(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
		t.Fatalf("held wakes blocked for %s", elapsed)
	}
	if len(driver.SpawnCalls) != 0 {
		t.Fatal("spawned before yabai was stable")
	}
	unix := listener.(*net.UnixListener)
	if err := unix.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	conn.Close()
	if err != nil || line != "wake\n" {
		t.Fatalf("retry = %q, %v", line, err)
	}
	if err := unix.SetDeadline(time.Now().Add(700 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	conn, err = listener.Accept()
	if err == nil {
		conn.Close()
		t.Fatal("repeated held wakes scheduled multiple retries")
	}
	if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatal(err)
	}
}
