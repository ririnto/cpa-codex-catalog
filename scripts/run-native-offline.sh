#!/bin/sh
set -eu

script_path=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/$(basename -- "$0")
script_dir=$(dirname -- "$script_path")
repo_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
output_dir="$repo_root/build/native"
host_binary="$output_dir/cliproxyapi-v8.0.20"
test_binary="$output_dir/catalog-integration.test"
marker="$output_dir/prepared.txt"
seed_dir="$repo_root/integration/testdata/v1-synthetic"
safe_path=/usr/sbin:/usr/bin:/sbin:/bin

if [ ! -f "$marker" ] || ! grep -Fxq 'sdk=v8.0.20' "$marker" || ! grep -Fxq 'fixtures=v1-synthetic' "$marker"; then
  printf 'Native artifacts are not prepared; run `go tool task prepare-native` first.\n' >&2
  exit 1
fi
if [ ! -x "$host_binary" ] || [ ! -x "$test_binary" ]; then
  printf 'Prepared native host or integration binary is missing; run `go tool task prepare-native` first.\n' >&2
  exit 1
fi
for seed in \
  generated-codex-catalog.json \
  plugin-config.json \
  patched-codex-catalog.expected.json \
  model-list-requests.json \
  model-list-responses.expected.json
do
  if [ ! -s "$seed_dir/$seed" ]; then
    printf 'Missing synthetic integration seed: %s\n' "$seed_dir/$seed" >&2
    exit 1
  fi
done

system=$(uname -s)
case "$system" in
  Linux)
    if [ "${CPA_NATIVE_NETNS:-}" = ready ]; then
      if ! command -v ip >/dev/null 2>&1; then
        printf 'The Linux offline lane requires iproute2 inside its network namespace.\n' >&2
        exit 1
      fi
      interfaces=$(ip -o link show | awk -F': ' '{name=$2; sub(/@.*/, "", name); print name}')
      if [ "$interfaces" != lo ]; then
        printf 'Expected only the loopback interface in the offline namespace; found: %s\n' "$interfaces" >&2
        exit 1
      fi
      ip link set lo up
      cd "$repo_root/integration"
      if [ -n "${CPA_NATIVE_USER:-}" ]; then
        runuser_path=$(command -v runuser) || {
          printf 'The sudo-created offline namespace requires util-linux runuser for user-owned test artifacts.\n' >&2
          exit 1
        }
        exec "$runuser_path" --user "$CPA_NATIVE_USER" --group "$CPA_NATIVE_GROUP" -- /usr/bin/env -i \
          PATH="$safe_path" \
          HOME="$TMPDIR/home" \
          TMPDIR="$TMPDIR" \
          GOPROXY=off \
          GOSUMDB=off \
          GOTOOLCHAIN=local \
          "$test_binary" -test.count=1 -test.v
      fi
      exec /usr/bin/env -i \
        PATH="$safe_path" \
        HOME="${TMPDIR:?}/home" \
        TMPDIR="$TMPDIR" \
        GOPROXY=off \
        GOSUMDB=off \
        GOTOOLCHAIN=local \
        "$test_binary" -test.count=1 -test.v
    fi
    if ! command -v unshare >/dev/null 2>&1 || ! command -v ip >/dev/null 2>&1; then
      printf 'The Linux offline lane requires util-linux unshare and iproute2.\n' >&2
      exit 1
    fi
    scratch=$(mktemp -d)
    mkdir -m 700 "$scratch/home"
    trap 'rm -rf "$scratch"' EXIT HUP INT TERM
    unshare_path=$(command -v unshare)
    if [ "$(id -u)" -eq 0 ]; then
      "$unshare_path" --net /usr/bin/env -i \
        PATH="$safe_path" \
        TMPDIR="$scratch" \
        CPA_NATIVE_NETNS=ready \
        /bin/sh "$script_path"
      exit $?
    fi
    if ! command -v sudo >/dev/null 2>&1; then
      printf 'Creating an isolated Linux network namespace requires root or sudo.\n' >&2
      exit 1
    fi
    sudo -- "$unshare_path" --net /usr/bin/env -i \
      PATH="$safe_path" \
      TMPDIR="$scratch" \
      CPA_NATIVE_NETNS=ready \
      CPA_NATIVE_USER="$(id -un)" \
      CPA_NATIVE_GROUP="$(id -gn)" \
      /bin/sh "$script_path"
    exit $?
    ;;
  Darwin)
    sandbox_exec=/usr/bin/sandbox-exec
    if [ ! -x "$sandbox_exec" ]; then
      printf 'The Darwin offline lane requires /usr/bin/sandbox-exec.\n' >&2
      exit 1
    fi
    scratch=$(mktemp -d)
    mkdir -m 700 "$scratch/home"
    trap 'rm -rf "$scratch"' EXIT HUP INT TERM
    cd "$repo_root/integration"
    /usr/bin/env -i \
      PATH="$safe_path" \
      HOME="$scratch/home" \
      TMPDIR="$scratch" \
      GOPROXY=off \
      GOSUMDB=off \
      GOTOOLCHAIN=local \
      "$sandbox_exec" -p '(version 1) (allow default) (deny network*) (allow network-bind (local ip "localhost:*")) (allow network-outbound (remote ip "localhost:*")) (allow network-inbound (local ip "localhost:*"))' \
      "$test_binary" -test.count=1 -test.v
    exit $?
    ;;
  *)
    printf 'Native offline integration is supported on Linux and macOS, not %s.\n' "$system" >&2
    exit 1
    ;;
esac
