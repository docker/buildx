#!/usr/bin/env bash

set -euo pipefail
shopt -s inherit_errexit

PYTHON_VERSION="${1:?"Error: The Python version to build must be specified as the first argument."}"
PYTHON_MAJOR_VERSION="${PYTHON_VERSION%.*}"

ARCH=$(dpkg --print-architecture)

# Python is relocated to different locations by the classic buildpack and CNB (which works since we
# set `LD_LIBRARY_PATH` and `PYTHONHOME` appropriately at build/run-time), so for packaging purposes
# we install Python into an arbitrary location that intentionally matches neither location.
INSTALL_DIR="/tmp/python"
SRC_DIR="/tmp/src"
UPLOAD_DIR="/tmp/upload"

function abort() {
	echo "Error: ${1}" >&2
	exit 1
}

case "${STACK:?}" in
	heroku-20 | heroku-22 | heroku-24)
		SUPPORTED_PYTHON_VERSIONS=(
			"3.9"
			"3.10"
			"3.11"
			"3.12"
			"3.13"
		)
		;;
	*)
		abort "Unsupported stack '${STACK}'!"
		;;
esac

if [[ ! " ${SUPPORTED_PYTHON_VERSIONS[*]} " == *" ${PYTHON_MAJOR_VERSION} "* ]]; then
	abort "Python ${PYTHON_MAJOR_VERSION} is not supported on ${STACK}!"
fi

# The release keys can be found on https://www.python.org/downloads/ -> "OpenPGP Public Keys".
case "${PYTHON_MAJOR_VERSION}" in
	3.13)
		# https://github.com/Yhg1s.gpg
		GPG_KEY_FINGERPRINT='7169605F62C751356D054A26A821E680E5FA6305'
		;;
	3.12)
		# https://github.com/Yhg1s.gpg
		GPG_KEY_FINGERPRINT='7169605F62C751356D054A26A821E680E5FA6305'
		;;
	3.10 | 3.11)
		# https://keybase.io/pablogsal/
		GPG_KEY_FINGERPRINT='A035C8C19219BA821ECEA86B64E628F8D684696D'
		;;
	3.9)
		# https://keybase.io/ambv/
		GPG_KEY_FINGERPRINT='E3FF2839C048B25C084DEBE9B26995E310250568'
		;;
	*)
		abort "Unsupported Python version '${PYTHON_MAJOR_VERSION}'!"
		;;
esac

echo "Building Python ${PYTHON_VERSION} for ${STACK} (${ARCH})..."

SOURCE_URL="https://www.python.org/ftp/python/${PYTHON_VERSION}/Python-${PYTHON_VERSION}.tgz"
SIGNATURE_URL="${SOURCE_URL}.asc"

set -o xtrace

mkdir -p "${SRC_DIR}" "${INSTALL_DIR}" "${UPLOAD_DIR}"

curl --fail --retry 3 --retry-connrefused --connect-timeout 10 --max-time 60 -o python.tgz "${SOURCE_URL}"
curl --fail --retry 3 --retry-connrefused --connect-timeout 10 --max-time 60 -o python.tgz.asc "${SIGNATURE_URL}"

gpg --batch --verbose --recv-keys "${GPG_KEY_FINGERPRINT}"
gpg --batch --verify python.tgz.asc python.tgz

tar --extract --file python.tgz --strip-components=1 --directory "${SRC_DIR}"
cd "${SRC_DIR}"

# Work around PGO profile test failures with Python 3.13 on Ubuntu 22.04, due to the tests
# checking the raw libexpat version which doesn't account for Ubuntu backports:
# https://github.com/heroku/heroku-buildpack-python/pull/1661#issuecomment-2405259352
# https://github.com/python/cpython/issues/125067
if [[ "${PYTHON_MAJOR_VERSION}" == "3.13" && "${STACK}" == "heroku-22" ]]; then
	patch -p1 </tmp/python-3.13-ubuntu-22.04-libexpat-workaround.patch
fi

