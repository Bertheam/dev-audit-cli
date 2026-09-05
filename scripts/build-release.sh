#!/bin/sh

set -eu

repository_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
	printf 'Usage: %s VERSION [OUTPUT_DIRECTORY]\n' "$0" >&2
	exit 2
fi

release_version=$1
case "$release_version" in
	""|*[!A-Za-z0-9._-]*)
		printf 'VERSION may contain only letters, digits, dots, underscores and hyphens\n' >&2
		exit 2
		;;
esac

if [ "$#" -eq 2 ]; then
	output_dir=$2
else
	output_dir=$repository_dir/dist
fi

mkdir -p -- "$output_dir"
output_dir=$(CDPATH= cd -- "$output_dir" && pwd -P)

staging_dir=$(mktemp -d "${TMPDIR:-/tmp}/dev-audit-release.XXXXXX")
cleanup() {
	rm -rf -- "$staging_dir"
}
trap cleanup EXIT HUP INT TERM

for release_arch in arm64 amd64; do
	package_name=dev-audit_${release_version}_darwin_${release_arch}
	package_dir=$staging_dir/$package_name
	archive_path=$output_dir/$package_name.tar.gz

	mkdir -p -- "$package_dir"
	cd -- "$repository_dir"
	CGO_ENABLED=0 GOOS=darwin GOARCH=$release_arch go build \
		-trimpath \
		-ldflags "-s -w -X main.version=$release_version" \
		-o "$package_dir/dev-audit" \
		./cmd/dev-audit
	cp -- scripts/install-binary.sh "$package_dir/install.sh"
	cp -- packaging/INSTALL.md "$package_dir/INSTALL.md"
	chmod 0755 "$package_dir/dev-audit" "$package_dir/install.sh"

	cd -- "$staging_dir"
	tar -czf "$archive_path" "$package_name"
done

cd -- "$output_dir"
checksum_file=SHA256SUMS
: > "$checksum_file"
for archive_path in dev-audit_${release_version}_darwin_*.tar.gz; do
	checksum_path=$archive_path.sha256
	shasum -a 256 "$archive_path" > "$checksum_path"
	cat "$checksum_path" >> "$checksum_file"
done

printf 'Release artifacts written to %s\n' "$output_dir"
printf 'Verify them with: cd %s && shasum -a 256 -c SHA256SUMS\n' "$output_dir"
