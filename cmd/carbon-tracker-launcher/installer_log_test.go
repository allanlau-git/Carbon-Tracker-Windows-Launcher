//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallerCommandChild(t *testing.T) {
	switch os.Getenv("CARBON_INSTALLER_TEST_CHILD") {
	case "success":
		fmt.Fprint(os.Stdout, "installer standard output")
		fmt.Fprint(os.Stderr, "installer standard error")
		os.Exit(0)
	case "failure":
		fmt.Fprint(os.Stderr, "simulated installer failure")
		os.Exit(23)
	case "timeout":
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
}

func TestInstallerDiagnostics(t *testing.T) {
	for _, mode := range []string{"success", "failure", "timeout", "missing"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("LOCALAPPDATA", t.TempDir())
			log, err := openInstallerLog()
			if err != nil {
				t.Fatal(err)
			}
			exe, _ := os.Executable()
			timeout := 5 * time.Second
			if mode == "timeout" {
				timeout = 100 * time.Millisecond
			}
			if mode == "missing" {
				exe = filepath.Join(t.TempDir(), "absent.exe")
			}
			env := append(os.Environ(), "CARBON_INSTALLER_TEST_CHILD="+mode)
			err = log.command(timeout, env, exe, "-test.run=^TestInstallerCommandChild$")
			if (err == nil) != (mode == "success") {
				t.Fatalf("unexpected result: %v", err)
			}
			if log.err != nil {
				t.Fatal(log.err)
			}
			if err := log.f.Close(); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(dockerInstallerLogPath())
			if err != nil {
				t.Fatal(err)
			}
			s := string(b)
			for _, want := range []string{"Command", "Exit code", time.Now().Format("2006-01-02")} {
				if !strings.Contains(s, want) {
					t.Errorf("missing %q: %s", want, s)
				}
			}
			switch mode {
			case "success":
				for _, want := range []string{"stdout", "stderr", "installer standard output", "installer standard error", "\r\n0\r\n"} {
					if !strings.Contains(s, want) {
						t.Errorf("missing %q: %s", want, s)
					}
				}
			case "failure":
				if !strings.Contains(s, "\r\n23\r\n") || !strings.Contains(s, "simulated installer failure") {
					t.Fatal(s)
				}
			case "timeout":
				if !strings.Contains(s, "timed out") {
					t.Fatal(s)
				}
			case "missing":
				if !strings.Contains(s, "not started") {
					t.Fatal(s)
				}
			}
			// A subsequent attempt must preserve the previous session.
			log, err = openInstallerLog()
			if err != nil {
				t.Fatal(err)
			}
			log.record("Retry", "second session")
			log.f.Close()
			b, _ = os.ReadFile(dockerInstallerLogPath())
			if !strings.HasPrefix(string(b), s) || !strings.Contains(string(b), "second session") {
				t.Fatal("retry erased earlier diagnostics")
			}
		})
	}
}

func TestComposeDiscoveryWithoutDocker(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	if _, err := findCompose(); err == nil {
		t.Fatal("unexpected Compose file on fresh machine")
	}
	if err := os.MkdirAll(appRoot(), 0755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(appRoot(), "docker-compose.yml")
	if err := os.WriteFile(p, []byte("services:\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got, err := findCompose(); err != nil || got != p {
		t.Fatalf("existing app logs would not be selected: %s, %v", got, err)
	}
}
