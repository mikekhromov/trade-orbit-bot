#!/usr/bin/env bash
set -Eeuo pipefail

if [[ "${1:-}" == "--help" ]]; then
  echo "Usage: bash shell-tools/install-docker.sh"
  echo "Install Docker Engine, Buildx and Compose on Ubuntu/Debian with systemd."
  exit 0
fi
[[ $# -eq 0 ]] || { echo "Unexpected arguments. Use --help." >&2; exit 1; }
[[ "$(uname -s)" == Linux ]] || { echo "Run this script on the Linux server." >&2; exit 1; }

if [[ $(id -u) -ne 0 ]]; then
  exec sudo bash "$0" "$@"
fi

trap 'echo "Docker setup failed at line $LINENO. Fix the error above and rerun." >&2' ERR
command -v systemctl >/dev/null || { echo "This installer requires systemd." >&2; exit 1; }

if command -v docker >/dev/null && docker compose version >/dev/null 2>&1 && docker buildx version >/dev/null 2>&1; then
  echo "Docker, Compose and Buildx are already installed."
else
  [[ -r /etc/os-release ]] || { echo "Cannot detect the server OS." >&2; exit 1; }
  . /etc/os-release
  case "${ID:-}:${VERSION_ID:-}" in
    ubuntu:22.04|ubuntu:24.04|ubuntu:26.04|debian:12|debian:13) ;;
    *) echo "Automatic installation supports Ubuntu 22.04/24.04/26.04 and Debian 12/13. Detected: ${PRETTY_NAME:-unknown}." >&2; exit 1 ;;
  esac
  codename="${UBUNTU_CODENAME:-${VERSION_CODENAME:-}}"
  [[ -n "$codename" ]] || { echo "OS release codename is missing." >&2; exit 1; }

  conflicts=()
  for package in docker.io docker-compose docker-compose-v2 docker-doc docker-buildx podman-docker containerd runc; do
    if [[ "$(dpkg-query -W -f='${Status}' "$package" 2>/dev/null || true)" == "install ok installed" ]]; then
      conflicts+=("$package")
    fi
  done
  if [[ ${#conflicts[@]} -gt 0 ]]; then
    echo "Packages conflict with Docker's official packages: ${conflicts[*]}" >&2
    echo "Resolve the existing Docker/container runtime installation before rerunning." >&2
    exit 1
  fi

  echo "Installing Docker from the official apt repository for $ID ($codename)."
  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  apt-get install -y ca-certificates curl
  if ! grep -RqsE '^[[:space:]]*(deb .*|URIs:.*)https://download\.docker\.com/linux/' /etc/apt/sources.list /etc/apt/sources.list.d; then
    install -m 0755 -d /etc/apt/keyrings
    curl --fail --silent --show-error --location --retry 3 \
      "https://download.docker.com/linux/$ID/gpg" -o /etc/apt/keyrings/docker.asc
    chmod 0644 /etc/apt/keyrings/docker.asc
    cat > /etc/apt/sources.list.d/docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/$ID
Suites: $codename
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
EOF
  fi
  apt-get update
  apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
fi

systemctl enable --now docker
docker info >/dev/null
docker compose version
docker buildx version
echo "Docker is ready and enabled at boot."
