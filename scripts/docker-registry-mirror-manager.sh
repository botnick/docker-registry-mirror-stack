#!/usr/bin/env bash
set -Eeuo pipefail

MIRROR_URL="${MIRROR_URL:-http://127.0.0.1:5000}"
CONTROL_URL="${CONTROL_URL:-http://127.0.0.1:8080}"
TARGET_TIME_BKK="${TARGET_TIME_BKK:-}"
TARGET_TZ="${TARGET_TZ:-Asia/Bangkok}"

INSTALL_PATH="/usr/local/sbin/docker-registry-mirror-manager.sh"
STATE_DIR="/var/lib/docker-registry-mirror-manager"
STATE_FILE="$STATE_DIR/state.env"
ROLLBACK_SCRIPT="/usr/local/sbin/docker-registry-mirror-rollback.sh"

DOCKER_DAEMON_JSON="/etc/docker/daemon.json"
BACKUP_JSON="$STATE_DIR/daemon.json.backup"

SYSTEMD_SERVICE="docker-registry-mirror-rollback.service"
SYSTEMD_TIMER="docker-registry-mirror-rollback.timer"
AT_JOB_ID_FILE="$STATE_DIR/at_job_id"

SELF_DELETE="false"

log()  { echo "[INFO] $*"; }
warn() { echo "[WARN] $*" >&2; }
err()  { echo "[ERROR] $*" >&2; }
die()  { err "$*"; exit 1; }

need_root() {
  [[ "${EUID:-$(id -u)}" -eq 0 ]] || die "กรุณารันด้วย root"
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "ไม่พบคำสั่งที่จำเป็น: $1"
}

