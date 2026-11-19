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

package diag

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestFromContextDiscardsWhenNothingWasAttached(t *testing.T) {
	if FromContext(context.Background()).Enabled(context.Background(), -8) {
		t.Fatal("a context with no logger yields one that writes")
	}
}

func TestFromContextReturnsTheAttachedLogger(t *testing.T) {
	var captured bytes.Buffer
	ctx := WithLogger(context.Background(), New(&captured, false))
	FromContext(ctx).Debug("a diagnostic")
	if !strings.Contains(captured.String(), "a diagnostic") {
		t.Fatalf("the attached logger was not used: %q", &captured)
	}
}