# Aim to keep this roughly consistent with the options used in the Python Docker images,
# for maximum compatibility / most battle-tested build configuration:
# https://github.com/docker-library/python
CONFIGURE_OPTS=(
	# Explicitly set the target architecture rather than auto-detecting based on the host CPU.
	# This only affects targets like i386 (for which we don't build), but we pass it anyway for
	# completeness and parity with the Python Docker image builds.
	"--build=$(dpkg-architecture --query DEB_BUILD_GNU_TYPE)"
	# Support loadable extensions in the `_sqlite` extension module.
	"--enable-loadable-sqlite-extensions"
	# Enable recommended release build performance optimisations such as PGO.
	"--enable-optimizations"
	# Make autoconf's configure option validation more strict.
	"--enable-option-checking=fatal"
	# Install Python into `/tmp/python` rather than the default of `/usr/local`.
	"--prefix=${INSTALL_DIR}"
	# Skip running `ensurepip` as part of install, since the buildpack installs a curated
	# version of pip itself (which ensures it's consistent across Python patch releases).
	"--with-ensurepip=no"
	# Build the `pyexpat` module using the `expat` library in the base image (which will
	# automatically receive security updates), rather than CPython's vendored version.
	"--with-system-expat"
)

if [[ "${PYTHON_MAJOR_VERSION}" != +(3.9) ]]; then
	CONFIGURE_OPTS+=(
		# Shared builds are beneficial for a number of reasons:
		# - Reduces the size of the build, since it avoids the duplication between
		#   the Python binary and the static library.
		# - Permits use-cases that only work with the shared Python library,
		#   and not the static library (such as `pycall.rb` or `PyO3`).
		# - More consistent with the official Python Docker images and other distributions.
		#
		# However, shared builds are slower unless `no-semantic-interposition`and LTO is used:
		# https://fedoraproject.org/wiki/Changes/PythonNoSemanticInterpositionSpeedup
		# https://github.com/python/cpython/issues/83161
		#
		# It's only as of Python 3.10 that `no-semantic-interposition` is enabled by default,
		# so we only use shared builds on Python 3.10+ to avoid needing to override the default
		# compiler flags.
		"--enable-shared"
		"--with-lto"
		# Counter-intuitively, the static library is still generated by default even when
		# the shared library is enabled, so we disable it to reduce the build size.
		# This option only exists for Python 3.10+.
		"--without-static-libpython"
	)
fi

if [[ "${PYTHON_MAJOR_VERSION}" != +(3.9|3.10) ]]; then
	CONFIGURE_OPTS+=(
		# Skip building the test modules, since we remove them after the build anyway.
		# This feature was added in Python 3.10+, however it wasn't until Python 3.11
		# that compatibility issues between it and PGO were fixed:
		# https://github.com/python/cpython/pull/29315
		"--disable-test-modules"
	)
fi

./configure "${CONFIGURE_OPTS[@]}"

# `-Wl,--strip-all` instructs the linker to omit all symbol information from the final binary
# and shared libraries, to reduce the size of the build. We have to use `--strip-all` and
# not `--strip-unneeded` since `ld` only understands the former (unlike the `strip` command),
# however, `--strip-all` is safe to use since LDFLAGS doesn't apply to static libraries.
# `dpkg-buildflags` returns the distro's default compiler/linker options, which enable various
# security/hardening best practices. See:
# - https://wiki.ubuntu.com/ToolChain/CompilerFlags
# - https://wiki.debian.org/Hardening
# - https://github.com/docker-library/python/issues/810
# We only use `dpkg-buildflags` for Python versions where we build in shared mode (Python 3.9+),
# since some of the options it enables interferes with the stripping of static libraries.
if [[ "${PYTHON_MAJOR_VERSION}" == +(3.9) ]]; then
	EXTRA_CFLAGS=''
	LDFLAGS='-Wl,--strip-all'
else
	EXTRA_CFLAGS="$(dpkg-buildflags --get CFLAGS)"
	LDFLAGS="$(dpkg-buildflags --get LDFLAGS) -Wl,--strip-all"
fi

CPU_COUNT="$(nproc)"
make -j "${CPU_COUNT}" "EXTRA_CFLAGS=${EXTRA_CFLAGS}" "LDFLAGS=${LDFLAGS}"
make install

