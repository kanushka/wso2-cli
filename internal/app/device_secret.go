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

package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/oauthflow"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
)

// deviceSecretMargin is how long before its stated expiry a device secret is
// no longer presented, so an exchange is not sent with one about to lapse.
const deviceSecretMargin = time.Minute

// deviceSecretDeadline bounds one exchange. Nobody is waiting at a browser,
// so it is a request's deadline, not a login's.
const deviceSecretDeadline = 30 * time.Second

// asksForDeviceSecret reports whether an authorization asks ThunderID for a
// device secret: only the login session's, since the secret describes the
// sign-on that sign-in started and every later product is obtained
// from it.
func asksForDeviceSecret(identity contexts.Account, access contexts.ProductAccess) bool {
	return signsInAtThunderID(identity) && access.SessionRef == identity.LoginAccess().SessionRef
}

// fromDeviceSecret reports whether an access may be obtained by exchanging
// the login session's device secret: a session of its own at the login issuer,
// which is the one a browser authorization would otherwise be opened for.
func fromDeviceSecret(identity contexts.Account, access contexts.ProductAccess) bool {
	return signsInAtThunderID(identity) && access.Strategy == contexts.StrategySibling &&
		access.Issuer == identity.Auth.Issuer && access.SessionRef != identity.LoginAccess().SessionRef
}

// establishFromDeviceSecret obtains access's session by exchanging the stored
// device secret, and reports whether it did. A missing or expired secret, or
// one the provider no longer answers, is not an error: the secret is
// discarded and the caller authorizes through the browser instead.
func (s Shell) establishFromDeviceSecret(selected contexts.Selection, access contexts.ProductAccess) (oauthflow.Result, bool, error) {
	identity := selected.Identity
	if !fromDeviceSecret(identity, access) {
		return oauthflow.Result{}, false, nil
	}
	root, err := s.stateRoot()
	if err != nil {
		return oauthflow.Result{}, false, err
	}
	store := session.Store{StateRoot: root}
	loginRef := identity.LoginAccess().SessionRef
	login, err := store.Load(loginRef)
	if isNoSession(err) {
		return oauthflow.Result{}, false, nil
	}
	if err != nil || login.DeviceSecret == "" {
		return oauthflow.Result{}, false, err
	}
	now := time.Now()
	if !login.DeviceSecretExpiresAt.IsZero() && !login.DeviceSecretExpiresAt.After(now.Add(deviceSecretMargin)) {
		s.log.Debug("the device secret has expired", "credential_ref", loginRef)
		return oauthflow.Result{}, false, discardDeviceSecret(store, loginRef)
	}
	s.log.Debug("establishing a product session from the device secret",
		"context", selected.Context.Name,
		"product", access.Namespace,
		"issuer", access.Issuer,
		"client_id", access.ClientID,
		"scopes", strings.Join(access.Scopes, " "),
		"resource", access.Resource)
	ctx, cancel := context.WithTimeout(context.Background(), deviceSecretDeadline)
	defer cancel()
	granted, err := auth.ExchangeDeviceSecret(ctx, nil, access, login.DeviceSecret, now)
	if errors.Is(err, auth.ErrDeviceSecretSpent) {
		s.log.Debug("the identity provider no longer accepts the device secret", "credential_ref", loginRef)
		return oauthflow.Result{}, false, discardDeviceSecret(store, loginRef)
	}
	if err != nil {
		return oauthflow.Result{}, false, err
	}
	extra := map[string]any{"scope": strings.Join(granted.Granted, " ")}
	if granted.RefreshTokenExpiresIn > 0 {
		extra["refresh_token_expires_in"] = float64(granted.RefreshTokenExpiresIn)
	}
	token := (&oauth2.Token{
		AccessToken: granted.AccessToken, RefreshToken: granted.RefreshToken, Expiry: granted.ExpiresAt,
	}).WithExtra(extra)
	result := oauthflow.Result{Token: token, Subject: login.Subject, Name: login.Name, Email: login.Email}
	if err := s.storeSession(access, result, session.EstablishedByDeviceSecret); err != nil {
		return oauthflow.Result{}, false, err
	}
	// Best effort, like the browser path's own lines: the session is stored.
	_, _ = fmt.Fprintf(s.Streams.Err, "Authorized the %q product from this sign-in, without the browser.\n",
		access.Namespace)
	return result, true, nil
}

// discardDeviceSecret removes the device secret from the login session,
// keeping the session itself.
func discardDeviceSecret(store session.Store, loginRef string) error {
	return store.WithLock(loginRef, func() error {
		stored, err := store.Load(loginRef)
		if isNoSession(err) {
			return nil
		}
		if err != nil {
			return err
		}
		stored.DeviceSecret, stored.DeviceSecretExpiresAt = "", time.Time{}
		return store.Save(loginRef, stored)
	})
}
