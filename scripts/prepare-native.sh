#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
seed_dir="$repo_root/integration/testdata/v1-synthetic"
output_dir="$repo_root/build/native"
host_binary="$output_dir/cliproxyapi-v8.0.15"
test_binary="$output_dir/catalog-integration.test"
marker="$output_dir/prepared.txt"

mkdir -p "$output_dir"
rm -f "$marker"

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

sdk_module=github.com/router-for-me/CLIProxyAPI/v8
sdk_version=$(go list -mod=readonly -m -f '{{.Version}}' "$sdk_module")
if [ "$sdk_version" != "v8.0.15" ]; then
  printf 'Pinned host SDK is %s; expected v8.0.15\n' "$sdk_version" >&2
  exit 1
fi
sdk_dir=$(go list -mod=readonly -m -f '{{.Dir}}' "$sdk_module")
if [ -z "$sdk_dir" ] || [ ! -d "$sdk_dir/cmd/server" ]; then
  printf 'Could not locate the pinned CLIProxyAPI server source: %s\n' "$sdk_dir" >&2
  exit 1
fi

CGO_ENABLED=1 go -C "$sdk_dir" build -mod=readonly -trimpath -buildvcs=false -o "$host_binary" ./cmd/server
(cd "$repo_root" && go test -mod=readonly -c -o "$test_binary" ./integration)
chmod 755 "$host_binary" "$test_binary"
printf 'sdk=v8.0.15\nfixtures=v1-synthetic\n' > "$marker"
