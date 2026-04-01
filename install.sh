#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT_DIR"

SUDO=""
OS_ID=""
OS_LIKE=""
OS_PRETTY_NAME="Linux"
PKG_MANAGER=""
COMPOSE_COMMAND_LABEL=""

if [[ "${EUID}" -ne 0 ]]; then
  if command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
  else
    echo "Please run this script as root or install sudo first."
    exit 1
  fi
fi

log() {
  printf '\n[%s] %s\n' "$(date '+%H:%M:%S')" "$1"
}

command_exists() {
  command -v "$1" >/dev/null 2>&1
}

detect_linux_env() {
  if [[ ! -r /etc/os-release ]]; then
    echo "Unable to detect Linux distribution because /etc/os-release is missing."
    exit 1
  fi

  # shellcheck disable=SC1091
  . /etc/os-release
  OS_ID="${ID:-linux}"
  OS_LIKE="${ID_LIKE:-}"
  OS_PRETTY_NAME="${PRETTY_NAME:-${ID:-Linux}}"

  if command_exists apt-get; then
    PKG_MANAGER="apt"
  elif command_exists dnf; then
    PKG_MANAGER="dnf"
  elif command_exists yum; then
    PKG_MANAGER="yum"
  elif command_exists zypper; then
    PKG_MANAGER="zypper"
  elif command_exists pacman; then
    PKG_MANAGER="pacman"
  elif command_exists apk; then
    PKG_MANAGER="apk"
  else
    echo "Unsupported Linux distribution: no supported package manager found."
    echo "Supported package managers: apt, dnf, yum, zypper, pacman, apk"
    exit 1
  fi

  log "Detected ${OS_PRETTY_NAME} using package manager: ${PKG_MANAGER}"
}

update_upgrade_system() {
  case "$PKG_MANAGER" in
    apt)
      log "Updating apt package index"
      $SUDO apt-get update -y
      log "Upgrading installed packages"
      DEBIAN_FRONTEND=noninteractive $SUDO apt-get upgrade -y
      ;;
    dnf)
      log "Refreshing dnf metadata and upgrading packages"
      $SUDO dnf makecache
      $SUDO dnf upgrade -y
      ;;
    yum)
      log "Refreshing yum metadata and upgrading packages"
      $SUDO yum makecache -y
      $SUDO yum update -y
      ;;
    zypper)
      log "Refreshing zypper metadata and upgrading packages"
      $SUDO zypper --gpg-auto-import-keys --non-interactive refresh
      $SUDO zypper --non-interactive update
      ;;
    pacman)
      log "Refreshing pacman metadata and upgrading packages"
      $SUDO pacman -Syu --noconfirm
      ;;
    apk)
      log "Refreshing apk indexes and upgrading packages"
      $SUDO apk update
      $SUDO apk upgrade
      ;;
  esac
}

install_packages() {
  case "$PKG_MANAGER" in
    apt)
      DEBIAN_FRONTEND=noninteractive $SUDO apt-get install -y "$@"
      ;;
    dnf)
      $SUDO dnf install -y "$@"
      ;;
    yum)
      $SUDO yum install -y "$@"
      ;;
    zypper)
      $SUDO zypper --non-interactive install "$@"
      ;;
    pacman)
      $SUDO pacman -S --noconfirm --needed "$@"
      ;;
    apk)
      $SUDO apk add --no-cache "$@"
      ;;
  esac
}

try_install_package() {
  local package_name="$1"
  set +e
  install_packages "$package_name" >/dev/null 2>&1
  local status=$?
  set -e
  return "$status"
}

install_base_packages() {
  log "Installing required base packages"
  case "$PKG_MANAGER" in
    apt)
      install_packages \
        ca-certificates \
        curl \
        gnupg \
        lsb-release \
        apt-transport-https \
        software-properties-common
      ;;
    dnf|yum)
      install_packages \
        ca-certificates \
        curl \
        gnupg2 \
        shadow-utils
      ;;
    zypper)
      install_packages \
        ca-certificates \
        curl \
        gnupg
      ;;
    pacman)
      install_packages \
        ca-certificates \
        curl \
        gnupg
      ;;
    apk)
      install_packages \
        bash \
        ca-certificates \
        curl \
        gnupg \
        shadow
      ;;
  esac
}

docker_install_via_official_script() {
  log "Installing Docker Engine via the official Docker convenience script"
  local tmp_script
  tmp_script="$(mktemp)"
  curl -fsSL https://get.docker.com -o "$tmp_script"
  $SUDO sh "$tmp_script"
  rm -f "$tmp_script"
}

