//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

func dockerInstallerLogPath() string {
	return filepath.Join(appRoot(), "logs", "docker-desktop-install.log")
}

// Append sessions so a retry does not erase evidence of an earlier failure.
type installerLog struct {
	mu  sync.Mutex
	f   *os.File
	err error
}

func openInstallerLog() (*installerLog, error) {
	p := dockerInstallerLogPath()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	return &installerLog{f: f}, nil
}

func (l *installerLog) record(label, message string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := fmt.Fprintf(l.f, "\r\n[%s] %s\r\n%s\r\n", time.Now().Format(time.RFC3339), label, message); err != nil && l.err == nil {
		l.err = err
	}
	if err := l.f.Sync(); err != nil && l.err == nil {
		l.err = err
	}
}

type installerOutput struct {
	log   *installerLog
	label string
}

func (w installerOutput) Write(p []byte) (int, error) {
	// Keep draining both pipes even if the disk fills; report logging errors later.
	w.log.record(w.label, string(p))
	return len(p), nil
}

func (l *installerLog) command(timeout time.Duration, env []string, exe string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = env
	cmd.WaitDelay = 2 * time.Second
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	l.record("Command", cmd.String())
	l.record("stdout/stderr", "Output is streamed below with separate stdout and stderr labels; empty streams produce no chunks.")
	cmd.Stdout = installerOutput{l, "stdout"}
	cmd.Stderr = installerOutput{l, "stderr"}
	err := cmd.Run()
	code := "not started"
	if cmd.ProcessState != nil {
		code = fmt.Sprint(cmd.ProcessState.ExitCode())
	}
	l.record("Exit code", code)
	if ctx.Err() != nil {
		err = fmt.Errorf("command timed out after %s: %w", timeout, ctx.Err())
	}
	if err != nil {
		l.record("Command error", err.Error())
	}
	return err
}

func (l *installerLog) detection(stage string) {
	d, err := dockerExe()
	l.record("Docker detection ("+stage+")", fmt.Sprintf("Desktop installed: %t\r\nCLI: %s\r\nCLI detection error: %v\r\nResource directories: %s\r\nCredential helper: %s", dockerDesktopInstalled(), d, err, strings.Join(dockerBinDirs(), "; "), credentialHelperPath()))
}

func (l *installerLog) diagnostics() {
	l.record("Session started", "Docker Desktop installation/update; architecture: "+runtime.GOARCH)
	l.detection("before")
	// Read-only, bounded checks. Missing tools, permissions and unsupported WSL
	// options are recorded without blocking the existing installation behavior.
	_ = l.command(5*time.Second, nil, "cmd.exe", "/d", "/c", "ver")
	_ = l.command(5*time.Second, nil, "wsl.exe", "--status")
	_ = l.command(5*time.Second, nil, "wsl.exe", "--version")
	_ = l.command(20*time.Second, nil, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `$ErrorActionPreference='Continue'; Get-CimInstance Win32_OperatingSystem | Select-Object Caption,Version,BuildNumber,OSArchitecture | Format-List; Get-CimInstance Win32_ComputerSystem | Select-Object HypervisorPresent | Format-List; Get-CimInstance Win32_Processor | Select-Object Name,VirtualizationFirmwareEnabled,SecondLevelAddressTranslationExtensions,VMMonitorModeExtensions | Format-List; 'Microsoft-Windows-Subsystem-Linux','VirtualMachinePlatform' | ForEach-Object { Get-WindowsOptionalFeature -Online -FeatureName $_ } | Select-Object FeatureName,State | Format-List`)
}

func viewInstallerLog() error {
	p := dockerInstallerLogPath()
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return fmt.Errorf("No Docker Desktop installer log exists yet. Click Docker Desktop to run installation diagnostics.\r\n\r\nLog location: %s", p)
	} else if err != nil {
		return err
	}
	if err := shellOpen(p); err != nil {
		return err
	}
	setStatus("Docker Desktop installer log opened.")
	setOutput("Log file:\r\n" + p)
	return nil
}
