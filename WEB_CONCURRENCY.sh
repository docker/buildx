#!/usr/bin/env bash

# This script was created by the Python buildpack to automatically set the `WEB_CONCURRENCY`
# environment variable at dyno boot (if it's not already set), based on the available memory
# and number of CPU cores. The env var is then used by some Python web servers (such as
# gunicorn and uvicorn) to control the default number of server processes that they launch.
#
# The default `WEB_CONCURRENCY` value is calculated as the lowest of either:
# - `<number of dyno CPU cores> * 2 + 1`
# - `<dyno available RAM in MB> / 256` (to ensure each process has at least 256 MB RAM)
#
# Currently, on Heroku dynos this results in the following concurrency values:
# - Eco / Basic / Standard-1X: 2 (capped by the 512 MB available memory)
# - Standard-2X / Private-S / Shield-S: 4 (capped by the 1 GB available memory)
# - Performance-M / Private-M / Shield-M: 5 (based on the 2 CPU cores)
# - Performance-L / Private-L / Shield-L: 17 (based on the 8 CPU cores)
# - Performance-L-RAM / Private-L-RAM / Shield-L-RAM: 9 (based on the 4 CPU cores)
# - Performance-XL / Private-XL / Shield-XL: 17 (based on the 8 CPU cores)
# - Performance-2XL / Private-2XL / Shield-2XL: 33 (based on the 16 CPU cores)
#
# To override these default values, either set `WEB_CONCURRENCY` as an explicit config var
# on the app, or pass `--workers <num>` when invoking gunicorn/uvicorn in your Procfile.

# Note: Since this is a .profile.d/ script it will be sourced, meaning that we cannot enable
# exit on error, have to use return not exit, and returning non-zero doesn't have an effect.

function detect_memory_limit_in_mb() {
	local memory_limit_file='/sys/fs/cgroup/memory/memory.limit_in_bytes'

	# This memory limits file only exists on Heroku, or when using cgroups v1 (Docker < 20.10).
	if [[ -f "${memory_limit_file}" ]]; then
		local memory_limit_in_mb=$(($(cat "${memory_limit_file}") / 1048576))

		# Ignore values above 1TB RAM, since when using cgroups v1 the limits file reports a
		# bogus value of thousands of TB RAM when there is no container memory limit set.
		if ((memory_limit_in_mb <= 1048576)); then
			echo "${memory_limit_in_mb}"
			return 0
		fi
	fi

	return 1
}

function output() {
	# Only display log output for web dynos, to prevent breaking one-off dyno scripting use-cases,
	# and to prevent confusion from messages about WEB_CONCURRENCY in the logs of non-web workers.
	# (We still actually set the env vars for all dyno types for consistency and easier debugging.)
	if [[ "${DYNO:-}" == web.* ]]; then
		echo "Python buildpack: $*" >&2
	fi
}

if ! available_memory_in_mb=$(detect_memory_limit_in_mb); then
	# This should never occur on Heroku, but will be common for non-Heroku environments such as Dokku.
	output "Couldn't determine available memory. Skipping automatic configuration of WEB_CONCURRENCY."
	return 0
fi

if ! cpu_cores=$(nproc); then
	# This should never occur in practice, since this buildpack only supports being run on our base
	# images, and nproc is installed in all of them.
	output "Couldn't determine number of CPU cores. Skipping automatic configuration of WEB_CONCURRENCY."
	return 0
fi

output "Detected ${available_memory_in_mb} MB available memory and ${cpu_cores} CPU cores."

# This env var is undocumented and not consistent with what other buildpacks set, however,
# GitHub code search shows there are Python apps in the wild that do rely upon it.
export DYNO_RAM="${available_memory_in_mb}"

if [[ -v WEB_CONCURRENCY ]]; then
	output "Skipping automatic configuration of WEB_CONCURRENCY since it's already set."
	return 0
fi

minimum_memory_per_process_in_mb=256

# Prevents WEB_CONCURRENCY being set to zero if the environment is extremely memory constrained.
if ((available_memory_in_mb < minimum_memory_per_process_in_mb)); then
	max_concurrency_for_available_memory=1
else
	max_concurrency_for_available_memory=$((available_memory_in_mb / minimum_memory_per_process_in_mb))
fi

max_concurrency_for_cpu_cores=$((cpu_cores * 2 + 1))

if ((max_concurrency_for_available_memory < max_concurrency_for_cpu_cores)); then
	export WEB_CONCURRENCY="${max_concurrency_for_available_memory}"
	output "Defaulting WEB_CONCURRENCY to ${WEB_CONCURRENCY} based on the available memory."
else
	export WEB_CONCURRENCY="${max_concurrency_for_cpu_cores}"
	output "Defaulting WEB_CONCURRENCY to ${WEB_CONCURRENCY} based on the number of CPU cores."
fi
