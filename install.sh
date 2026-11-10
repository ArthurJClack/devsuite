#!/bin/sh
# hq installer for mac/linux:
#   curl -fsSL https://raw.githubusercontent.com/ArthurJClack/devsuite/main/install.sh | sh
# downloads the latest hq and runs it; hq installs itself to ~/.local/share/hq, links ~/.local/bin/hq
# and keeps itself updated.
set -e
case "$(uname -s)" in
  Darwin) asset=hq-macos ;;
  *) asset=hq-linux ;;
esac
tmp="${TMPDIR:-/tmp}/hq-setup"
echo "getting hq (fish market studios)..."
curl -fsSL "https://github.com/ArthurJClack/devsuite/releases/latest/download/$asset" -o "$tmp"
chmod +x "$tmp"
command -v git >/dev/null || echo "heads up: hq needs git (https://git-scm.com/downloads)"
"$tmp" &
echo "done. next time just run: hq"
