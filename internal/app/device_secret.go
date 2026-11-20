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
	"github.com/wso2/wso2-cli/sdk/problem"
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
//
// A context whose products are authorized in the browser asks for none: it
// would never present one.
func asksForDeviceSecret(identity contexts.Account, access contexts.ProductAccess) bool {
	return signsInAtThunderID(identity) && access.SessionRef == identity.LoginAccess().SessionRef &&
		identity.Auth.ProductSessionMethod() == contexts.ProductSessionsExchange
}

// fromDeviceSecret reports whether an access is obtained by exchanging the
// login session's device secret: a session of its own at the login issuer, on
// a context whose productSessions setting is the exchange.
func fromDeviceSecret(identity contexts.Account, access contexts.ProductAccess) bool {
	return signsInAtThunderID(identity) && access.Strategy == contexts.StrategySibling &&
		access.Issuer == identity.Auth.Issuer && access.SessionRef != identity.LoginAccess().SessionRef &&
		identity.Auth.ProductSessionMethod() == contexts.ProductSessionsExchange
}

// establishFromDeviceSecret obtains access's session by exchanging the stored
// device secret, and reports whether the access is one the exchange serves. For
// such an access a missing, expired or refused secret is refused with
// auth.device_secret_unavailable rather than authorized in the browser: the
// person chose the exchange, and productSessions is how they choose the
// browser. A secret the provider no longer answers is discarded first.
func (s Shell) establishFromDeviceSecret(selected contexts.Selection, access contexts.ProductAccess) (oauthflow.Result, bool, error) {
	identity := selected.Identity
	if !fromDeviceSecret(identity, access) {
		return oauthflow.Result{}, false, nil
	}
	again := s.contextArgument(selected.Context.Name)
	unavailable := func(reason string) (oauthflow.Result, bool, error) {
		return oauthflow.Result{}, true, deviceSecretUnavailable(access.Namespace, reason,
			fmt.Sprintf("Run wso2 login%s to sign in again, which issues a new device secret, or %s.",
				again, browserSetting(selected.Context.Name)))
	}
	root, err := s.stateRoot()
	if err != nil {
		return oauthflow.Result{}, true, err
	}
	store := session.Store{StateRoot: root}
	loginRef := identity.LoginAccess().SessionRef
	login, err := store.Load(loginRef)
	if isNoSession(err) {
		return unavailable("no sign-in is stored to exchange a device secret from")
	}
	if err != nil {
		return oauthflow.Result{}, true, err
	}
	discarded := func(reason string) (oauthflow.Result, bool, error) {
		if err := discardDeviceSecret(store, loginRef, login.DeviceSecret); err != nil {
			return oauthflow.Result{}, true, err
		}
		return unavailable(reason)
	}
	if login.DeviceSecret == "" {
		return oauthflow.Result{}, true, s.noDeviceSecretKept(selected, access)
	}
	now := time.Now()
	if !login.DeviceSecretExpiresAt.IsZero() && !login.DeviceSecretExpiresAt.After(now.Add(deviceSecretMargin)) {
		s.log.Debug("the device secret has expired", "credential_ref", loginRef)
		return discarded("the device secret has expired")
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
		return discarded("the identity provider no longer accepts the device secret, since the sign-on " +
			"it describes has ended")
	}
	if err != nil {
		return oauthflow.Result{}, true, err
	}
	extra := map[string]any{}
	if granted.Granted != nil {
		extra["scope"] = strings.Join(granted.Granted, " ")
	}
	if granted.RefreshTokenExpiresIn > 0 {
		extra["refresh_token_expires_in"] = float64(granted.RefreshTokenExpiresIn)
	}
	token := (&oauth2.Token{
		AccessToken: granted.AccessToken, RefreshToken: granted.RefreshToken, Expiry: granted.ExpiresAt,
	}).WithExtra(extra)
	result := oauthflow.Result{Token: token, Subject: login.Subject, Name: login.Name, Email: login.Email}
	if checkIssuedIn(selected.Context, result) != nil {
		// The sign-on the secret describes is in another organization, and
		// so will every later exchange be.
		s.log.Debug("the device secret answers in another organization", "credential_ref", loginRef)
		s.revokeUnused(access, result)
		if identity.Auth.Kind == contexts.KindOAuthDevice {
			if err := discardDeviceSecret(store, loginRef, login.DeviceSecret); err != nil {
				return oauthflow.Result{}, true, err
			}
			return oauthflow.Result{}, true, deviceSecretUnavailable(access.Namespace,
				"the device secret answers in another organization than the context records",
				s.chooseOrganizationRecovery(selected.Context.Name, selected.Context.Organization)+" Or "+
					browserSetting(selected.Context.Name)+".")
		}
		return discarded("the device secret answers in another organization than the context records")
	}
	if err := s.storeSession(access, result, session.EstablishedByDeviceSecret); err != nil {
		return oauthflow.Result{}, true, err
	}
	// Best effort, like the browser path's own lines: the session is stored.
	_, _ = fmt.Fprintf(s.Streams.Err, "Authorized the %q product from this sign-in, without the browser.\n",
		access.Namespace)
	return result, true, nil
}

