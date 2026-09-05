package dockerinventory

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

var errOutputLimit = errors.New("command output limit reached")

type CommandResult struct {
	Stdout        []byte
	Stderr        []byte
	Err           error
	OutputLimited bool
	TimedOut      bool
}

type Runner interface {
	LookPath(file string) (string, error)
	Run(ctx context.Context, name string, arguments []string, maxOutputBytes int64) CommandResult
}

type OSRunner struct{}

func (OSRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (OSRunner) Run(ctx context.Context, name string, arguments []string, maxOutputBytes int64) CommandResult {
	if maxOutputBytes <= 0 {
		maxOutputBytes = DefaultMaxOutputBytes
	}
	stdout := newBoundedBuffer(maxOutputBytes)
	stderr := newBoundedBuffer(minInt64(maxOutputBytes/8, 256<<10))
	command := exec.CommandContext(ctx, name, arguments...)
	command.Stdin = nil
	command.Stdout = stdout
	command.Stderr = stderr
	command.WaitDelay = 250 * time.Millisecond
	err := command.Run()
	return CommandResult{
		Stdout:        stdout.Bytes(),
		Stderr:        stderr.Bytes(),
		Err:           err,
		OutputLimited: stdout.Limited() || stderr.Limited() || errors.Is(err, errOutputLimit),
		TimedOut:      errors.Is(ctx.Err(), context.DeadlineExceeded),
	}
}

type boundedBuffer struct {
	buffer  bytes.Buffer
	limit   int64
	limited bool
}

func newBoundedBuffer(limit int64) *boundedBuffer {
	if limit <= 0 {
		limit = 1
	}
	return &boundedBuffer{limit: limit}
}

func (buffer *boundedBuffer) Write(content []byte) (int, error) {
	remaining := buffer.limit - int64(buffer.buffer.Len())
	if remaining <= 0 {
		buffer.limited = true
		return 0, errOutputLimit
	}
	if int64(len(content)) > remaining {
		written, _ := buffer.buffer.Write(content[:remaining])
		buffer.limited = true
		return written, errOutputLimit
	}
	return buffer.buffer.Write(content)
}

func (buffer *boundedBuffer) Bytes() []byte {
	return append([]byte(nil), buffer.buffer.Bytes()...)
}

func (buffer *boundedBuffer) Limited() bool {
	return buffer.limited
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
