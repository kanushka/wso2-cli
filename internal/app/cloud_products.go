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
	"maps"
	"slices"
	"strings"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// cloudSharedResource is the resource server the WSO2 Cloud products that
// share one gateway are authorized under, each with its own scope tree.
const cloudSharedResource = "urn:wso2:cloud"

// cloudProduct is where one WSO2 Cloud product answers in an environment, and
// what its access is bound to.
type cloudProduct struct {
	host func(cloudEnvironment) string
	path string
	// audience is the resource server the product is authorized under, or
	// empty when that is the product's own URL.
	audience string
	// scopes, when set, replace the module descriptor's on WSO2 Cloud.
	scopes []string
}

func cloudProductGateway(e cloudEnvironment) string {
	return fmt.Sprintf("https://%s-wso2cloud.gateway.%s", e.Environment, e.BaseDomain)
}

func cloudAPIGateway(e cloudEnvironment) string {
	return fmt.Sprintf("https://api-%s.gateway.%s", e.Environment, e.BaseDomain)
}

// cloudProducts are the products a WSO2 Cloud context records by itself, by
// namespace.
var cloudProducts = map[string]cloudProduct{
	"apim": {host: cloudProductGateway, path: "/apip-platform-api-apip-platform-api-endpoint",
		audience: cloudSharedResource},
	"intg": {host: cloudProductGateway, path: "/ipaas-service-ipaas-api-endpoint", audience: cloudSharedResource},
	"am": {host: cloudProductGateway, path: "/agent-manager-service-agent-manager-api/api/v1",
		audience: cloudSharedResource},
	// The preview iam module declares the self-hosted permission system beside
	// WSO2 Cloud's iam:system, and asks for whatever its record holds; Cloud
	// knows only iam:system.
	"iam": {host: cloudAPIGateway, path: "/iam/v1", scopes: []string{"iam:system"}},
}

func (p cloudProduct) spec(e cloudEnvironment) productSpec {
	return productSpec{URL: p.host(e) + p.path}
}

// signsInAtThunderID reports whether an account's login provider is ThunderID,
// which is what WSO2 Cloud and the device secret both depend on.
func signsInAtThunderID(account contexts.Account) bool {
	return account.Auth.Provider == contexts.ProviderThunder
}

// unrecordedCloudProducts are the installed WSO2 Cloud products a context
// does not record yet, in namespace order. A record already there, however it
// got there, is the user's and is left as it is.
func unrecordedCloudProducts(target contexts.Context, lookup descriptorLookup) []string {
	var missing []string
	for _, namespace := range slices.Sorted(maps.Keys(cloudProducts)) {
		if _, recorded := target.Products[namespace]; recorded || lookup(namespace) == nil {
			continue
		}
		missing = append(missing, namespace)
	}
	return missing
}

// withCloudProducts records the named products on a WSO2 Cloud context, where
// the environment says they answer.
func withCloudProducts(target contexts.Context, environment cloudEnvironment, namespaces []string,
	lookup descriptorLookup) (contexts.Context, error) {
	products := maps.Clone(target.Products)
	if products == nil {
		products = map[string]contexts.Product{}
	}
	for _, namespace := range namespaces {
		product, err := resolveProduct(namespace, lookup(namespace), cloudProducts[namespace].spec(environment), target)
		if err != nil {
			return target, err
		}
		products[namespace] = product
	}
	target.Products = products
	return target, nil
}

// cloudProductScopes are the scopes a Cloud login records for a product: the
// Cloud product table's where it states any, else the installed descriptor's.
func cloudProductScopes(namespace string, descriptor *modules.ProductDescriptor) []string {
	if known := cloudProducts[namespace].scopes; known != nil {
		return known
	}
	return descriptor.Scopes
}

// staleCloudScopes are the recorded WSO2 Cloud products whose scopes differ
// from what the installed module now asks for, in namespace order, among the
// records a Cloud login wrote itself: those still at the URL and audience the
// environment gives them. A record pointed anywhere else was edited by a
// person and is left as it is.
func staleCloudScopes(target contexts.Context, environment cloudEnvironment, lookup descriptorLookup) []string {
	var stale []string
	for _, namespace := range slices.Sorted(maps.Keys(cloudProducts)) {
		recorded, found := target.Products[namespace]
		descriptor := lookup(namespace)
		if !found || descriptor == nil {
			continue
		}
		known := cloudProducts[namespace]
		url := known.spec(environment).URL
		audience := known.audience
		if audience == "" {
			audience = url
		}
		if recorded.Endpoint != url || recorded.Audience != audience {
			continue
		}
		if !sameScopes(recorded.Scopes, cloudProductScopes(namespace, descriptor)) {
			stale = append(stale, namespace)
		}
	}
	return stale
}

