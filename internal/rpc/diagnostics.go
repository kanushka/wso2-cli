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

package rpc

import (
	"strings"
	"sync"
)

// Diagnostics is what a module wrote to its standard error, bounded.
//
// Module diagnostics never reach standard output, so a module cannot corrupt
// the shell's table or JSON however much it writes. They are also capped, so a
// looping module cannot exhaust shell memory.
type Diagnostics struct {
	// Text is the captured output, at most the configured limit.
	Text string
	// Truncated reports that the module wrote more than the limit and the
	// remainder was discarded.
	Truncated bool
}

// Lines splits the diagnostics into non-empty lines, for one rendered warning
// per line. A trailing partial line from truncated output is kept, because it
// may carry the only useful detail.
func (d Diagnostics) Lines() []string {
	var lines []string
	for _, line := range strings.Split(d.Text, "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines
}

// boundedWriter keeps the first limit bytes written to it and counts the rest
// as truncated.
//
// It keeps the head rather than the tail because a module's first diagnostics
// describe why it went wrong; a module stuck in a loop repeats itself
// afterwards.
type boundedWriter struct {
	mutex     sync.Mutex
	limit     int
	kept      []byte
	truncated bool
}

func newBoundedWriter(limit int) *boundedWriter {
	if limit <= 0 {
		limit = DefaultDiagnosticLimit
	}
	return &boundedWriter{limit: limit}
}

// Write never reports a short write, so a module that exceeds the limit sees a
// working stream and keeps running rather than failing on a broken pipe.
func (b *boundedWriter) Write(p []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	remaining := b.limit - len(b.kept)
	if remaining <= 0 {
		b.truncated = b.truncated || len(p) > 0
		return len(p), nil
	}
	if len(p) > remaining {
		b.kept = append(b.kept, p[:remaining]...)
		b.truncated = true
		return len(p), nil
	}
	b.kept = append(b.kept, p...)
	return len(p), nil
}

// Diagnostics reports what was captured.
func (b *boundedWriter) Diagnostics() Diagnostics {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return Diagnostics{Text: string(b.kept), Truncated: b.truncated}
}
