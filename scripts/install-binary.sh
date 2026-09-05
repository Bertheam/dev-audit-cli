#!/bin/sh

set -eu

package_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
source_binary=$package_dir/dev-audit

if [ "$#" -gt 1 ]; then
	printf 'Usage: %s [INSTALL_DIRECTORY]\n' "$0" >&2
	exit 2
fi

if [ ! -f "$source_binary" ] || [ ! -x "$source_binary" ]; then
	printf 'The dev-audit binary is missing or is not executable in %s\n' "$package_dir" >&2
	exit 3
fi

if [ "$#" -eq 1 ]; then
	install_dir=$1
elif [ -n "${DEV_AUDIT_INSTALL_DIR:-}" ]; then
	install_dir=$DEV_AUDIT_INSTALL_DIR
elif [ -d /opt/homebrew/bin ] && [ -w /opt/homebrew/bin ]; then
	install_dir=/opt/homebrew/bin
elif [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
	install_dir=/usr/local/bin
else
	install_dir=$HOME/.local/bin
fi

mkdir -p -- "$install_dir"
install -m 0755 "$source_binary" "$install_dir/dev-audit"

printf 'Installed dev-audit in %s\n' "$install_dir"
case ":$PATH:" in
	*":$install_dir:"*) ;;
	*)
		printf 'Add this directory to PATH before using the command:\n'
		printf '  export PATH="%s:$PATH"\n' "$install_dir"
		;;
esac
