//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package semantic

import (
	"io"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestProcessGroupHelper(t *testing.T) {
	mode := os.Getenv("SEMANTIC_PROCESS_HELPER")
	if mode == "" {
		return
	}
	inherited := os.NewFile(3, "descendant-liveness")
	if mode == "child" {
		_, _ = inherited.Write([]byte("x"))
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestProcessGroupHelper$")
	child.Env = append(os.Environ(), "SEMANTIC_PROCESS_HELPER=child")
	child.ExtraFiles = []*os.File{inherited}
	if child.Start() != nil {
		os.Exit(2)
	}
	if mode == "exited-root" {
		os.Exit(0)
	}
	time.Sleep(time.Hour)
	os.Exit(0)
}

func TestStartOwnProcessGroup(t *testing.T) {
	c, _ := startServer(t, "normal")
	group, err := syscall.Getpgid(c.cmd.Process.Pid)
	if err != nil || group != c.cmd.Process.Pid {
		t.Fatalf("server group %d, pid %d, error %v", group, c.cmd.Process.Pid, err)
	}
}

func TestKillProcessGroupDescendants(t *testing.T) {
	for _, mode := range []string{"live-root", "exited-root"} {
		t.Run(mode, func(t *testing.T) {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			cmd := exec.Command(os.Args[0], "-test.run=^TestProcessGroupHelper$")
			cmd.Env = append(os.Environ(), "SEMANTIC_PROCESS_HELPER="+mode)
			cmd.ExtraFiles = []*os.File{writer}
			setupProcessGroup(cmd)
			if err := cmd.Start(); err != nil {
				writer.Close()
				t.Fatal(err)
			}
			defer func() { killProcessGroup(cmd); _ = cmd.Wait() }()
			writer.Close()
			if err := reader.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			var ready [1]byte
			if _, err := io.ReadFull(reader, ready[:]); err != nil {
				t.Fatalf("descendant startup: %v", err)
			}
			if mode == "exited-root" {
				if err := cmd.Wait(); err != nil {
					t.Fatal(err)
				}
			}
			killProcessGroup(cmd)
			// EOF proves the descendant's inherited descriptor was closed, including
			// systems whose init process does not promptly reap orphaned zombies.
			if _, err := reader.Read(ready[:]); err != io.EOF {
				t.Fatalf("descendant survived group kill: %v", err)
			}
		})
	}
}
