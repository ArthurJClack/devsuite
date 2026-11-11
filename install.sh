#!/bin/sh
# hq installer for mac/linux:
#   curl -fsSL https://raw.githubusercontent.com/ArthurJClack/devsuite/main/install.sh | sh
set -e
case "$(uname -s)" in Darwin) asset=hq-macos ;; *) asset=hq-linux ;; esac
dir="$HOME/.local/share/hq"; mkdir -p "$dir" "$HOME/.local/bin"
echo "getting hq (fish market studios)..."
curl -fsSL "https://github.com/ArthurJClack/devsuite/releases/latest/download/$asset" -o "$dir/hq"
chmod +x "$dir/hq"; ln -sf "$dir/hq" "$HOME/.local/bin/hq"
command -v git >/dev/null || echo "heads up: hq needs git (https://git-scm.com/downloads)"
nohup "$dir/hq" >/dev/null 2>&1 &
echo "done. next time just run: hq"