install_docker_from_system_packages() {
  log "Installing Docker from the distribution package repositories"
  case "$PKG_MANAGER" in
    zypper)
      install_packages docker docker-compose
      ;;
    pacman)
      install_packages docker docker-compose
      ;;
    apk)
      install_packages docker docker-cli-compose
      ;;
    *)
      echo "No system-package Docker installation path defined for ${PKG_MANAGER}."
      exit 1
      ;;
  esac
}

install_compose_fallback() {
  if docker compose version >/dev/null 2>&1 || command_exists docker-compose; then
    return 0
  fi

  log "Docker Compose plugin not found yet, trying distro fallback packages"
  case "$PKG_MANAGER" in
    apt|dnf|yum)
      try_install_package docker-compose-plugin || true
      try_install_package docker-compose || true
      ;;
    zypper|pacman)
      try_install_package docker-compose || true
      ;;
    apk)
      try_install_package docker-cli-compose || true
      try_install_package docker-compose || true
      ;;
  esac
}

start_and_enable_docker() {
  if command_exists systemctl; then
    set +e
    $SUDO systemctl enable docker >/dev/null 2>&1
    $SUDO systemctl start docker >/dev/null 2>&1
    local systemctl_status=$?
    set -e
    if [[ "$systemctl_status" -eq 0 ]]; then
      return 0
    fi
  fi

  if command_exists rc-update; then
    $SUDO rc-update add docker default >/dev/null 2>&1 || true
    if command_exists rc-service; then
      $SUDO rc-service docker start
    else
      $SUDO service docker start
    fi
    return 0
  fi

  if command_exists service; then
    $SUDO service docker start
    return 0
  fi

  echo "Docker was installed, but no supported service manager was found to start it automatically."
  echo "Please start the docker service manually and re-run the installer."
  exit 1
}

docker_restart_hint() {
  if command_exists systemctl; then
    printf '%s' 'sudo systemctl restart docker'
    return 0
  fi

  if command_exists rc-service; then
    printf '%s' 'sudo rc-service docker restart'
    return 0
  fi

  if command_exists service; then
    printf '%s' 'sudo service docker restart'
    return 0
  fi

  printf '%s' 'restart the docker service with your distro service manager'
}

detect_compose_command() {
  if docker compose version >/dev/null 2>&1; then
    COMPOSE_COMMAND_LABEL="docker compose"
    return 0
  fi

  if command_exists docker-compose; then
    COMPOSE_COMMAND_LABEL="docker-compose"
    return 0
  fi

  return 1
}

run_compose() {
  if [[ "$COMPOSE_COMMAND_LABEL" == "docker compose" ]]; then
    $SUDO docker compose "$@"
  elif [[ "$COMPOSE_COMMAND_LABEL" == "docker-compose" ]]; then
    $SUDO docker-compose "$@"
  else
    echo "Docker Compose command not detected."
    exit 1
  fi
}

install_docker() {
  if command_exists docker; then
    install_compose_fallback
    if detect_compose_command; then
      log "Docker and ${COMPOSE_COMMAND_LABEL} already installed"
      start_and_enable_docker
      return 0
    fi
  fi

  case "$PKG_MANAGER" in
    apt|dnf|yum)
      docker_install_via_official_script
      ;;
    zypper|pacman|apk)
      install_docker_from_system_packages
      ;;
  esac

  install_compose_fallback
  start_and_enable_docker

  if ! detect_compose_command; then
    echo "Docker was installed, but neither 'docker compose' nor 'docker-compose' is available."
    exit 1
  fi
}

group_exists() {
  if command_exists getent; then
    getent group docker >/dev/null 2>&1
  else
    grep -q '^docker:' /etc/group 2>/dev/null
  fi
}

create_docker_group_if_missing() {
  if group_exists; then
    return 0
  fi

  if command_exists groupadd; then
    $SUDO groupadd docker
  elif command_exists addgroup; then
    set +e
    $SUDO addgroup docker >/dev/null 2>&1
    local status=$?
    set -e
    if [[ "$status" -ne 0 ]]; then
      $SUDO addgroup -S docker
    fi
  fi
}

user_in_docker_group() {
  local target_user
  target_user="${SUDO_USER:-${USER:-root}}"
  id -nG "$target_user" 2>/dev/null | tr ' ' '\n' | grep -qx docker
}

add_user_to_docker_group() {
  local target_user
  target_user="${SUDO_USER:-${USER:-root}}"

  if [[ "$target_user" == "root" ]]; then
    return 0
  fi

  if command_exists usermod; then
    $SUDO usermod -aG docker "$target_user"
  elif command_exists gpasswd; then
    $SUDO gpasswd -a "$target_user" docker
  elif command_exists addgroup; then
    $SUDO addgroup "$target_user" docker
  else
    echo "Could not find a supported command to add ${target_user} to the docker group."
    echo "Please add the user to the docker group manually after installation."
  fi
}

