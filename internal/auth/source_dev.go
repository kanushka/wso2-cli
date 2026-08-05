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

package auth

import (
	"fmt"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/devtoken"
)

// devSource mints the architecture proof's fixture token.
//
// It holds the credential it was built with for the length of one invocation
// and hands out something derived from it. The credential itself never reaches
// a grant, a problem, or the module: what crosses the boundary is a token that
// the proof's own status service verifies and nothing else accepts.
type devSource struct {
	// namespace is the module asking, named in the one refusal this source has.
	namespace string
	// credential is the fixture issuer's signing key.
	credential string
	// organization is what access is bound to, from the selected context.
	organization string
	// invocation is the command access is bound to.
	invocation string
}

func (s devSource) mint(request Request, now time.Time) (Grant, error) {
	token, err := devtoken.Mint(s.credential, devtoken.Claims{
		Audience:     request.Audience,
		Scopes:       request.Scopes,
		Organization: s.organization,
		Invocation:   s.invocation,
	}, now)
	if err != nil {
		// The issuer's own error may name what it was given, so it is not
		// carried into a problem the shell renders.
		return Grant{}, denial("auth.access_not_issued",
			fmt.Sprintf("the shell could not issue access for the %q module", s.namespace),
			"Retry the command. Report the failure if it persists.")
	}
	return Grant{Token: token, ExpiresAt: now.Add(devtoken.Lifetime).UTC()}, nil
}
