#!/usr/bin/env bash
# TEMPORARY probe for FU-PERFFLAKE; removed before the fix lands.
# Times the notify-held-local call in a loop on the runner, with and without pending writeback,
# to find where the slow runs come from. Prints numbers only.
set -uo pipefail
bin=${1:-dist/yo-human}
runs=${RUNS:-60}

fixture() {
  local dir=$1
  mkdir -p "$dir/config/yo-human"
  cat > "$dir/config/yo-human/config.yaml" <<'CFG'
version: 1
channels:
  phone: { type: ntfy, server: "https://ntfy.example.com", topic: { secret: ntfy-topic } }
  pager: { type: pagerduty, routing_key: { secret: pd-critical } }
  journal: { type: log, path: /dev/null }
levels:
  - { name: note, use_when: "FYI only", ask: optional, send: [ { channel: journal, interruption: passive }, { channel: phone, interruption: passive } ] }
  - { name: night, use_when: "can wait until morning", quiet_hours: hold, send: [ { channel: pager, interruption: active } ] }
quiet_hours:
  timezone: UTC
  windows: [ { days: [mon, tue, wed, thu, fri, sat, sun], from: "00:00", to: "00:00" } ]
CFG
  printf 'ntfy-topic: FAKE-PERF-TOPIC\npd-critical: FAKE-PERF-ROUTING\n' > "$dir/config/yo-human/secrets.yaml"
  chmod 600 "$dir/config/yo-human/"*
}

dirty() { grep -E '^(Dirty|Writeback):' /proc/meminfo 2>/dev/null | tr -s ' ' | tr '\n' ' '; echo; }

loop() {
  local label=$1 dir; dir=$(mktemp -d)
  fixture "$dir"
  local t=()
  for _ in $(seq "$runs"); do
    local s e
    s=$(date +%s%N)
    HOME=$dir XDG_CONFIG_HOME=$dir/config XDG_STATE_HOME=$dir/state YO_HUMAN_CONFIG= \
      "$bin" notify --level night --title perf --ask look >/dev/null 2>&1
    e=$(date +%s%N)
    t+=($(( (e - s) / 100000 )))
  done
  printf '%s runs(0.1ms, in order): %s\n' "$label" "${t[*]}"
  printf '%s sorted top5: %s\n' "$label" "$(printf '%s\n' "${t[@]}" | sort -n | tail -5 | tr '\n' ' ')"
  rm -rf "$dir"
}

churn() { go build -a -o "$(mktemp -d)/perf" ./tools/perf; }

echo "fs: $(df -T . | tail -1 | awk '{print $2}')"
for round in 1 2 3; do
  churn; echo "round $round A (after build, no sync): $(dirty)"; loop "r${round}A"
  churn; sync; echo "round $round B (after build + sync): $(dirty)"; loop "r${round}B"
done

if command -v strace >/dev/null; then
  churn
  dir=$(mktemp -d); fixture "$dir"
  for i in 1 2 3 4 5; do
    HOME=$dir XDG_CONFIG_HOME=$dir/config XDG_STATE_HOME=$dir/state YO_HUMAN_CONFIG= \
      strace -f -T -e trace=fsync,fdatasync,flock,rename,renameat,renameat2 -o "$dir/st.$i" \
      "$bin" notify --level night --title perf --ask look >/dev/null 2>&1
    printf 'strace run %s slowest: %s\n' "$i" \
      "$(grep -o '^[0-9]* *[a-z0-9]*(.*<[0-9.]*>' "$dir/st.$i" | sed -E 's/^[0-9]+ +([a-z0-9]+)\(.*<([0-9.]+)>/\1 \2/' | sort -k2 -rn | head -4 | tr '\n' ';')"
  done
  rm -rf "$dir"
else
  echo "strace: not installed"
fi