configure_docker_group() {
  local target_user
  target_user="${SUDO_USER:-${USER:-root}}"
  create_docker_group_if_missing

  if [[ "$target_user" != "root" ]] && user_in_docker_group; then
    log "User ${target_user} already belongs to docker group"
    return 0
  fi

  log "Adding ${target_user} to docker group"
  add_user_to_docker_group
}

rand_string() {
  local length="$1"
  set +o pipefail
  local value
  value="$(tr -dc 'A-Za-z0-9' </dev/urandom | head -c "$length")"
  set -o pipefail
  printf '%s' "$value"
}

ensure_env_file() {
  if [[ ! -f .env ]]; then
    cp .env.example .env
  fi
}

set_env_value() {
  local key="$1"
  local value="$2"
  if grep -q "^${key}=" .env 2>/dev/null; then
    sed -i "s|^${key}=.*|${key}=${value}|" .env
  else
    printf '%s=%s\n' "$key" "$value" >> .env
  fi
}

read_env_value() {
  grep -E "^$1=" .env 2>/dev/null | tail -n 1 | cut -d'=' -f2- || true
}

detect_server_ip() {
  local ip
  ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
  if [[ -n "$ip" ]]; then
    printf '%s' "$ip"
    return 0
  fi

  if command_exists ip; then
    ip route get 1.1.1.1 2>/dev/null | awk '/src/ {for (i = 1; i <= NF; i++) if ($i == "src") {print $(i+1); exit}}'
  fi
}

wait_for_http() {
  local url="$1"
  local attempts="${2:-60}"
  local sleep_seconds="${3:-2}"
  local i

  for ((i=1; i<=attempts; i++)); do
    if command_exists curl && curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep "$sleep_seconds"
  done
  return 1
}

validate_password() {
  local username="$1"
  local password="$2"
  if [[ "${#password}" -lt 12 ]]; then
    echo "Password must be at least 12 characters long."
    return 1
  fi
  if [[ ! "$password" =~ [A-Za-z] || ! "$password" =~ [0-9] ]]; then
    echo "Password must contain both letters and numbers."
    return 1
  fi
  if [[ "$password" == "$username" ]]; then
    echo "Password must not match the username."
    return 1
  fi
  return 0
}

prompt_bootstrap_credentials() {
  local default_user="admin"
  local input_user=""
  local password_one=""
  local password_two=""

  read -r -p "Bootstrap admin username [${default_user}]: " input_user
  BOOTSTRAP_USERNAME="${input_user:-$default_user}"

  echo "Enter a bootstrap admin password, or press Enter to auto-generate one."
  read -r -s -p "Bootstrap admin password: " password_one
  echo
  if [[ -z "$password_one" ]]; then
    GENERATED_PASSWORD="true"
    BOOTSTRAP_PASSWORD="$(rand_string 24)"
    FORCE_PASSWORD_CHANGE="true"
    return
  fi

  read -r -s -p "Confirm bootstrap admin password: " password_two
  echo
  if [[ "$password_one" != "$password_two" ]]; then
    echo "Password confirmation does not match."
    exit 1
  fi

  validate_password "$BOOTSTRAP_USERNAME" "$password_one" || exit 1
  GENERATED_PASSWORD="false"
  BOOTSTRAP_PASSWORD="$password_one"
  FORCE_PASSWORD_CHANGE="false"
}

prepare_runtime() {
  log "Preparing environment files and runtime folders"
  ensure_env_file

  mkdir -p metadata state
  chmod 700 metadata state 2>/dev/null || true
  chmod 600 .env 2>/dev/null || true

  if [[ -z "$(read_env_value SESSION_SECRET)" || "$(read_env_value SESSION_SECRET)" == "CHANGE_ME" ]]; then
    set_env_value SESSION_SECRET "$(rand_string 48)"
  fi

  if [[ -z "$(read_env_value CONTROL_BOOTSTRAP_USERNAME)" ]]; then
    set_env_value CONTROL_BOOTSTRAP_USERNAME "admin"
  fi

  if [[ -z "$(read_env_value PUBLIC_BASE_URL)" ]]; then
    local ip port
    ip="$(detect_server_ip || true)"
    port="$(read_env_value CONTROL_PORT)"
    port="${port:-8080}"
    if [[ -n "$ip" ]]; then
      set_env_value PUBLIC_BASE_URL "http://${ip}:${port}"
    fi
  fi
}

