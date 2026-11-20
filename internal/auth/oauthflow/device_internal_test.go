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
	"bytes"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
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

// Code exchange and device polling post the code or device code, and a 307
// resends it. Both flows' clients refuse a redirect off HTTPS.
func TestLoginAndDeviceClientsRefuseAPlaintextRedirect(t *testing.T) {
	target, err := url.Parse("http://issuer.example/token")
	if err != nil {
		t.Fatal(err)
	}
	request := &http.Request{URL: target}
	for name, client := range map[string]*http.Client{
		"login":  Login{}.httpClient(),
		"device": DeviceLogin{}.httpClient(),
	} {
		if client.CheckRedirect == nil || client.CheckRedirect(request, nil) == nil {
			t.Fatalf("%s: a redirect to plain http was allowed", name)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type countingReader struct {
	reader io.Reader
	read   int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	c.read += n
	return n, err
}

func TestTheSlowDownNoticerReadsABoundedPrefixAndPassesTheWholeBodyOn(t *testing.T) {
	const tokenURL = "https://issuer.example.test/token"
	payload := append([]byte(`{"error":"slow_down","error_description":"`),
		bytes.Repeat([]byte("x"), 4<<20)...)
	payload = append(payload, `"}`...)
	counted := &countingReader{reader: bytes.NewReader(payload)}
	var said bytes.Buffer
	noticer := &slowDownNoticer{tokenURL: tokenURL, out: &said,
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(counted)}, nil
		})}

	request, _ := http.NewRequest(http.MethodPost, tokenURL, nil)
	response, err := noticer.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	if counted.read > slowDownInspectLimit+1 {
		t.Errorf("read %d bytes to inspect the answer, want at most %d", counted.read, slowDownInspectLimit+1)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil || !bytes.Equal(body, payload) {
		t.Errorf("the body passed on is %d bytes (%v), want the %d sent", len(body), err, len(payload))
	}

	small := &countingReader{reader: bytes.NewReader([]byte(`{"error":"slow_down"}`))}
	noticer.base = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(small)}, nil
	})
	if _, err := noticer.RoundTrip(request); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(said.String(), slowDownNotice); got != 1 {
		t.Errorf("the notice was said %d times, want once:\n%s", got, said.String())
	}
}
