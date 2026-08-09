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

package oauthflow

import (
	"math"
	"testing"
	"time"
)

// TestUsableIntervalRefusesWhatWouldNotBeADuration proves the shell does not
// carry a deployment's polling interval into its arithmetic unchecked.
//
// The values here are not hypothetical shapes. x/oauth2 substitutes RFC 8628's
// default only when the advertised interval is exactly zero, and hands
// everything else to time.NewTicker — which panics on a non-positive duration.
// A negative interval reaches it directly; an interval beyond nine billion
// seconds reaches it as a negative one, because the conversion to nanoseconds
// overflows int64. Either would replace a typed refusal with a stack trace and
// an exit code outside the class list.
func TestUsableIntervalRefusesWhatWouldNotBeADuration(t *testing.T) {
	// The threshold the multiplication overflows past, computed rather than
	// written down so it cannot drift from the type it describes.
	overflowing := int64(math.MaxInt64/int64(time.Second)) + 1

	for name, testcase := range map[string]struct {
		advertised int64
		want       int64
	}{
		"absent is the specification's default": {advertised: 0, want: defaultPollIntervalSeconds},
		"negative is not a wait":                {advertised: -1, want: defaultPollIntervalSeconds},
		"deeply negative is not a wait":         {advertised: math.MinInt64, want: defaultPollIntervalSeconds},
		"an overflowing interval is clamped":    {advertised: overflowing, want: maxPollIntervalSeconds},
		"the largest possible is clamped":       {advertised: math.MaxInt64, want: maxPollIntervalSeconds},
		"a sane interval is honoured":           {advertised: 7, want: 7},
		"the ceiling itself is honoured":        {advertised: maxPollIntervalSeconds, want: maxPollIntervalSeconds},
	} {
		t.Run(name, func(t *testing.T) {
			got := usableInterval(testcase.advertised)
			if got != testcase.want {
				t.Fatalf("usableInterval(%d) = %d, want %d", testcase.advertised, got, testcase.want)
			}
			// The property behind every row: whatever comes back must be a
			// duration a ticker will accept, which is what the panic was.
			if duration := time.Duration(got) * time.Second; duration <= 0 {
				t.Fatalf("usableInterval(%d) yields %v, which time.NewTicker panics on",
					testcase.advertised, duration)
			}
		})
	}
}

// TestTheClampNeverPollsSoonerThanADeploymentAsked guards the one way this
// clamp could do harm. Clamping an interval *down* would mean polling a
// deployment faster than it consented to, which is the abuse RFC 8628 section
// 3.5 exists to prevent — so the ceiling has to sit above any interval a login
// could actually act on. A login is bounded by its own deadline, and an
// interval longer than that produces no poll at all whether it is clamped or
// not.
func TestTheClampNeverPollsSoonerThanADeploymentAsked(t *testing.T) {
	const longestLoginDeadline = 15 * time.Minute
	if ceiling := time.Duration(maxPollIntervalSeconds) * time.Second; ceiling <= longestLoginDeadline {
		t.Fatalf("the interval ceiling %v is within the %v a login may run for, so clamping "+
			"could make the shell poll sooner than a deployment asked", ceiling, longestLoginDeadline)
	}
}
