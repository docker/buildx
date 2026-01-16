package policy

import "github.com/open-policy-agent/opa/v1/ast"

const builtinPolicyModuleFilename = "builtin/buildx_defaults.rego"

const builtinPolicyModule = `package docker

docker_github_builder(image, repo) if {
	image.hasProvenance
	some sig in image.signatures
	valid_docker_github_builder_signature(sig, repo)
}

valid_docker_github_builder_signature(sig, repo) if {
	sig.kind == "docker-github-builder"
	sig.type == "bundle-v0.3"
	sig.signer.certificateIssuer == "CN=sigstore-intermediate,O=sigstore.dev"
	sig.signer.issuer == "https://token.actions.githubusercontent.com"
	sig.signer.sourceRepositoryURI == sprintf("https://github.com/%s", [repo])
	sig.signer.runnerEnvironment == "github-hosted"
	count(sig.timestamps) > 0
}
`

func builtinPolicyModuleAST() (*ast.Module, error) {
	return ast.ParseModuleWithOpts(builtinPolicyModuleFilename, builtinPolicyModule, ast.ParserOptions{
		RegoVersion: ast.RegoV1,
	})
}
