#!/bin/sh

set -eu

PROJECT_ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
PROJECT_TMP=${PROJECT_TMP:-"$PROJECT_ROOT/tmp"}
NATIVE_TIMEOUT_SECONDS=${NATIVE_SWIFT_TEST_TIMEOUT_SECONDS:-180}
NATIVE_SAMPLE_SECONDS=${NATIVE_SWIFT_TEST_SAMPLE_SECONDS:-3}
TIMEOUT_FLAG="$PROJECT_TMP/native-swift-test-timeout.flag"
SAMPLE_FILE="$PROJECT_TMP/native-swift-test.sample.txt"
COMMAND_PID_FILE="$PROJECT_TMP/native-swift-test.pid"

log() {
  printf '[native/test][%s] %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$*"
}

capture_processes() {
  log "Capturing process snapshot."
  if ps_output=$(ps -axo pid,ppid,pgid,stat,etime,command 2>&1); then
    printf '%s\n' "$ps_output" | grep -E 'swift|swift-package|swiftpm-testing' || true
  else
    log "ps snapshot unavailable: $ps_output"
  fi

  if pgrep_output=$(pgrep -fl 'swift|swift-package|swiftpm-testing' 2>&1); then
    printf '%s\n' "$pgrep_output"
  else
    log "pgrep snapshot unavailable: $pgrep_output"
  fi
}

capture_sample() {
  if ! command -v sample >/dev/null 2>&1; then
    log "Skipping stack sample because 'sample' is unavailable."
    return
  fi

  target_pid=$1
  log "Capturing sample for PID $target_pid into $SAMPLE_FILE."
  sample "$target_pid" "$NATIVE_SAMPLE_SECONDS" -file "$SAMPLE_FILE" >/dev/null 2>&1 || true
  if [ -f "$SAMPLE_FILE" ]; then
    log "Sample captured; showing first 80 lines."
    sed -n '1,80p' "$SAMPLE_FILE" || true
  fi
}

terminate_process_tree() {
  target_pid=$1
  target_pgid=$2

  if [ -n "$target_pgid" ]; then
    log "Sending TERM to process group $target_pgid."
    kill -TERM -- "-$target_pgid" 2>/dev/null || true
  else
    log "Sending TERM to PID $target_pid."
    kill -TERM "$target_pid" 2>/dev/null || true
  fi

  sleep 5

  if kill -0 "$target_pid" 2>/dev/null; then
    if [ -n "$target_pgid" ]; then
      log "Process group $target_pgid still alive; sending KILL."
      kill -KILL -- "-$target_pgid" 2>/dev/null || true
    else
      log "PID $target_pid still alive; sending KILL."
      kill -KILL "$target_pid" 2>/dev/null || true
    fi
  fi
}

mkdir -p "$PROJECT_TMP"
rm -f "$TIMEOUT_FLAG" "$SAMPLE_FILE" "$COMMAND_PID_FILE"

cd "$PROJECT_ROOT/native/OrbitCockpit"

log "GitHub Actions watchdog enabled."
log "Working directory: $(pwd)"
log "Timeout seconds: $NATIVE_TIMEOUT_SECONDS"
log "HOME: ${HOME:-"(unset)"}"
log "PROJECT_TMP: $PROJECT_TMP"

swift --version
xcodebuild -version || true

if [ "$#" -gt 0 ]; then
  if [ "$1" = "--" ]; then
    shift
  fi
  COMMAND_DESC="$*"
  "$@" &
else
  COMMAND_DESC="swift test --disable-sandbox --build-path $PROJECT_TMP/swift-build --disable-xctest --enable-swift-testing"
  swift test --disable-sandbox --build-path "$PROJECT_TMP/swift-build" --disable-xctest --enable-swift-testing &
fi

COMMAND_PID=$!
printf '%s\n' "$COMMAND_PID" >"$COMMAND_PID_FILE"
if pgid_output=$(ps -o pgid= -p "$COMMAND_PID" 2>&1); then
  COMMAND_PGID=$(printf '%s' "$pgid_output" | tr -d ' ')
else
  COMMAND_PGID=""
  log "PGID lookup unavailable: $pgid_output"
fi

log "Launched command: $COMMAND_DESC"
log "Child PID: $COMMAND_PID"
log "Child PGID: ${COMMAND_PGID:-unknown}"
log "Watchdog wait started."

(
  sleep "$NATIVE_TIMEOUT_SECONDS"
  if kill -0 "$COMMAND_PID" 2>/dev/null; then
    log "Watchdog threshold reached while command is still running."
    printf 'timeout\n' >"$TIMEOUT_FLAG"
    capture_processes
    capture_sample "$COMMAND_PID"
    terminate_process_tree "$COMMAND_PID" "$COMMAND_PGID"
  fi
) &
WATCHDOG_PID=$!

cleanup() {
  if kill -0 "$WATCHDOG_PID" 2>/dev/null; then
    kill "$WATCHDOG_PID" 2>/dev/null || true
    wait "$WATCHDOG_PID" 2>/dev/null || true
  fi
}

trap cleanup EXIT INT TERM

set +e
wait "$COMMAND_PID"
COMMAND_STATUS=$?
set -e

cleanup
trap - EXIT INT TERM

if [ -f "$TIMEOUT_FLAG" ]; then
  log "Watchdog forced termination after timeout."
  exit 124
fi

log "Command exited with status $COMMAND_STATUS."
exit "$COMMAND_STATUS"
