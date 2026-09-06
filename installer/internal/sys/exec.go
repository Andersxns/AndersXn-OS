// Package sys wraps the host inspection and command execution the installer
// needs. Everything that shells out lives here so the rest of the installer can
// be exercised against a fake Runner in tests.
package sys

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Runner executes external commands. Install steps take a Runner rather than
// calling os/exec directly, which is what makes a dry-run install possible.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) error
	Output(ctx context.Context, name string, args ...string) (string, error)
	RunInput(ctx context.Context, stdin, name string, args ...string) error
}

// LogFunc receives every command and every line of its output.
type LogFunc func(line string)

// ExecRunner is the real Runner. All output is streamed to Log as it arrives so
// the installer's log pane stays live during a long apt or mkfs run rather than
// blocking until the command exits.
type ExecRunner struct {
	Log    LogFunc
	DryRun bool

	// Env is applied on top of the current environment for every command.
	Env []string

	mu sync.Mutex
}

// NewExecRunner returns a Runner that streams output to log.
func NewExecRunner(log LogFunc) *ExecRunner {
	if log == nil {
		log = func(string) {}
	}
	return &ExecRunner{
		Log: log,
		Env: []string{
			"DEBIAN_FRONTEND=noninteractive",
			"DEBCONF_NONINTERACTIVE_SEEN=true",
			"LC_ALL=C.UTF-8",
		},
	}
}

func (r *ExecRunner) logf(format string, a ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Log(fmt.Sprintf(format, a...))
}

// Run executes a command, streaming stdout and stderr into the log.
func (r *ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	return r.RunInput(ctx, "", name, args...)
}

// RunInput is Run with data written to the command's stdin.
func (r *ExecRunner) RunInput(ctx context.Context, stdin, name string, args ...string) error {
	r.logf("$ %s %s", name, strings.Join(args, " "))
	if r.DryRun {
		r.logf("  (dry run - not executed)")
		return nil
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), r.Env...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	scanner := bufio.NewScanner(pipe)
	// Package managers and mkfs happily emit lines longer than the 64KiB
	// default, and a truncation panic mid-install would be unrecoverable.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	scanner.Split(scanLinesOrCR)
	for scanner.Scan() {
		r.logf("  %s", scanner.Text())
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// Output runs a command and returns its stdout, without logging the body. Used
// for the small informational queries (lsblk, blkid) where the output is data
// rather than progress.
func (r *ExecRunner) Output(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), r.Env...)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, okExit := err.(*exec.ExitError); okExit {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		if stderr != "" {
			return "", fmt.Errorf("%s: %w: %s", name, err, stderr)
		}
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return string(out), nil
}

// Chroot runs a command inside the target root.
func Chroot(ctx context.Context, r Runner, root string, args ...string) error {
	return r.Run(ctx, "chroot", append([]string{root}, args...)...)
}

// ChrootScript pipes a shell script into the target root's bash.
func ChrootScript(ctx context.Context, r Runner, root, script string) error {
	return r.RunInput(ctx, script, "chroot", root, "/bin/bash", "-euo", "pipefail")
}

// scanLinesOrCR splits on a newline OR a carriage return.
//
// Long-running tools redraw a progress bar in place with '\r' and never emit a
// newline until they finish. bufio's default ScanLines therefore yields nothing
// at all for the whole run, which makes the longest step of an install -
// unsquashfs writing ~4GB across 114,000 files - look completely frozen.
// Splitting on '\r' too turns that silence into live progress.
func scanLinesOrCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		adv := i + 1
		// Treat a CRLF pair as a single terminator rather than emitting a
		// spurious empty token between the two bytes.
		if data[i] == '\r' && len(data) > i+1 && data[i+1] == '\n' {
			adv++
		}
		return adv, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}
