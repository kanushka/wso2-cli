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

package app_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// errNoBrowser is what a machine with no browser answers.
var errNoBrowser = errors.New("this machine has no browser to open")

// syncBuilder is an output stream a test can read while the command under test
// is still writing to it, which is how a test plays the user who copies the
// printed authorization URL into a browser.
type syncBuilder struct {
	mutex   sync.Mutex
	builder strings.Builder
}

func (s *syncBuilder) Write(data []byte) (int, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.builder.Write(data)
}

func (s *syncBuilder) String() string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.builder.String()
}

// waitForURL returns the first URL the command printed, waiting for it.
func waitForURL(t *testing.T, printed *syncBuilder) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(printed.String(), "\n") {
			if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
				return line
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the login never printed an authorization URL, printed:\n%s", printed.String())
	return ""
}
