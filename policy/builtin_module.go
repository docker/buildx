package policy

import (
	_ "embed"

	"github.com/open-policy-agent/opa/v1/ast"
)

const builtinPolicyModuleFilename = "builtin/buildx_defaults.rego"

//go:embed builtins.rego
var builtinPolicyModule string

func builtinPolicyModuleAST() (*ast.Module, error) {
	return ast.ParseModuleWithOpts(builtinPolicyModuleFilename, builtinPolicyModule, ast.ParserOptions{
		RegoVersion: ast.RegoV1,
	})
}