bootstrap_credentials_if_needed() {
  FIRST_BOOTSTRAP="false"
  GENERATED_PASSWORD="false"
  FORCE_PASSWORD_CHANGE="$(read_env_value CONTROL_BOOTSTRAP_FORCE_PASSWORD_CHANGE)"
  BOOTSTRAP_USERNAME="$(read_env_value CONTROL_BOOTSTRAP_USERNAME)"
  BOOTSTRAP_PASSWORD="$(read_env_value CONTROL_BOOTSTRAP_PASSWORD)"

  if [[ ! -f metadata/registry_meta.db ]]; then
    FIRST_BOOTSTRAP="true"
    prompt_bootstrap_credentials
    set_env_value CONTROL_BOOTSTRAP_USERNAME "$BOOTSTRAP_USERNAME"
    set_env_value CONTROL_BOOTSTRAP_PASSWORD "$BOOTSTRAP_PASSWORD"
    set_env_value CONTROL_BOOTSTRAP_FORCE_PASSWORD_CHANGE "$FORCE_PASSWORD_CHANGE"
  else
    log "Existing metadata database found, keeping current admin account"
  fi
}

start_stack() {
  log "Building and starting the Docker stack with ${COMPOSE_COMMAND_LABEL}"
  run_compose up -d --build
}

print_summary() {
  local control_port registry_port public_base_url health_url healthy restart_hint
  control_port="$(read_env_value CONTROL_PORT)"
  registry_port="$(read_env_value REGISTRY_PORT)"
  public_base_url="$(read_env_value PUBLIC_BASE_URL)"
  restart_hint="$(docker_restart_hint)"
  control_port="${control_port:-8080}"
  registry_port="${registry_port:-5000}"
  public_base_url="${public_base_url:-http://YOUR_SERVER_IP:${control_port}}"
  health_url="http://127.0.0.1:${control_port}/healthz"
  healthy="false"

  if wait_for_http "$health_url" 60 2; then
    healthy="true"
  fi

  if [[ "$FIRST_BOOTSTRAP" == "true" && "$healthy" == "true" ]]; then
    set_env_value CONTROL_BOOTSTRAP_PASSWORD ""
  fi

  cat <<EOF

============================================================
Registry Mirror Stack installation completed
============================================================
Linux distribution:
  ${OS_PRETTY_NAME}

Package manager:
  ${PKG_MANAGER}

Compose command:
  ${COMPOSE_COMMAND_LABEL}

Registry mirror:
  http://YOUR_SERVER_IP:${registry_port}

Web UI / API:
  ${public_base_url}/login

Health check:
  ${public_base_url}/healthz

Useful commands:
  cd ${ROOT_DIR}
  sudo ${COMPOSE_COMMAND_LABEL} ps
  sudo ${COMPOSE_COMMAND_LABEL} logs -f control
  sudo ${COMPOSE_COMMAND_LABEL} logs -f registry
  sudo ${COMPOSE_COMMAND_LABEL} up -d --build
  sudo ${COMPOSE_COMMAND_LABEL} restart
EOF

  if [[ "$FIRST_BOOTSTRAP" == "true" ]]; then
    cat <<EOF

First login username:
  ${BOOTSTRAP_USERNAME}
EOF
    if [[ "$GENERATED_PASSWORD" == "true" ]]; then
      cat <<EOF
First login password:
  ${BOOTSTRAP_PASSWORD}

The generated password is shown only now.
The system will force a password change after the first login.
EOF
    else
      cat <<'EOF'
First login password:
  The password you entered during installation.
EOF
    fi
  fi

  if [[ "$healthy" != "true" ]]; then
    cat <<EOF

Warning:
  The control service did not report healthy within the expected time.
  Check:
    sudo ${COMPOSE_COMMAND_LABEL} ps
    sudo ${COMPOSE_COMMAND_LABEL} logs -f control
EOF
  fi

  cat <<EOF

Docker daemon mirror example on your client machine:
  sudo mkdir -p /etc/docker
  sudo tee /etc/docker/daemon.json >/dev/null <<'JSON'
  {
    "registry-mirrors": ["http://YOUR_SERVER_IP:${registry_port}"]
  }
  JSON
  ${restart_hint}

If you were just added to the docker group, reconnect your SSH session once before using docker without sudo.
EOF
}

main() {
  detect_linux_env
  update_upgrade_system
  install_base_packages
  install_docker
  configure_docker_group
  prepare_runtime
  bootstrap_credentials_if_needed
  start_stack
  print_summary
}

main "$@"
