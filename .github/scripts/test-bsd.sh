#!/usr/bin/env bash

set -euo pipefail

# Runs BSD unit tests for the test-bsd-unit workflow job.
# Arguments: <os> <version> [go test arguments...]
# The workflow supplies GO_VERSION and installs the host dependencies.
if [[ "${GITHUB_ACTIONS:-}" != true ]]; then
  echo "This script is intended to run only in GitHub Actions (GITHUB_ACTIONS=true)." >&2
  exit 1
fi

log() {
  printf '[test-bsd +%ss] %s\n' "$SECONDS" "$*"
}

os="${1:?missing BSD OS}"
version="${2:?missing BSD version}"
shift 2
if [[ "$#" == 0 ]]; then
  set -- ./...
fi
: "${GO_VERSION:?missing Go version}"

case "$os/$version" in
  freebsd/14.5) imageRelease=v0.17.0; imageFormat=qcow2 ;;
  netbsd/10.1) imageRelease=v1.1.0; imageFormat=tar.zst ;;
  openbsd/7.9) imageRelease=v0.14.0; imageFormat=qcow2 ;;
  *) echo "Unsupported BSD image: $os/$version" >&2; exit 1 ;;
esac

if [[ ! -r /dev/kvm || ! -w /dev/kvm ]]; then
  echo "KVM read/write access is required; grant the runner access to /dev/kvm." >&2
  exit 1
fi

sourceDir="$(cd "$(dirname "$0")/../.." && pwd)"
workDir="$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/buildx-bsd-$os.XXXXXX")"
qemuPid=""
log "Preparing $os $version with Go $GO_VERSION; temporary files: $workDir"

# Called indirectly by the EXIT trap.
# shellcheck disable=SC2317
cleanup() {
  status=$?
  trap - EXIT
  log "Cleaning up (exit status: $status)"
  if [[ -n "$qemuPid" ]]; then
    kill "$qemuPid" 2>/dev/null || true
    wait "$qemuPid" 2>/dev/null || true
  fi
  if [[ "$status" != 0 && -f "$workDir/console.log" ]]; then
    echo "::group::BSD VM console"
    cat "$workDir/console.log"
    echo "::endgroup::"
  fi
  rm -rf "$workDir"
  exit "$status"
}
trap 'cleanup' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

image="$os-$version-x86-64.$imageFormat"
log "Downloading $image from $os-builder release $imageRelease"
curl --fail --location --silent --show-error --retry 3 "https://github.com/cross-platform-actions/$os-builder/releases/download/$imageRelease/$image" -o "$workDir/$image"
log "Preparing VM disk"
if [[ "$imageFormat" == tar.zst ]]; then
  tar --zstd -xf "$workDir/$image" -C "$workDir" disk.img
else
  qemu-img convert -f qcow2 -O raw "$workDir/$image" "$workDir/disk.img"
fi

# Keep SSH configuration, host keys and identities private to this invocation.
# FreeBSD and NetBSD images permit an empty-password login for runner.
log "Preparing SSH access and guest boot configuration"
ssh-keygen -q -t ed25519 -N "" -f "$workDir/id_ed25519"
cat > "$workDir/ssh_config" <<EOF
Host bsd
  HostName 127.0.0.1
  Port 10022
  User runner
  IdentityFile "$workDir/id_ed25519"
  IdentitiesOnly yes
  IdentityAgent none
  BatchMode yes
  StrictHostKeyChecking accept-new
  UserKnownHostsFile "$workDir/known_hosts"
  GlobalKnownHostsFile /dev/null
  ConnectTimeout 5
  ServerAliveInterval 15
  ServerAliveCountMax 4
EOF
sshCmd=(ssh -F "$workDir/ssh_config")
diskArgs=(-device virtio-scsi-pci -device "scsi-hd,drive=os,bootindex=0")
firmwareArgs=()
networkDevice=virtio-net-pci

case "$os" in
  freebsd)
    diskArgs=(-device "virtio-blk-pci,drive=os,bootindex=0")
    ;;
  openbsd)
    # The x86-64 image configures networking on em0, not vio0.
    networkDevice=e1000
    # OpenBSD imports KEYS from an unpartitioned FAT disk at first boot.
    # mtools writes it without mounting a filesystem on the host.
    mkfs.fat -F 32 -C "$workDir/keys.img" 65536
    mcopy -i "$workDir/keys.img" "$workDir/id_ed25519.pub" ::KEYS
    diskArgs+=(-device "scsi-hd,drive=keys" -drive "if=none,id=keys,format=raw,file=$workDir/keys.img")
    cp /usr/share/OVMF/OVMF_VARS_4M.fd "$workDir/OVMF_VARS.fd"
    firmwareArgs=(-drive "if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd" -drive "if=pflash,format=raw,file=$workDir/OVMF_VARS.fd")
    ;;