const deviceSecretUnavailableCode = "auth.device_secret_unavailable"

// deviceSecretUnavailable refuses a product the exchange cannot serve. Every
// recovery names the setting that authorizes products in the browser instead.
func deviceSecretUnavailable(namespace, reason, recovery string) error {
	return problem.New(problem.CategoryAuthPolicy, deviceSecretUnavailableCode,
		fmt.Sprintf("the %q product cannot be authorized from this sign-in: %s", namespace, reason)).
		WithRecovery(recovery)
}

func browserSetting(contextName string) string {
	return fmt.Sprintf("run wso2 context edit and set productSessions: browser in the login of the %q "+
		"context to authorize each product in the browser instead", contextName)
}

// noDeviceSecretKept refuses a product when the sign-in kept no device secret,
// saying which of the two reasons it was: a provider that offers none, which
// no sign-in changes, or one that handed none out this time, as a provider not
// yet issuing one on a reused browser sign-on does. A plain "sign in again"
// would loop in both.
func (s Shell) noDeviceSecretKept(selected contexts.Selection, access contexts.ProductAccess) error {
	issuer := selected.Identity.Auth.Issuer
	ctx, cancel := context.WithTimeout(context.Background(), deviceSecretDeadline)
	defer cancel()
	offers, err := oauthflow.OffersDeviceSecret(ctx, issuer, selected.Identity.Auth.IssuerID)
	if err == nil && !offers {
		return deviceSecretUnavailable(access.Namespace,
			fmt.Sprintf("the sign-in service at %s does not offer a sign-in shared between products", issuer),
			"Run wso2 context edit and set productSessions: browser in the login of the "+
				fmt.Sprintf("%q context; productSessions: exchange cannot work with this sign-in service.",
					selected.Context.Name))
	}
	again := s.contextArgument(selected.Context.Name)
	return deviceSecretUnavailable(access.Namespace,
		"the sign-in service handed out no shared sign-in, likely because the browser was already signed in",
		fmt.Sprintf("Run wso2 logout%s, then wso2 login%s, to sign in afresh, or %s.", again, again,
			browserSetting(selected.Context.Name)))
}

// browserProductsNotice is said once, when a context's productSessions is
// recorded as browser because its provider offers no device secret.
const browserProductsNotice = "This sign-in service can't share a sign-in between products, so each product " +
	"signs in through the browser. Change it with wso2 context edit."

// settleProductSessions records a ThunderID context's productSessions the
// first time it is needed, when the context states none: exchange where the
// provider offers a device secret, and on WSO2 Cloud, which does; browser
// otherwise. From then on the recorded value decides, and a stated one is
// never changed.
func (s Shell) settleProductSessions(selected contexts.Selection) (contexts.Selection, error) {
	if !signsInAtThunderID(selected.Identity) || selected.Context.Login.ProductSessions != "" {
		return selected, nil
	}
	method := contexts.ProductSessionsExchange
	if !selected.Context.WSO2Cloud() {
		ctx, cancel := context.WithTimeout(context.Background(), deviceSecretDeadline)
		defer cancel()
		offers, err := oauthflow.OffersDeviceSecret(ctx, selected.Identity.Auth.Issuer,
			selected.Identity.Auth.IssuerID)
		if err != nil {
			return selected, err
		}
		if !offers {
			method = contexts.ProductSessionsBrowser
		}
	}
	root, err := s.stateRoot()
	if err != nil {
		return selected, err
	}
	name := selected.Context.Name
	err = contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
		found, ok := document.Find(name)
		if !ok || found.Login.ProductSessions != "" {
			return document, nil
		}
		found.Login.ProductSessions = method
		return document.Put(found), nil
	})
	if err != nil {
		return selected, s.explainWriteRefusal(root, err)
	}
	s.log.Debug("recorded how products get their sessions", "context", name, "product_sessions", method)
	if method == contexts.ProductSessionsBrowser {
		if _, err := fmt.Fprintln(s.Streams.Err, browserProductsNotice); err != nil {
			return selected, err
		}
	}
	selected.Context.Login.ProductSessions = method
	selected.Identity = selected.Context.Account()
	return selected, nil
}

// contextArgument is " --context <name>" when name is not the context a
// command selects by default, so a recovery reruns against the context that
// failed (ADR 0023).
func (s Shell) contextArgument(name string) string {
	root, err := s.stateRoot()
	if err != nil {
		return ""
	}
	document, err := contexts.Load(root)
	if err != nil || document.DefaultContext == name {
		return ""
	}
	return " --context " + name
}

// discardDeviceSecret removes the device secret from the login session,
// keeping the session itself. A secret a newer login stored in its place is
// not the one that failed, and stays.
func discardDeviceSecret(store session.Store, loginRef, secret string) error {
	return store.WithLock(loginRef, func() error {
		stored, err := store.Load(loginRef)
		if isNoSession(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if stored.DeviceSecret != secret {
			return nil
		}
		stored.DeviceSecret, stored.DeviceSecretExpiresAt = "", time.Time{}
		return store.Save(loginRef, stored)
	})
}