// recordCloudProducts records the installed WSO2 Cloud products an existing
// Cloud context lacks, before a login authorizes them, and brings the scopes
// of the records it wrote itself up to what the installed modules ask for.
// The records are the login's own, not a person's, so the exception to a
// product update never editing a context (ADR 0016) is announced in one line
// per product.
//
// The environment file is read again rather than kept on the context: it is
// where the environment's values live, and the context document has no member
// for them. Without it a login still runs for what is recorded, and says what
// it could not record and where the file belongs.
func (s Shell) recordCloudProducts(root, env string, target contexts.Context, lookup descriptorLookup) error {
	name := target.Name
	missing := unrecordedCloudProducts(target, lookup)
	environment, err := readCloudEnvironment(root, env)
	if err != nil && len(missing) == 0 {
		return nil
	}
	var refused problem.Problem
	if errors.As(err, &refused) && refused.Code == "shell.cloud_environment_unknown" {
		_, err := fmt.Fprintf(s.Streams.Err, "Not recorded on the %q context: %s. Place the %s environment "+
			"file at %s and log in again to record them.\n",
			name, strings.Join(missing, ", "), env, cloudEnvironmentPath(root, env))
		return err
	}
	if err != nil {
		return err
	}
	stale := staleCloudScopes(target, environment, lookup)
	if len(missing) == 0 && len(stale) == 0 {
		return nil
	}
	plan, err := s.planChange(root, func(document contexts.Document) (contexts.Document, error) {
		target, found := document.Find(name)
		if !found {
			return document, nil
		}
		// Frozen first, so the login keeps running for the product it ran for
		// whatever a newly recorded one's namespace sorts as.
		target = target.Freeze()
		target, err := withCloudProducts(target, environment, unrecordedCloudProducts(target, lookup), lookup)
		if err != nil {
			return document, err
		}
		for _, namespace := range staleCloudScopes(target, environment, lookup) {
			product := target.Products[namespace]
			product.Scopes = slices.Clone(cloudProductScopes(namespace, lookup(namespace)))
			target.Products[namespace] = product
		}
		return document.Put(target), nil
	})
	if err != nil {
		return err
	}
	if _, err := s.writeChange(root, plan); err != nil {
		return err
	}
	for _, namespace := range stale {
		if _, err := fmt.Fprintf(s.Streams.Err, "Updated the %q product's permissions on the %q context to the "+
			"ones the installed module asks for.\n", namespace, name); err != nil {
			return err
		}
	}
	return nil
}

// installCloudProducts installs each WSO2 Cloud product that is not installed,
// on every login to a Cloud context, one at a time, so one that cannot be
// installed leaves the others and the sign-in standing. A product that failed
// to install gets one line saying why; it is not recorded, and the next login
// tries it again.
func (s Shell) installCloudProducts() error {
	var wanted []installNeed
	for _, namespace := range slices.Sorted(maps.Keys(cloudProducts)) {
		wanted = append(wanted, installNeed{Namespace: namespace})
	}
	plan, err := s.planInstalls(wanted)
	if err != nil || len(plan.Missing) == 0 {
		return err
	}
	installer, err := s.installer()
	if err != nil {
		return err
	}
	for _, need := range plan.Missing {
		ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
		installed, err := installer.Run(ctx, installRequest(need))
		cancel()
		line := fmt.Sprintf("Installed %s v%s.", installed.Namespace, installed.Version)
		var refused problem.Problem
		switch {
		case errors.As(err, &refused) && refused.Code == "catalog.unknown_module":
			// Nothing the user can do about it, so it is said only under --verbose.
			s.log.Debug("skipped a Cloud product: it is not published in the module catalog",
				"namespace", need.Namespace)
			continue
		case err != nil:
			line = fmt.Sprintf("Skipped %s: %v. The next login tries again.", need.Namespace, err)
		}
		if _, err := fmt.Fprintln(s.Streams.Err, line); err != nil {
			return err
		}
	}
	return nil
}
