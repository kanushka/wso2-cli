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
