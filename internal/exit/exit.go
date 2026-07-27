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

// Package exit owns the shell's exit classes.
//
// Exit codes are shell policy, not module policy: a module returns a typed
// problem and the shell alone decides the process status. Keeping the mapping
// here means automation sees one stable contract regardless of which module ran.
package exit

import "github.com/wso2/wso2-cli/sdk/problem"

// Code is a process exit status the shell may return.
type Code int

const (
	// OK reports a successful command.
	OK Code = 0
	// Usage reports invalid arguments, flags, or configuration.
	Usage Code = 64
	// AuthPolicy reports an authentication or broker policy failure.
	AuthPolicy Code = 77
	// ModuleTrust reports a module integrity or compatibility failure.
	ModuleTrust Code = 69
	// ModuleProcess reports a protocol or module process failure.
	ModuleProcess Code = 70
	// ProductService reports a failure reported by a product service.
	ProductService Code = 75
)

// ForProblem maps a typed problem to its exit class. An unrecognized category
// is treated as a module process failure rather than a success.
func ForProblem(p problem.Problem) Code {
	switch p.Category {
	case problem.CategoryUsage:
		return Usage
	case problem.CategoryAuthPolicy:
		return AuthPolicy
	case problem.CategoryModuleTrust:
		return ModuleTrust
	case problem.CategoryModuleProcess:
		return ModuleProcess
	case problem.CategoryProductService:
		return ProductService
	default:
		return ModuleProcess
	}
}
