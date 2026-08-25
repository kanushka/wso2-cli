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

# GoReleaser builds the release artifacts. It is pinned and run through `go run`
# rather than installed, so a contributor reproducing a release uses the same
# version CI does without adding a tool to their machine. The same version is
# pinned in .github/workflows/release.yml; move both together.
GORELEASER ?= $(GO) run github.com/goreleaser/goreleaser/v2@v2.17.1

# A file describing one deployment, sourced by the live targets when it exists.
#
# Go has no dotenv convention and this module stays lean, so nothing parses this
# file: it is an ordinary shell fragment, and `. test/smoke/.env` in your own
# shell has exactly the same effect as letting these targets read it. That is
# the point — one file, usable either way, and no dependency to make it work.
#
# Keep one per deployment and name the one you want:
#
#   make smoke-login SMOKE_ENV=test/smoke/asgardeo.env
#
# Values in the file overwrite what the calling shell already exported, so
# switching deployments does not need a fresh terminal. See test/smoke/env.example.
SMOKE_ENV ?= test/smoke/.env

# `set -a` exports every variable the file assigns, so it works whether the file
# writes `NAME=value` or the `export NAME=value` lines the registration output
# prints. Sourcing happens inside the recipe's own shell and reaches nothing else.
#
# The `case` keeps a bare filename from being looked up on PATH, which is what
# POSIX `.` does with an argument carrying no slash.
#
# Nothing here may contain a `#`. Make strips one and everything after it from a
# variable's value without knowing that it fell inside a shell quote, and the
# recipe then reaches the shell with the quote still open.
smoke_env = set -a; \
	if [ -f '$(SMOKE_ENV)' ]; then \
		echo 'reading $(SMOKE_ENV)' >&2; \
		case '$(SMOKE_ENV)' in */*) . '$(SMOKE_ENV)';; *) . './$(SMOKE_ENV)';; esac; \
	fi; \
	set +a;

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
	@echo '  make release-check        Validate the release configuration.'
	@echo '  make release-snapshot     Build every release artifact into dist/, publishing nothing.'
	@echo ''
	@echo 'Starting a new product module:'
	@echo '  make new-module NAMESPACE=<namespace>   Create modules/<namespace>, ready to build.'
	@echo ''
	@echo 'Against a real deployment (Asgardeo, Identity Server 7.x, or ThunderID):'
	@echo '  make smoke-login          Log in and broker one acquisition. Opens a browser.'
	@echo '  make smoke-login-device   The same, approved on another device. Opens no browser.'
	@echo '  make smoke-ci             Broker one acquisition the way CI does. No browser.'
	@echo '  make empirical-asgardeo   Run the two one-time experiments and print their verdicts.'
	@echo '  make empirical-thunder    The same questions against a Thunder deployment.'
	@echo ''
	@echo 'Every live target skips cleanly when no deployment is configured.'
	@echo 'They read $(SMOKE_ENV) when it exists; name another with'
	@echo 'SMOKE_ENV=<path>. Copy test/smoke/env.example to start one.'
	@echo 'See test/smoke/RUNNING.md.'

# Creates a new product module, ready to build and test with nothing edited.
#
# The namespace is the first word of every command the module will answer, so it
# is refused when it is already taken, when a shell command owns it, when it is
# the reference module's reserved namespace, or when it is not something a user
# could type. See docs/guides/building-product-modules.md.
.PHONY: new-module
new-module:
ifndef NAMESPACE
	$(error NAMESPACE is required: make new-module NAMESPACE=mycloud)
endif
	$(GO) run ./cmd/wso2-module-new -namespace '$(NAMESPACE)'

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

# Builds every release artifact into dist/ and publishes nothing. This is how a
# contributor checks a change to .goreleaser.yaml, and how the artifact names and
# checksums can be inspected without pushing a tag. A snapshot names its archives
# for the most recent tag in the checkout and reports a -snapshot version from the
# binary inside; see docs/reference/release-artifacts.md.
.PHONY: release-snapshot
release-snapshot:
	$(GORELEASER) release --snapshot --clean

# Checks .goreleaser.yaml without building anything.
.PHONY: release-check
release-check:
	$(GORELEASER) check

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
	@$(smoke_env) $(GO) test $(SMOKE_FLAGS) $(SMOKE_PACKAGE) -run TestLoginSmoke

# The same deployment, logged in to without a browser. It reads exactly the
# variables smoke-login reads: nothing in the registration is specific to the
# device grant beyond enabling it on the application, and this target exists
# partly to keep that claim honest. Skips when no deployment is configured.
.PHONY: smoke-login-device
smoke-login-device:
	@$(smoke_env) $(GO) test $(SMOKE_FLAGS) $(SMOKE_PACKAGE) -run TestDeviceLoginSmoke

# Ends a real session and measures what that achieved at the issuer: whether the
# deployment advertises a revocation endpoint, whether it lets this public client
# use it, and whether the refresh token stops renewing afterwards. Prints one
# verdict line per question for recording. Skips when no deployment is
# configured. See docs/adr/0010-best-effort-revocation-on-session-end.md, which
# chose a design that survives not knowing these answers and asked for them to
# be measured.
.PHONY: smoke-logout
smoke-logout:
	@$(smoke_env) $(GO) test $(SMOKE_FLAGS) $(SMOKE_PACKAGE) -run TestLogoutSmoke

# Answers the two questions the redirect-and-narrowing research left open, and
# prints one verdict line each for recording in that document. Skips when no
# deployment is configured.
.PHONY: empirical-asgardeo
empirical-asgardeo:
	@$(smoke_env) WSO2_EMPIRICAL=1 \
		$(GO) test $(SMOKE_FLAGS) $(SMOKE_PACKAGE) -run TestAsgardeoEmpirical

# Answers the questions that decided how the shell derives access on a
# deployment which binds tokens to a named resource, and prints one verdict line
# each for recording. Skips unless the configured deployment says it is a
# Thunder one, because the experiments are meaningless against a product that
# takes no resource indicator.
.PHONY: empirical-thunder
empirical-thunder:
	@$(smoke_env) WSO2_EMPIRICAL=1 \
		$(GO) test $(SMOKE_FLAGS) $(SMOKE_PACKAGE) -run TestThunderEmpirical

# Brokers one acquisition the way a CI job does: inline, from a client secret
# already in this shell, with no login and no browser. Needs no human, so unlike
# smoke-login it can run unattended. The secret comes from the environment and
# from no file; RUNNING.md says which variable.
.PHONY: smoke-ci
smoke-ci:
	@$(smoke_env) $(GO) test $(SMOKE_FLAGS) $(SMOKE_PACKAGE) -run TestCISmoke
