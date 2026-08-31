// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package output

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Clock reports the current time. NewProgress takes one rather than calling
// time.Now itself, so a test can throttle a fake ten minutes in a single
// function call instead of actually waiting one.
type Clock interface {
	Now() time.Time
}

// SystemClock is the wall-clock Clock production uses.
type SystemClock struct{}

// Now reports the current time.
func (SystemClock) Now() time.Time { return time.Now() }

// progressInterval bounds how often a Progress repaints, whatever the read
// frequency. A caller may call Report once per network read, which for a fast
// local link can be thousands of times a second; without a floor here, the
// indicator itself would be the bottleneck it exists to report on.
const progressInterval = 200 * time.Millisecond

// Progress reports how much of one fixed-size operation has completed.
//
// Report may be called as often as the caller likes — deciding how often that
// becomes an actual paint is this type's job, not the caller's. Finish must be
// called exactly once, whether the operation succeeded, failed, or its context
// was cancelled, so that whatever this Progress drew is always cleaned up
// rather than left half-drawn under the next line of output.
type Progress interface {
	// Report records that read bytes, out of the total given to NewProgress,
	// have arrived so far.
	Report(read int64)
	// Finish cleans up. It is safe to call more than once; only the first call
	// has an effect.
	Finish()
}

// NewProgress builds the Progress a fixed-size operation of total bytes
// should report to.
//
// label names what is downloading — a module namespace, for this wave's one
// caller — so a run that downloads more than one thing in sequence (module
// update, moving several modules) draws a line that says which one, rather
// than an indistinguishable "Downloading... 12%" repeated once per module.
//
// w must be Streams.Err: progress is a diagnostic, and ADR 0003 reserves
// Streams.Out for the command's result. Which of three renderings NewProgress
// returns follows R3 in the wave 4 rulings:
//
//   - w is a terminal: a live indicator, repainted in place.
//   - w is not a terminal and log is enabled (--verbose): periodic
//     line-oriented updates through log, which already writes to Streams.Err.
//   - neither: nothing. A script reading a pipe gets no output it did not ask
//     for.
func NewProgress(w io.Writer, log *Logger, label string, total int64, clock Clock) Progress {
	switch {
	case IsTerminal(w):
		return &terminalProgress{w: w, label: label, total: total, clock: clock}
	case log.Enabled():
		return &verboseProgress{log: log, label: label, total: total, clock: clock}
	default:
		return NoProgress()
	}
}

// NoProgress is a Progress that renders nothing. It is what an install
// reports to when nothing downloads, and the default for a caller that never
// sets up progress reporting at all.
func NoProgress() Progress { return noProgress{} }

type noProgress struct{}

func (noProgress) Report(int64) {}
func (noProgress) Finish()      {}

// terminalProgress draws one line, repainted with a carriage return rather
// than a newline, so it updates in place instead of scrolling the terminal.
type terminalProgress struct {
	w     io.Writer
	label string
	total int64
	clock Clock
	last  time.Time
	drawn bool
	width int
	done  bool
}

func (p *terminalProgress) Report(read int64) {
	if p.done {
		return
	}
	now := p.clock.Now()
	if p.drawn && now.Sub(p.last) < progressInterval {
		return
	}
	p.last = now
	line := progressLine(p.label, read, p.total)
	// A failed write here has nothing useful to report to: this is a
	// diagnostic drawn best-effort over Streams.Err, not the operation
	// Report is tracking, so its error is dropped rather than turned into a
	// second failure mode for a download that may otherwise be succeeding.
	_, _ = fmt.Fprint(p.w, "\r"+line)
	p.drawn = true
	// Bytes, not display columns, and that is sound rather than lucky here.
	// Every part of this line is ASCII: progressLine renders digits, a
	// percentage, a unit and the label, and the only label this package is
	// given in production is a product namespace, which
	// internal/modules.ValidNamespace constrains to ^[a-z][a-z0-9-]{0,31}$ —
	// the catalog generator applies the same rule, so a namespace the shell
	// would refuse cannot be published in the first place. Hand a label
	// outside that alphabet and Finish would clear too few columns, leaving
	// residue; a caller wanting that would have to bring a display-width
	// measurement with it, which is a dependency this shell does not have and
	// does not currently need.
	p.width = len(line)
}

// Finish clears the drawn line rather than leaving it behind: a carriage
// return, a run of spaces at least as wide as the last thing painted, and a
// second carriage return, so the cursor is back at column zero with nothing
// left on screen for the next output to draw over.
func (p *terminalProgress) Finish() {
	if p.done {
		return
	}
	p.done = true
	if p.drawn {
		_, _ = fmt.Fprint(p.w, "\r"+strings.Repeat(" ", p.width)+"\r")
	}
}

// progressLine renders one human-readable progress line, naming what is
// downloading so a sequence of several (module update, moving several
// modules one after another) does not draw the same unlabelled line for each
// one. It never divides by a zero total: a total this package was not told is
// rendered as a running byte count instead of a percentage nothing can be
// computed against.
func progressLine(label string, read, total int64) string {
	if total <= 0 {
		return fmt.Sprintf("Downloading %s... %s", label, formatBytes(read))
	}
	percent := read * 100 / total
	if percent > 100 {
		percent = 100
	}
	return fmt.Sprintf("Downloading %s... %s / %s (%d%%)", label, formatBytes(read), formatBytes(total), percent)
}

// formatBytes renders a byte count the way a person reads it, not the way a
// machine counts it.
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for value := n / unit; value >= unit; value /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// verboseProgress writes one log line per interval rather than drawing over
// itself, because a non-terminal reader — a CI log, a file — has no cursor to
// draw in place with, and a scrolling series of timestamped lines is what
// --verbose already promises through Logger.
type verboseProgress struct {
	log   *Logger
	label string
	total int64
	clock Clock
	last  time.Time
	began bool
	done  bool
}

func (p *verboseProgress) Report(read int64) {
	if p.done {
		return
	}
	now := p.clock.Now()
	if p.began && now.Sub(p.last) < progressInterval {
		return
	}
	p.began = true
	p.last = now
	p.log.Debug("download progress", "namespace", p.label, "bytes_read", read, "bytes_total", p.total)
}

func (p *verboseProgress) Finish() {
	p.done = true
}