esac

# Mask CPU features unsupported by the BSD guests, including OpenBSD's 1GB pages.
# User-mode networking needs no tap device or root QEMU process.
log "Starting QEMU with KVM, 2 CPUs and 4096 MiB RAM"
qemu-system-x86_64 \
  -machine q35 -accel kvm \
  -cpu host,amx-tile=off,amx-int8=off,amx-bf16=off,pdpe1gb=off \
  -smp 2 -m 4096 \
  -display none -monitor none -serial "file:$workDir/console.log" \
  -device "$networkDevice,netdev=net0" \
  -netdev user,id=net0,ipv6=off,hostfwd=tcp:127.0.0.1:10022-:22 \
  -drive "if=none,id=os,format=raw,file=$workDir/disk.img" \
  "${diskArgs[@]}" "${firmwareArgs[@]}" &
qemuPid=$!

log "Waiting up to 240 seconds for SSH (QEMU PID: $qemuPid)"
deadline=$((SECONDS + 240))
nextLog=$((SECONDS + 15))
until "${sshCmd[@]}" bsd true 2>"$workDir/ssh.log"; do
  if ! kill -0 "$qemuPid" 2>/dev/null; then
    echo "QEMU exited before SSH became ready." >&2
    cat "$workDir/ssh.log" >&2
    exit 1
  fi
  if (( SECONDS >= deadline )); then
    echo "Timed out waiting for BSD VM SSH." >&2
    cat "$workDir/ssh.log" >&2
    exit 1
  fi
  if (( SECONDS >= nextLog )); then
    log "Still waiting for SSH ($((deadline - SECONDS)) seconds remaining)"
    nextLog=$((SECONDS + 15))
  fi
  sleep 2
done
log "SSH is ready"
"${sshCmd[@]}" bsd 'uname -a; id'

log "Transferring tracked source files to the VM"
"${sshCmd[@]}" bsd 'mkdir -p /home/runner/buildx'
git -C "$sourceDir" ls-files -z | rsync --from0 --files-from=- --stats -rlpt -e "ssh -F \"$workDir/ssh_config\"" "$sourceDir/" bsd:/home/runner/buildx/

archive="go$GO_VERSION.$os-amd64.tar.gz"
log "Downloading and transferring $archive"
curl --fail --location --silent --show-error --retry 3 "https://go.dev/dl/$archive" -o "$workDir/go.tar.gz"
"${sshCmd[@]}" bsd 'cat > /var/tmp/buildx-go.tar.gz' < "$workDir/go.tar.gz"

# Only the OS and explicit test arguments cross the VM boundary, not the job env.
{
  printf 'os=%q\n' "$os"
  cat <<'EOF'
set -ex
export PATH="/var/tmp/go/bin:$PATH"
case "$os" in
  freebsd) sudo pkg install -y git ;;
  netbsd) sudo pkgin -y install git ;;
  openbsd) sudo pkg_add git ;;
esac
tar -C /var/tmp -xzf /var/tmp/buildx-go.tar.gz
go version
cd /home/runner/buildx
export TMPDIR=/var/tmp
export SKIP_INTEGRATION_TESTS=1
EOF
  printf 'go test -mod=vendor -buildvcs=false -coverprofile=coverage.txt -covermode=atomic'
  printf ' %q' "$@"
  printf '\n'
} > "$workDir/test.sh"

log "Installing guest dependencies and running unit tests"
testStatus=0
"${sshCmd[@]}" bsd 'env PATH=/usr/local/sbin:/usr/local/bin:/usr/pkg/sbin:/usr/pkg/bin:/usr/sbin:/usr/bin:/sbin:/bin bash -s' < "$workDir/test.sh" || testStatus=$?

log "Unit test command exited with status $testStatus"
log "Collecting coverage.txt"
# Copy only the coverage file. Never sync guest files over the runner workspace.
coverageStatus=0
"${sshCmd[@]}" bsd 'cat /home/runner/buildx/coverage.txt' > "$workDir/coverage.txt" || coverageStatus=$?
if [[ "$coverageStatus" == 0 ]]; then
  mv "$workDir/coverage.txt" "$sourceDir/coverage.txt"
  log "Coverage saved to $sourceDir/coverage.txt"
else
  log "Coverage collection failed with status $coverageStatus"
fi
if [[ "$testStatus" != 0 ]]; then
  exit "$testStatus"
fi
exit "$coverageStatus"
