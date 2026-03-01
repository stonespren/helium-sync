package sync

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	barWidth = 30
	barFull  = "█"
	barEmpty = "░"
)

// ProgressBar displays a terminal progress bar on stderr.
type ProgressBar struct {
	total   int
	current int
	label   string
	start   time.Time
	active  bool
	writer  *os.File
}

var activeProgress *ProgressBar

// NewProgressBar creates a new progress bar that writes to stderr.
func NewProgressBar() *ProgressBar {
	return &ProgressBar{writer: os.Stderr}
}

// SetActiveProgress registers the progress bar for logger coordination.
func SetActiveProgress(p *ProgressBar) {
	activeProgress = p
}

// Start begins displaying progress with the given label and total count.
func (p *ProgressBar) Start(label string, total int) {
	p.label = label
	p.total = total
	p.current = 0
	p.start = time.Now()
	p.active = true
	p.render()
}

// Increment advances the progress bar by one.
func (p *ProgressBar) Increment() {
	if !p.active {
		return
	}
	p.current++
	p.render()
}

// Finish completes the progress bar and moves to a new line.
func (p *ProgressBar) Finish() {
	if !p.active {
		return
	}
	p.current = p.total
	p.render()
	fmt.Fprintln(p.writer)
	p.active = false
}

// Clear erases the progress bar line (used by logger before printing a message).
func (p *ProgressBar) Clear() {
	if !p.active {
		return
	}
	fmt.Fprintf(p.writer, "\r\033[K")
}

// Redraw redraws the progress bar (used by logger after printing a message).
func (p *ProgressBar) Redraw() {
	if !p.active {
		return
	}
	p.render()
}

func (p *ProgressBar) render() {
	if p.total <= 0 {
		return
	}
	pct := float64(p.current) / float64(p.total)
	filled := int(pct * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	elapsed := time.Since(p.start)
	elapsedStr := formatDuration(elapsed)

	bar := "\033[32m" + strings.Repeat(barFull, filled) + "\033[0m" + strings.Repeat(barEmpty, empty)
	fmt.Fprintf(p.writer, "\r  %s %s %d/%d (%d%%) [%s]",
		p.label, bar, p.current, p.total, int(pct*100), elapsedStr)
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
