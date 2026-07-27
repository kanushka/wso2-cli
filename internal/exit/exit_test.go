package exit

import (
	"testing"

	"github.com/wso2/wso2-cli/sdk/problem"
)

func TestForProblemMapsEveryCategoryToADistinctNonZeroClass(t *testing.T) {
	categories := []problem.Category{
		problem.CategoryUsage,
		problem.CategoryAuthPolicy,
		problem.CategoryModuleTrust,
		problem.CategoryModuleProcess,
		problem.CategoryProductService,
	}

	seen := make(map[Code]problem.Category, len(categories))
	for _, category := range categories {
		code := ForProblem(problem.New(category, "code", "message"))
		if code == OK {
			t.Fatalf("category %s mapped to a success exit code", category)
		}
		if existing, duplicate := seen[code]; duplicate {
			t.Fatalf("categories %s and %s share exit code %d", existing, category, code)
		}
		seen[code] = category
	}
}

func TestForProblemFailsClosedForAnUnknownCategory(t *testing.T) {
	if code := ForProblem(problem.Problem{Category: "invented"}); code != ModuleProcess {
		t.Fatalf("unknown category mapped to %d, want the module process class %d", code, ModuleProcess)
	}
}
