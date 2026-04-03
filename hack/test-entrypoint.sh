#!/bin/sh

set -eu

# Nested containers on cgroup v2 need controllers delegated from the root cgroup.
if [ -f /sys/fs/cgroup/cgroup.controllers ]; then
	mkdir -p /sys/fs/cgroup/init
	xargs -rn1 < /sys/fs/cgroup/cgroup.procs > /sys/fs/cgroup/init/cgroup.procs || :
	sed -e 's/ / +/g' -e 's/^/+/' < /sys/fs/cgroup/cgroup.controllers \
		> /sys/fs/cgroup/cgroup.subtree_control
fi

exec "$@"
