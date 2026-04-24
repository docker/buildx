package docker

# Default policy embedded in Buildx. It verifies trust for images shipped
# by Docker that may be implicitly loaded during a build:
#
#   - docker/dockerfile
#   - docker/dockerfile-upstream
#   - docker/buildkit-syft-scanner
#
# Any image outside this managed set is allowed and passes through to user
# policies unchanged. Access by digest is always allowed. For tag-based
# access the rules below enforce a signed release from the expected GitHub
# source repository using the existing docker_github_builder_signature
# helper from builtins.rego.

is_dockerfile if {
	input.image
	input.image.fullRepo == "docker.io/docker/dockerfile"
}

is_dockerfile if {
	input.image
	input.image.fullRepo == "docker.io/docker/dockerfile-upstream"
}

is_syft_scanner if {
	input.image
	input.image.fullRepo == "docker.io/docker/buildkit-syft-scanner"
}

dockerfile_floating_tag(tag) if tag == "latest"
dockerfile_floating_tag(tag) if tag == "labs"
dockerfile_floating_tag(tag) if tag == "master"

dockerfile_tag_requires_sig(tag) if dockerfile_floating_tag(tag)
dockerfile_tag_requires_sig(tag) if version_tag_ge(tag, 1, 21)

syft_scanner_floating_tag(tag) if tag == "latest"

syft_scanner_tag_requires_sig(tag) if syft_scanner_floating_tag(tag)
syft_scanner_tag_requires_sig(tag) if version_tag_ge(tag, 1, 10)


default_policy_deny_msgs contains msg if {
	is_dockerfile
	tag := input.image.tag
	tag != ""
	dockerfile_tag_requires_sig(tag)
	not dockerfile_sig_ok(tag)
	msg := sprintf("image %s is not allowed by default policy: a verified docker-github-builder signature is required for %s tag", [input.image.ref, input.image.tag])
}

default_policy_deny_msgs contains msg if {
	is_syft_scanner
	tag := input.image.tag
	tag != ""
	syft_scanner_tag_requires_sig(tag)
	not syft_scanner_sig_ok(tag)
	msg := sprintf("image %s is not allowed by default policy: a verified docker-github-builder signature is required for %s tag", [input.image.ref, input.image.tag])
}

dockerfile_sig_ok(tag) if {
	dockerfile_floating_tag(tag)
	some sig in input.image.signatures
	docker_github_builder_signature(sig, "moby/buildkit")
}

dockerfile_sig_ok(tag) if {
	not dockerfile_floating_tag(tag)
	some sig in input.image.signatures
	docker_github_builder_signature(sig, "moby/buildkit")
	dockerfile_sig_ref_matches(sig, tag)
}

syft_scanner_sig_ok(tag) if {
	syft_scanner_floating_tag(tag)
	some sig in input.image.signatures
	docker_github_builder_signature(sig, "docker/buildkit-syft-scanner")
}

syft_scanner_sig_ok(tag) if {
	not syft_scanner_floating_tag(tag)
	some sig in input.image.signatures
	docker_github_builder_signature(sig, "docker/buildkit-syft-scanner")
	syft_scanner_sig_ref_matches(sig, tag)
}


decision := {
	"allow": count(default_policy_deny_msgs) == 0,
	"deny_msg": [msg | some msg in default_policy_deny_msgs],
}

# ---- helpers ----

# parse_version returns [major, minor] when tag matches a version pattern
# like "1", "1.21", "1.21.0", "1.21.0-labs". For a major-only tag such as
# "1", the minor component is treated as effectively unbounded so floating
# major tags are handled like the newest release in that major line.
parse_version(tag) := [maj, min] if {
	m := regex.find_all_string_submatch_n(`^(\d+)\.(\d+)(?:\.\d+)?(?:-labs)?$`, tag, 1)
	count(m) == 1
	maj := to_number(m[0][1])
	min := to_number(m[0][2])
}

parse_version(tag) := [maj, 999999] if {
	m := regex.find_all_string_submatch_n(`^(\d+)(?:-labs)?$`, tag, 1)
	count(m) == 1
	maj := to_number(m[0][1])
}

version_tag_ge(tag, target_major, _) if {
	v := parse_version(tag)
	v[0] > target_major
}

version_tag_ge(tag, target_major, target_minor) if {
	v := parse_version(tag)
	v[0] == target_major
	v[1] >= target_minor
}

dockerfile_sig_ref_matches(sig, tag) if {
	sig_ref_matches(sig.signer.sourceRepositoryRef, tag, "refs/tags/dockerfile/")
}

syft_scanner_sig_ref_matches(sig, tag) if {
	ref := trim_prefix(sig.signer.sourceRepositoryRef, "refs/tags/")
	ref != sig.signer.sourceRepositoryRef
	version_tag_selector_matches(tag, ref)
}

sig_ref_matches(ref, tag, prefix) if {
	stripped_ref := trim_prefix(ref, prefix)
	stripped_ref != ref
	tag_labs := endswith(tag, "-labs")
	ref_labs := endswith(stripped_ref, "-labs")
	tag_labs == ref_labs
	version_tag_selector_matches(
		trim_suffix(tag, "-labs"),
		trim_suffix(stripped_ref, "-labs"),
	)
}

version_tag_selector_matches(selector, candidate) if {
	selector == candidate
}

version_tag_selector_matches(selector, candidate) if {
	m := regex.find_all_string_submatch_n(`^(\d+)\.(\d+)$`, selector, 1)
	count(m) == 1
	parse_version(selector) == parse_version(candidate)
}

version_tag_selector_matches(selector, candidate) if {
	m := regex.find_all_string_submatch_n(`^(\d+)$`, selector, 1)
	count(m) == 1
	sel := parse_version(selector)
	cand := parse_version(candidate)
	sel[0] == cand[0]
}