parse_args() {
  COMMAND="${1:-}"
  [[ -n "$COMMAND" ]] || return 0
  shift || true

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --mirror-url)
        [[ $# -ge 2 ]] || die "ต้องใส่ค่าหลัง --mirror-url"
        MIRROR_URL="$2"
        shift 2
        ;;
      --control-url)
        [[ $# -ge 2 ]] || die "ต้องใส่ค่าหลัง --control-url"
        CONTROL_URL="$2"
        shift 2
        ;;
      --rollback-at)
        [[ $# -ge 2 ]] || die "ต้องใส่ค่าหลัง --rollback-at"
        TARGET_TIME_BKK="$2"
        shift 2
        ;;
      --timezone)
        [[ $# -ge 2 ]] || die "ต้องใส่ค่าหลัง --timezone"
        TARGET_TZ="$2"
        shift 2
        ;;
      --self-delete)
        SELF_DELETE="true"
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        die "ไม่รู้จัก option: $1"
        ;;
    esac
  done
}

ensure_dirs() {
  mkdir -p "$STATE_DIR"
  chmod 700 "$STATE_DIR"
}

save_state() {
  cat > "$STATE_FILE" <<EOF
MIRROR_URL='$MIRROR_URL'
CONTROL_URL='$CONTROL_URL'
TARGET_TIME_BKK='$TARGET_TIME_BKK'
TARGET_TZ='$TARGET_TZ'
INSTALL_PATH='$INSTALL_PATH'
STATE_DIR='$STATE_DIR'
STATE_FILE='$STATE_FILE'
ROLLBACK_SCRIPT='$ROLLBACK_SCRIPT'
DOCKER_DAEMON_JSON='$DOCKER_DAEMON_JSON'
BACKUP_JSON='$BACKUP_JSON'
SYSTEMD_SERVICE='$SYSTEMD_SERVICE'
SYSTEMD_TIMER='$SYSTEMD_TIMER'
AT_JOB_ID_FILE='$AT_JOB_ID_FILE'
EOF
  chmod 600 "$STATE_FILE"
}

load_state_if_present() {
  if [[ -f "$STATE_FILE" ]]; then
    # shellcheck disable=SC1090
    source "$STATE_FILE"
  fi
}

install_self() {
  local src
  src="$(readlink -f "$0")"
  if [[ "$src" != "$INSTALL_PATH" ]]; then
    cp -f "$src" "$INSTALL_PATH"
    chmod 700 "$INSTALL_PATH"
    log "ติดตั้ง script ไปที่ $INSTALL_PATH"
  fi
}

write_rollback_wrapper() {
  cat > "$ROLLBACK_SCRIPT" <<EOF
#!/usr/bin/env bash
set -Eeuo pipefail
exec "$INSTALL_PATH" rollback --self-delete
EOF
  chmod 700 "$ROLLBACK_SCRIPT"
}

docker_service_name() {
  if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files 2>/dev/null | grep -q '^docker\.service'; then
    echo "docker.service"
  else
    echo "docker"
  fi
}

restart_docker() {
  local svc
  svc="$(docker_service_name)"
  if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
    systemctl restart "$svc"
    return 0
  fi
  if command -v service >/dev/null 2>&1; then
    service docker restart
    return 0
  fi
  die "ไม่พบ systemctl หรือ service สำหรับ restart docker"
}

docker_mirrors_json() {
  docker system info --format '{{json .RegistryConfig.Mirrors}}' 2>/dev/null || true
}

verify_mirror_enabled() {
  local mirrors
  mirrors="$(docker_mirrors_json)"
  echo "$mirrors" | grep -qF "$MIRROR_URL"
}

verify_mirror_disabled() {
  local mirrors
  mirrors="$(docker_mirrors_json)"
  ! echo "$mirrors" | grep -qF "$MIRROR_URL"
}

backup_current_config_once() {
  if [[ ! -f "$BACKUP_JSON" ]]; then
    if [[ -f "$DOCKER_DAEMON_JSON" ]]; then
      cp -a "$DOCKER_DAEMON_JSON" "$BACKUP_JSON"
      log "backup config เดิมไว้ที่ $BACKUP_JSON"
    else
      printf '{}\n' > "$BACKUP_JSON"
      chmod 600 "$BACKUP_JSON"
      log "ไม่พบ daemon.json เดิม สร้าง backup เป็น {}"
    fi
  fi
}

apply_mirror_json() {
  need_cmd python3

  MIRROR_URL="$MIRROR_URL" DOCKER_DAEMON_JSON="$DOCKER_DAEMON_JSON" python3 - <<'PY'
import json
import os
from pathlib import Path

daemon_path = Path(os.environ["DOCKER_DAEMON_JSON"])
mirror = os.environ["MIRROR_URL"]

if daemon_path.exists():
    raw = daemon_path.read_text(encoding="utf-8").strip()
    data = {} if not raw else json.loads(raw)
else:
    data = {}

if not isinstance(data, dict):
    raise SystemExit("daemon.json ไม่ใช่ JSON object")

mirrors = data.get("registry-mirrors", [])
if not isinstance(mirrors, list):
    mirrors = [mirrors]

if mirror not in mirrors:
    mirrors.insert(0, mirror)

seen = set()
new_mirrors = []
for item in mirrors:
    if isinstance(item, str) and item not in seen:
        seen.add(item)
        new_mirrors.append(item)

data["registry-mirrors"] = new_mirrors

daemon_path.parent.mkdir(parents=True, exist_ok=True)
tmp = daemon_path.with_suffix(".json.tmp")
tmp.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
os.replace(tmp, daemon_path)
PY
}

rollback_mirror_json() {
  need_cmd python3

  if [[ -f "$BACKUP_JSON" ]]; then
    cp -f "$BACKUP_JSON" "$DOCKER_DAEMON_JSON"
    log "restore config จาก backup"
    return 0
  fi

  MIRROR_URL="$MIRROR_URL" DOCKER_DAEMON_JSON="$DOCKER_DAEMON_JSON" python3 - <<'PY'
import json
import os
from pathlib import Path

daemon_path = Path(os.environ["DOCKER_DAEMON_JSON"])
mirror = os.environ["MIRROR_URL"]

if not daemon_path.exists():
    raise SystemExit(0)

raw = daemon_path.read_text(encoding="utf-8").strip()
if not raw:
    raise SystemExit(0)

data = json.loads(raw)
if not isinstance(data, dict):
    raise SystemExit("daemon.json ไม่ใช่ JSON object")

mirrors = data.get("registry-mirrors")
if isinstance(mirrors, list):
    mirrors = [m for m in mirrors if m != mirror]
    if mirrors:
        data["registry-mirrors"] = mirrors
    else:
        data.pop("registry-mirrors", None)
elif mirrors == mirror:
    data.pop("registry-mirrors", None)

tmp = daemon_path.with_suffix(".json.tmp")
tmp.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
os.replace(tmp, daemon_path)
PY
}

control_healthz() {
  printf '%s' "${CONTROL_URL%/}/healthz"
}

verify_control_if_possible() {
  if command -v curl >/dev/null 2>&1; then
    if curl -fsS "$(control_healthz)" >/dev/null 2>&1; then
      log "control plane ตอบกลับได้ที่ $(control_healthz)"
    else
      warn "ยังเช็ก control plane ที่ $(control_healthz) ไม่ผ่าน แต่จะตั้ง mirror ต่อให้"
    fi
  fi
}

local_schedule_time() {
  need_cmd python3
  TARGET_TIME_BKK="$TARGET_TIME_BKK" TARGET_TZ="$TARGET_TZ" python3 - <<'PY'
from datetime import datetime
from zoneinfo import ZoneInfo
import os

target_time = os.environ["TARGET_TIME_BKK"].strip()
target_tz = os.environ["TARGET_TZ"].strip()

if not target_time:
    raise SystemExit(0)

target = datetime.strptime(target_time, "%Y-%m-%d %H:%M:%S").replace(tzinfo=ZoneInfo(target_tz))
print(target.astimezone().strftime("%Y-%m-%d %H:%M:%S"))
PY
}

schedule_with_systemd() {
  local local_time
  local_time="$(local_schedule_time)"
  [[ -n "$local_time" ]] || die "schedule เวลา rollback ไม่ถูกต้อง"

  cat > "/etc/systemd/system/$SYSTEMD_SERVICE" <<EOF
[Unit]
Description=Rollback Docker registry mirror at scheduled time

[Service]
Type=oneshot
ExecStart=$ROLLBACK_SCRIPT
EOF

  cat > "/etc/systemd/system/$SYSTEMD_TIMER" <<EOF
[Unit]
Description=Timer for Docker registry mirror rollback

[Timer]
OnCalendar=$local_time
Persistent=true
Unit=$SYSTEMD_SERVICE

[Install]
WantedBy=timers.target
EOF

  systemctl daemon-reload
  systemctl enable --now "$SYSTEMD_TIMER"
  log "schedule rollback ด้วย systemd timer สำเร็จ"
  systemctl list-timers "$SYSTEMD_TIMER" --no-pager || true
}

schedule_with_at() {
  need_cmd at
  need_cmd python3

  local local_time
  local_time="$(TARGET_TIME_BKK="$TARGET_TIME_BKK" TARGET_TZ="$TARGET_TZ" python3 - <<'PY'
from datetime import datetime
from zoneinfo import ZoneInfo
import os

target_time = os.environ["TARGET_TIME_BKK"].strip()
target_tz = os.environ["TARGET_TZ"].strip()
target = datetime.strptime(target_time, "%Y-%m-%d %H:%M:%S").replace(tzinfo=ZoneInfo(target_tz))
print(target.astimezone().strftime("%Y%m%d%H%M"))
PY
)"

  local at_output
  at_output="$(echo "$ROLLBACK_SCRIPT" | at -t "$local_time" 2>&1)" || {
    echo "$at_output" >&2
    die "schedule rollback ด้วย at ไม่สำเร็จ"
  }

  echo "$at_output" | awk '/job[[:space:]]+[0-9]+/{print $2}' > "$AT_JOB_ID_FILE" || true
  log "schedule rollback ด้วย at สำเร็จ"
  [[ -s "$AT_JOB_ID_FILE" ]] && log "AT job id: $(cat "$AT_JOB_ID_FILE")"
}

schedule_rollback_if_requested() {
  if [[ -z "$TARGET_TIME_BKK" ]]; then
    log "ไม่ได้ตั้ง TARGET_TIME_BKK จึงข้ามการ schedule rollback อัตโนมัติ"
    return 0
  fi

  clear_schedules
  if command -v systemctl >/dev/null 2>&1; then
    schedule_with_systemd
  elif command -v at >/dev/null 2>&1; then
    schedule_with_at
  else
    die "ไม่พบทั้ง systemctl และ at จึงตั้ง schedule rollback อัตโนมัติไม่ได้"
  fi
}

clear_schedules() {
  if command -v systemctl >/dev/null 2>&1; then
    systemctl disable --now "$SYSTEMD_TIMER" >/dev/null 2>&1 || true
    rm -f "/etc/systemd/system/$SYSTEMD_TIMER" "/etc/systemd/system/$SYSTEMD_SERVICE"
    systemctl daemon-reload || true
    systemctl reset-failed >/dev/null 2>&1 || true
  fi

  if [[ -f "$AT_JOB_ID_FILE" ]] && command -v atrm >/dev/null 2>&1; then
    local jobid
    jobid="$(cat "$AT_JOB_ID_FILE" 2>/dev/null || true)"
    [[ -n "$jobid" ]] && atrm "$jobid" >/dev/null 2>&1 || true
    rm -f "$AT_JOB_ID_FILE"
  fi
}

cleanup_self_files() {
  rm -f "$ROLLBACK_SCRIPT"
  rm -f "$INSTALL_PATH"
  rm -f "$STATE_FILE"
  rm -f "$BACKUP_JSON"
  rmdir "$STATE_DIR" 2>/dev/null || true
}

show_status() {
  load_state_if_present
  echo "Mirror target: $MIRROR_URL"
  echo "Control URL: $CONTROL_URL"
  if [[ -n "$TARGET_TIME_BKK" ]]; then
    echo "Target rollback time: $TARGET_TIME_BKK $TARGET_TZ"
  else
    echo "Target rollback time: <not scheduled>"
  fi
  echo

  if [[ -f "$DOCKER_DAEMON_JSON" ]]; then
    echo "Current $DOCKER_DAEMON_JSON:"
    cat "$DOCKER_DAEMON_JSON"
  else
    echo "Current $DOCKER_DAEMON_JSON: <not found>"
  fi
  echo

  echo "docker system info --format '{{json .RegistryConfig.Mirrors}}'"
  docker system info --format '{{json .RegistryConfig.Mirrors}}' 2>/dev/null || true
  echo

  if command -v curl >/dev/null 2>&1; then
    echo "Control health:"
    curl -fsS "$(control_healthz)" 2>/dev/null || true
    echo
  fi

  if command -v systemctl >/dev/null 2>&1; then
    systemctl status "$SYSTEMD_TIMER" --no-pager 2>/dev/null || true
  fi

  if [[ -f "$AT_JOB_ID_FILE" ]]; then
    echo "AT job id: $(cat "$AT_JOB_ID_FILE")"
  fi
}

apply() {
  need_root
  need_cmd docker
  ensure_dirs
  save_state
  install_self
  write_rollback_wrapper
  backup_current_config_once
  verify_control_if_possible
  apply_mirror_json
  restart_docker

  if ! verify_mirror_enabled; then
    die "ตั้ง mirror แล้ว แต่ docker ยังไม่เห็น $MIRROR_URL"
  fi

  schedule_rollback_if_requested
  log "ตั้ง mirror สำเร็จและตรวจแล้วว่าใช้งานได้จริง"
}

rollback() {
  need_root
  need_cmd docker
  load_state_if_present

  clear_schedules
  rollback_mirror_json
  restart_docker

  if ! verify_mirror_disabled; then
    die "rollback แล้ว แต่ docker ยังเห็น mirror อยู่"
  fi

  log "rollback สำเร็จ กลับไปใช้ Docker แบบปกติแล้ว"

  if [[ "$SELF_DELETE" == "true" ]]; then
    cleanup_self_files
    log "ลบ script และไฟล์ประกอบของตัวเองแล้ว"
  fi
}

usage() {
  cat <<EOF
Usage:
  $0 apply [--mirror-url URL] [--control-url URL] [--rollback-at "YYYY-MM-DD HH:MM:SS"] [--timezone Asia/Bangkok]
  $0 rollback [--self-delete]
  $0 status

Examples:
  sudo $0 apply --mirror-url http://185.84.161.209:5000 --control-url http://185.84.161.209:8080
  sudo $0 apply --mirror-url http://185.84.161.209:5000 --control-url http://185.84.161.209:8080 --rollback-at "2026-04-08 12:00:00"
  sudo $0 rollback
  sudo $0 status
EOF
}

main() {
  parse_args "$@"

  case "$COMMAND" in
    apply)
      apply
      ;;
    rollback)
      rollback
      ;;
    status)
      show_status
      ;;
    ""|-h|--help)
      usage
      ;;
    *)
      usage
      exit 1
      ;;
  esac
}

main "$@"
