package main

import (
	"fmt"
	"io"
	"sync"
	"time"
)

var progressFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type progressDisplay struct {
	writer      io.Writer
	enabled     bool
	interactive bool
	color       bool

	mu        sync.Mutex
	message   string
	startedAt time.Time
	stop      chan struct{}
	done      chan struct{}
	rendered  bool
	stopped   bool
}

func newProgressDisplay(mode, colorMode string, writer io.Writer) *progressDisplay {
	interactive := writerIsTerminal(writer) && terminalSupportsInteraction()
	enabled := mode == "always" || mode == "auto" && interactive
	return &progressDisplay{
		writer:      writer,
		enabled:     enabled,
		interactive: interactive,
		color:       interactive && progressColorEnabled(colorMode, writer),
	}
}

func (display *progressDisplay) Start(message string) {
	if display == nil || !display.enabled {
		return
	}
	display.mu.Lock()
	if !display.startedAt.IsZero() || display.stopped {
		display.mu.Unlock()
		return
	}
	display.message = message
	display.startedAt = time.Now()
	if display.interactive {
		display.stop = make(chan struct{})
		display.done = make(chan struct{})
	}
	display.mu.Unlock()

	if !display.interactive {
		fmt.Fprintf(display.writer, "  … %s\n", message)
		return
	}
	go display.animate()
}

func (display *progressDisplay) Update(message string) {
	if display == nil || !display.enabled {
		return
	}
	display.mu.Lock()
	if display.startedAt.IsZero() || display.stopped || message == display.message {
		display.mu.Unlock()
		return
	}
	display.message = message
	display.mu.Unlock()
	if !display.interactive {
		fmt.Fprintf(display.writer, "  … %s\n", message)
	}
}

func (display *progressDisplay) Stop(success bool, message string) {
	if display == nil || !display.enabled {
		return
	}
	display.mu.Lock()
	if display.stopped || display.startedAt.IsZero() {
		display.mu.Unlock()
		return
	}
	display.stopped = true
	startedAt := display.startedAt
	stop := display.stop
	done := display.done
	interactive := display.interactive
	display.mu.Unlock()

	if interactive {
		close(stop)
		<-done
		display.mu.Lock()
		rendered := display.rendered
		display.mu.Unlock()
		if !rendered {
			return
		}
		fmt.Fprint(display.writer, "\r\x1b[2K")
	}

	symbol := "✓"
	colorCode := "\x1b[32m"
	if !success {
		symbol = "✗"
		colorCode = "\x1b[31m"
	}
	if display.color {
		symbol = colorCode + symbol + "\x1b[0m"
	}
	fmt.Fprintf(display.writer, "  %s %s · %s\n", symbol, message, formatProgressElapsed(time.Since(startedAt)))
}

func (display *progressDisplay) animate() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	defer close(display.done)
	frame := 0
	for {
		select {
		case <-ticker.C:
			display.mu.Lock()
			message := display.message
			startedAt := display.startedAt
			display.rendered = true
			display.mu.Unlock()
			symbol := progressFrames[frame%len(progressFrames)]
			if display.color {
				symbol = "\x1b[36m" + symbol + "\x1b[0m"
			}
			fmt.Fprintf(display.writer, "\r\x1b[2K  %s %s · %s", symbol, message, formatProgressElapsed(time.Since(startedAt)))
			frame++
		case <-display.stop:
			return
		}
	}
}

func formatProgressElapsed(elapsed time.Duration) string {
	elapsed = elapsed.Round(100 * time.Millisecond)
	if elapsed < time.Second {
		return fmt.Sprintf("%dms", elapsed.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", elapsed.Seconds())
}
