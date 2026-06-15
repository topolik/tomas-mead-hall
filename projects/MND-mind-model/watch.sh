#!/usr/bin/env bash
# watch.sh — run MND's retrain loop as a background daemon in a tmux session
# (same pattern as LLP/GML watch.sh). The session survives your shell.
#
# Usage:
#   ./watch.sh status            # running? last retrain result?
#   ./watch.sh start             # start the daily retrain loop in the background
#   ./watch.sh stop              # stop it
#   ./watch.sh restart           # stop + start (rotates the log)
#   ./watch.sh logs [--follow]   # show the log
#   ./watch.sh attach            # attach to the tmux session (Ctrl+B then D to detach)
#
# Cadence: MND_RETRAIN_INTERVAL seconds (default 86400 = daily). The model can
# be pinned with MND_MODEL (default gemini → LLP auto).
# Log: ~/.local/share/mnd/retrain.log   (rotated on restart)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LOG_DIR="${HOME}/.local/share/mnd"
SESSION="mnd"
LOGFILE="${LOG_DIR}/retrain.log"

is_running() { tmux has-session -t "$SESSION" 2>/dev/null; }

do_status() {
  if is_running; then
    local pid size
    pid="$(tmux list-panes -t "$SESSION" -F '#{pane_pid}' 2>/dev/null | head -1)"
    size=""; [[ -f "$LOGFILE" ]] && size=", log: $(du -h "$LOGFILE" | cut -f1)"
    echo "  mnd retrain: running (session: $SESSION, pid: ${pid}${size})"
    if [[ -f "$LOGFILE" ]]; then
      grep -E "retrain run starting|production learning committed|no brain changes|retrain:" "$LOGFILE" 2>/dev/null | tail -3 | sed 's/^/       /'
    fi
    echo "  logs [--follow] | attach | stop | restart"
  else
    [[ -f "$LOGFILE" ]] && echo "  mnd retrain: stopped (log: $(du -h "$LOGFILE" | cut -f1))" || echo "  mnd retrain: stopped"
  fi
}

do_start() {
  if is_running; then
    echo "  mnd retrain: already running (use restart to replace)"
    return 0
  fi
  local branch
  branch="$(git -C "$SCRIPT_DIR" rev-parse --abbrev-ref HEAD 2>/dev/null || echo '?')"
  mkdir -p "$LOG_DIR"
  if [[ -f "$LOGFILE" && -s "$LOGFILE" ]]; then
    local started
    started="$(head -1 "$LOGFILE" | grep -oP '(?<=^# started ).*' || echo unknown)"
    mv "$LOGFILE" "${LOG_DIR}/retrain-${started}--$(date +%Y%m%dT%H%M%S).log"
  fi
  local now; now="$(date +%Y%m%dT%H%M%S)"
  echo "  mnd retrain: starting on $branch (interval ${MND_RETRAIN_INTERVAL:-86400}s)..."
  tmux new-session -d -s "$SESSION" \
    "echo '# started $now' > '$LOGFILE'; '${SCRIPT_DIR}/run-task.sh' watch-retrain 2>&1 | tee -a '$LOGFILE'"
  sleep 2
  if is_running; then
    echo "  mnd retrain: started (log: $LOGFILE)"
    do_status
  else
    echo "  mnd retrain: failed to start — see: ./watch.sh logs" >&2
    [[ -f "$LOGFILE" ]] && tail -8 "$LOGFILE" >&2
    exit 1
  fi
}

do_stop() {
  if is_running; then tmux kill-session -t "$SESSION"; echo "  mnd retrain: stopped"; else echo "  mnd retrain: not running"; fi
}

usage() { sed -n '2,21p' "$0" | sed 's/^# \{0,1\}//'; }

case "${1:-}" in
  status) do_status ;;
  start)  do_start ;;
  stop)   do_stop ;;
  restart) do_stop; do_start ;;
  logs)
    [[ -f "$LOGFILE" ]] || { echo "no log yet ($LOGFILE)"; exit 1; }
    if [[ "${2:-}" == "--follow" || "${2:-}" == "-f" ]]; then exec tail -f "$LOGFILE"; else tail -200 "$LOGFILE"; fi
    ;;
  attach) if is_running; then exec tmux attach -t "$SESSION"; else echo "mnd retrain is not running"; exit 1; fi ;;
  -h|--help|help|"") usage ;;
  *) echo "error: unknown command '$1'" >&2; echo "" >&2; usage; exit 1 ;;
esac
