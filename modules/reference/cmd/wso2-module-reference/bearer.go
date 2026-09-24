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

package main

import (
	"errors"
	"net"
	"net/http"
	"strings"
)

// bearerClient carries the brokered access token to the product. It follows a
// redirect only to HTTPS, or to plain HTTP on a loopback host, because a
// redirect keeps the Authorization header when the host stays the same: an
// HTTPS endpoint answering with a redirect to its own host over http would
// otherwise hand the token to anyone on the path. The shell checks the url it
// gives the module; only the module sees where a request ends.
var bearerClient = &http.Client{
	CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if !secure(request) {
			return errors.New("refused a redirect to a URL not served over HTTPS")
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	},
}

// secure is the shell's rule for where an access token may go: HTTPS, or
// plain HTTP only on this machine's loopback interface. A name is not
// resolved, so a host that merely resolves to loopback does not count.
func secure(request *http.Request) bool {
	switch strings.ToLower(request.URL.Scheme) {
	case "https":
		return true
	case "http":
		host := request.URL.Hostname()
		if strings.EqualFold(host, "localhost") {
			return true
		}
		ip := net.ParseIP(host)
		return ip != nil && ip.IsLoopback()
	}
	return false
}
