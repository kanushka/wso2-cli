# Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
#
# WSO2 LLC. licenses this file to you under the Apache License,
# Version 2.0 (the "License"); you may not use this file except
# in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.

# Entry points a contributor runs by hand.
#
# The deterministic gate is scripts/acceptance.sh and needs no credentials; it
# is what CI runs. The targets below that reach a real deployment are separated
# from it by the `smoke` build tag, so nothing here can be pulled into the
# default `go test ./...` by accident.
#
# See test/smoke/RUNNING.md for the variables the live targets read, and
# docs/guides/login.md for how to register the application they read them from.

GO ?= go

# golangci-lint is installed into the Go binary directory, which is not on every
# contributor's PATH. Resolve it there when the shell cannot find it, so `make
# lint` works from a plain checkout rather than only for whoever set PATH up.
GOLANGCI_LINT ?= $(shell command -v golangci-lint 2>/dev/null || \
	echo "$$($(GO) env GOPATH)/bin/golangci-lint")

SMOKE_PACKAGE := ./test/smoke/

# Live runs are never answered from the test cache. A cached pass would report
# a deployment as working without having contacted it, which is the one result
# a smoke target must not be able to produce. The timeout is generous because a
# human is signing in inside it.
SMOKE_FLAGS := -tags smoke -count=1 -v -timeout 30m

.DEFAULT_GOAL := help

.PHONY: help
help:
	@echo 'Deterministic, no credentials needed:'
	@echo '  make test                 Run every test in the default gate, with the race detector.'
	@echo '  make vet                  Vet the shell, including the build-tagged live runs.'
	@echo '  make lint                 Lint the shell, including the build-tagged live runs.'
	@echo '  make acceptance           Run the full architecture-proof acceptance gate.'
	@echo '  make smoke-build          Compile the live runs without executing them.'
	@echo ''
	@echo 'Against a real deployment (Asgardeo or a local Identity Server 7.x):'
	@echo '  make smoke-login          Log in and broker one acquisition. Opens a browser.'
	@echo '  make empirical-asgardeo   Run the two one-time experiments and print their verdicts.'
	@echo ''
	@echo 'Both live targets skip cleanly when no deployment is configured.'
	@echo 'See test/smoke/RUNNING.md.'

.PHONY: test
test:
	$(GO) test ./... -race -count=1

.PHONY: vet
vet:
	$(GO) vet ./...
	$(GO) vet -tags smoke $(SMOKE_PACKAGE)

# The default golangci-lint run cannot see the live runs: the tag that keeps
# them out of the default gate keeps the linter out too, so they are linted by a
# second invocation that opts into the tag.
.PHONY: lint
lint:
	$(GOLANGCI_LINT) run
	$(GOLANGCI_LINT) run --build-tags=smoke $(SMOKE_PACKAGE)...

.PHONY: acceptance
acceptance:
	./scripts/acceptance.sh

# Proves the live runs still compile against the shell they drive. The default
# gate cannot do this for them: the tag that keeps them out of it also keeps
# them from being built by it, so without this target they rot silently.
.PHONY: smoke-build
smoke-build:
	$(GO) test -tags smoke -run '^$$' $(SMOKE_PACKAGE)

# Signs a human in against the configured deployment, proves the refresh token
# reached the operating system's secure store, and brokers one acquisition on
# top of the session. Skips when no deployment is configured.
.PHONY: smoke-login
smoke-login:
	$(GO) test $(SMOKE_FLAGS) $(SMOKE_PACKAGE) -run TestLoginSmoke

# Answers the two questions the redirect-and-narrowing research left open, and
# prints one verdict line each for recording in that document. Skips when no
# deployment is configured.
.PHONY: empirical-asgardeo
empirical-asgardeo:
	WSO2_EMPIRICAL=1 $(GO) test $(SMOKE_FLAGS) $(SMOKE_PACKAGE) -run TestAsgardeoEmpirical
