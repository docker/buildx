package docker

docker_github_builder(image, repo) if {
	image.hasProvenance
	some sig in image.signatures
	docker_github_builder_signature(sig, repo)
}

docker_github_builder_tag(image, repo, tag) if {
	docker_github_builder(image, repo)
	some sig in image.signatures
	sig.signer.sourceRepositoryRef == sprintf("refs/tags/%s", [tag])
}

docker_github_builder_signature(sig, repo) if {
	sig.kind == "docker-github-builder"
	sig.type == "bundle-v0.3"
	sig.signer.certificateIssuer == "CN=sigstore-intermediate,O=sigstore.dev"
	sig.signer.issuer == "https://token.actions.githubusercontent.com"
	sig.signer.sourceRepositoryURI == sprintf("https://github.com/%s", [repo])
	sig.signer.runnerEnvironment == "github-hosted"
	count(sig.timestamps) > 0
}

docker_github_builder_bundle(http, filename, repo) if {
	sig := artifact_attestation(http, filename)
	docker_github_builder_signature(sig, repo)
}

github_release_attestation(http) := sig if {
	http.schema == "https"
	http.host == "github.com"

	m := regex.find_all_string_submatch_n(`^/([^/]+)/([^/]+)/releases/download/[^/]+/.+$`, http.path, 1)[0]
	owner := m[1]
	repo := m[2]

	sig := github_attestation(http, sprintf("%s/%s", [owner, repo]))
}