if [[ "${PYTHON_MAJOR_VERSION}" == +(3.9) ]]; then
	# On older versions of Python we're still building the static library, which has to be
	# manually stripped since the linker stripping enabled in LDFLAGS doesn't cover them.
	# We're using `--strip-unneeded` since `--strip-all` would remove the `.symtab` section
	# that is required for static libraries to be able to be linked.
	# `find` is used since there are multiple copies of the static library in version-specific
	# locations, eg:
	#   - `lib/libpython3.9.a`
	#   - `lib/python3.9/config-3.9-x86_64-linux-gnu/libpython3.9.a`
	find "${INSTALL_DIR}" -type f -name '*.a' -print -exec strip --strip-unneeded '{}' +
elif ! find "${INSTALL_DIR}" -type f -name '*.a' -print -exec false '{}' +; then
	abort "Unexpected static libraries found!"
fi

# Remove unneeded test directories, similar to the official Docker Python images:
# https://github.com/docker-library/python
# This is a no-op on Python 3.11+, since --disable-test-modules will have prevented
# the test files from having been built in the first place.
find "${INSTALL_DIR}" -depth -type d -a \( -name 'test' -o -name 'tests' -o -name 'idle_test' \) -print -exec rm -rf '{}' +

# The `make install` step automatically generates `.pyc` files for the stdlib, however:
# - It generates these using the default `timestamp` invalidation mode, which does
#   not work well with the CNB file timestamp normalisation behaviour. As such, we
#   must use one of the hash-based invalidation modes to prevent the `.pyc`s from
#   always being treated as outdated and so being regenerated at application boot.
# - It generates `.pyc`s for all three optimisation levels (standard, -O and -OO),
#   when the vast majority of apps only use the standard mode. As such, we can skip
#   regenerating/shipping those `.opt-{1,2}.pyc` files, reducing build output by 18MB.
#
# We use the `unchecked-hash` mode rather than `checked-hash` since it improves app startup
# times by ~5%, and is only an issue if manual edits are made to the stdlib, which is not
# something we support.
#
# See:
# https://docs.python.org/3/reference/import.html#cached-bytecode-invalidation
# https://docs.python.org/3/library/compileall.html
# https://peps.python.org/pep-0488/
# https://peps.python.org/pep-0552/
find "${INSTALL_DIR}" -depth -type f -name "*.pyc" -delete
# We use the Python binary from the original build output in the source directory,
# rather than the installed binary in `$INSTALL_DIR`, for parity with the automatic
# `.pyc` generation run by `make install`:
# https://github.com/python/cpython/blob/v3.11.3/Makefile.pre.in#L2087-L2113
LD_LIBRARY_PATH="${SRC_DIR}" "${SRC_DIR}/python" -m compileall -f --invalidation-mode unchecked-hash --workers 0 "${INSTALL_DIR}"

# Delete entrypoint scripts (and their symlinks) that don't work with relocated Python since they
# hardcode the Python install directory in their shebangs (e.g. `#!/tmp/python/bin/python3.NN`).
# These scripts are rarely used in production, and can still be accessed via their Python module
# (e.g. `python -m pydoc`) if needed.
rm "${INSTALL_DIR}"/bin/{idle,pydoc}*
# The 2to3 module and entrypoint was removed from the stdlib in Python 3.13.
if [[ "${PYTHON_MAJOR_VERSION}" == +(3.9|3.10|3.11|3.12) ]]; then
	rm "${INSTALL_DIR}"/bin/2to3*
fi

# Support using Python 3 via the version-less `python` command, for parity with virtualenvs,
# the Python Docker images and to also ensure buildpack Python shadows any installed system
# Python, should that provide a version-less alias too.
# This symlink must be relative, to ensure that the Python install remains relocatable.
ln -srvT "${INSTALL_DIR}/bin/python3" "${INSTALL_DIR}/bin/python"

# Results in a compressed archive filename of form: 'python-X.Y.Z-ubuntu-22.04-amd64.tar.zst'
UBUNTU_VERSION=$(lsb_release --short --release 2>/dev/null)
TAR_FILEPATH="${UPLOAD_DIR}/python-${PYTHON_VERSION}-ubuntu-${UBUNTU_VERSION}-${ARCH}.tar"
tar --create --format=pax --sort=name --file "${TAR_FILEPATH}" --directory="${INSTALL_DIR}" .
zstd -T0 -22 --ultra --long --no-progress --rm "${TAR_FILEPATH}"

du --max-depth 1 --human-readable "${INSTALL_DIR}"
du --all --human-readable "${UPLOAD_DIR}"
