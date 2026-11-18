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
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/wso2/wso2-cli/internal/contexts"
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
	"iam": {host: cloudAPIGateway, path: "/iam/v1"},
}

func (p cloudProduct) spec(e cloudEnvironment) productSpec {
	url := p.host(e) + p.path
	audience := p.audience
	if audience == "" {
		audience = url
	}
	return productSpec{URL: url, Audience: audience}
}

// onWSO2Cloud reports whether a context signs in to WSO2 Cloud, whose
// identity provider is ThunderID. There a token exchange carries no
// permission scopes, so a product descriptor's grant is set aside and the
// product is authorized on its own at the login issuer, for its own resource
// and scopes.
func onWSO2Cloud(c contexts.Context) bool {
	return c.Type == contexts.TypeCloud && c.Login.Provider == contexts.ProviderThunder
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

// recordCloudProducts records the installed WSO2 Cloud products an existing
// Cloud context lacks, before a login authorizes them.
//
// The environment file is read again rather than kept on the context: it is
// where the environment's values live, and the context document has no member
// for them. Without it a login still runs for what is recorded, and says what
// it could not record and where the file belongs.
func (s Shell) recordCloudProducts(root, env string, target contexts.Context, lookup descriptorLookup) error {
	name := target.Name
	missing := unrecordedCloudProducts(target, lookup)
	if len(missing) == 0 {
		return nil
	}
	environment, err := readCloudEnvironment(root, env)
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
		return document.Put(target), nil
	})
	if err != nil {
		return err
	}
	_, err = s.writeChange(root, plan)
	return err
}

// installCloudProducts installs each WSO2 Cloud product that is not installed,
// one at a time, so one the catalog cannot supply leaves the others and the
// sign-in standing. A product left out is not recorded, and a later login
// records it once its module is installed.
func (s Shell) installCloudProducts() error {
	var wanted []installNeed
	for _, namespace := range slices.Sorted(maps.Keys(cloudProducts)) {
		wanted = append(wanted, installNeed{Namespace: namespace})
	}
	plan, err := s.planInstalls(wanted)
	if err != nil {
		return err
	}
	for _, need := range plan.Missing {
		if err := s.install([]installNeed{need}); err != nil {
			if _, werr := fmt.Fprintf(s.Streams.Err, "Skipped %s: %v\n", need.Namespace, err); werr != nil {
				return werr
			}
		}
	}
	return nil
}
