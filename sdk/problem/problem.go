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

// Package problem declares the typed failures a product module returns through
// the module contract.
//
// A module never writes user-facing output or selects an exit code. It returns
// a Problem, and the shell owns rendering and exit classification. See
// docs/adr/0003-shell-owned-output.md.
package problem

import "fmt"

// Category is the stable failure class a problem belongs to. The shell maps a
// category to an exit class, so categories are part of the module contract and
// must not be renamed without a protocol change.
type Category string

const (
	// CategoryUsage covers invalid arguments, flags, or configuration.
	CategoryUsage Category = "usage"
	// CategoryAuthPolicy covers authentication and broker policy failures.
	CategoryAuthPolicy Category = "auth_policy"
	// CategoryModuleTrust covers module integrity and compatibility failures.
	CategoryModuleTrust Category = "module_trust"
	// CategoryModuleProcess covers protocol and module process failures.
	CategoryModuleProcess Category = "module_process"
	// CategoryProductService covers failures reported by a product service.
	CategoryProductService Category = "product_service"
)

// Problem is a typed, safe-to-render failure. Its fields must never carry
// credential material: the shell renders Message and Recovery verbatim.
type Problem struct {
	// Category places the problem in a stable failure class.
	Category Category `json:"category"`
	// Code is a stable, machine-readable identifier such as
	// "reference.status_unavailable".
	Code string `json:"code"`
	// Message states what failed, without secret detail.
	Message string `json:"message"`
	// Recovery states what the user can do next. It may be empty when no
	// user action applies.
	Recovery string `json:"recovery,omitempty"`
}

// New builds a problem in the given category.
func New(category Category, code, message string) Problem {
	return Problem{Category: category, Code: code, Message: message}
}

// WithRecovery returns a copy of the problem carrying user recovery guidance.
func (p Problem) WithRecovery(recovery string) Problem {
	p.Recovery = recovery
	return p
}

// Error lets a problem travel as an ordinary Go error inside a module.
func (p Problem) Error() string {
	return fmt.Sprintf("%s: %s: %s", p.Category, p.Code, p.Message)
}
