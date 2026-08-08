#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPOSITORY_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd)

# 设置 BENCHMARK_HOST 时在远端仓库执行；未设置时直接在当前主机运行。
if [[ ${BENCHMARK_REMOTE_STAGE:-0} != 1 && -n ${BENCHMARK_HOST:-} ]]; then
	: "${BENCHMARK_REMOTE_REPOSITORY:?set BENCHMARK_REMOTE_REPOSITORY to the repository path on the benchmark host}"
	remote_command=
	printf -v remote_command 'cd %q && env %q' "$BENCHMARK_REMOTE_REPOSITORY" 'BENCHMARK_REMOTE_STAGE=1'
	for variable in BENCHMARK_ROOT BENCHMARK_MANIFEST BENCHCTL_BINARY KEEPER_BINARY CPU_LIST PREPARE_DATASET CONTROLLER_ID REDIS_PORT APP_PORT; do
		if [[ -v $variable ]]; then
			printf -v remote_command '%s %q' "$remote_command" "$variable=${!variable}"
		fi
	done
	printf -v remote_command '%s bash %q' "$remote_command" 'internal/benchmark/scripts/run-capacity.sh'
	exec ssh -- "$BENCHMARK_HOST" "$remote_command"
fi

ROOT=${BENCHMARK_ROOT:-/var/tmp/cpa-usage-keeper-capacity}
SOURCE_MANIFEST=${BENCHMARK_MANIFEST:-$REPOSITORY_ROOT/internal/benchmark/manifest/capacity-v1.json}
BENCH=${BENCHCTL_BINARY:-$ROOT/bin/benchctl}
KEEPER=${KEEPER_BINARY:-$ROOT/bin/keeper}
PLAN=$ROOT/config/capacity-v1.plan.json
MANIFEST=$ROOT/config/capacity-v1.json
DATASET_ID=reference-2m
DATASET_DIR=$ROOT/datasets/$DATASET_ID
CPU_LIST=${CPU_LIST:-1,2,4}
PREPARE_DATASET=${PREPARE_DATASET:-0}
CONTROLLER_ID=${CONTROLLER_ID:-capacity-v1-$(date -u +%Y%m%d-%H%M%S)}
REDIS_PORT=${REDIS_PORT:-16379}
APP_PORT=${APP_PORT:-18080}
CONTROLLER_DIR=$ROOT/runs/$CONTROLLER_ID
RESULTS=$CONTROLLER_DIR/controller.tsv

case $(readlink -m -- "$ROOT") in
	/|/root|"${HOME:-/root}") printf 'BENCHMARK_ROOT is too broad: %s\n' "$ROOT" >&2; exit 1 ;;
esac
if [[ ! $CONTROLLER_ID =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]*$ ]]; then
	printf 'CONTROLLER_ID must contain only letters, digits, dot, underscore, or hyphen\n' >&2
	exit 1
fi

for command in go gcc jq lscpu redis-server sqlite3 systemd-run systemctl taskset; do
	command -v "$command" >/dev/null || {
		printf 'required command is unavailable: %s\n' "$command" >&2
		exit 1
	}
done

if [[ $(uname -s) != Linux || $(uname -m) != x86_64 ]]; then
	printf 'capacity-v1 requires linux/amd64\n' >&2
	exit 1
fi

IFS=',' read -r -a cpus <<<"$CPU_LIST"
for cpu in "${cpus[@]}"; do
	case $cpu in
		1|2|4) ;;
		*) printf 'CPU_LIST may contain only 1, 2, and 4: %s\n' "$CPU_LIST" >&2; exit 1 ;;
	esac
done

mkdir -p "$ROOT/bin" "$ROOT/config" "$DATASET_DIR" "$CONTROLLER_DIR"
if [[ $SOURCE_MANIFEST != "$MANIFEST" ]]; then
	cp -- "$SOURCE_MANIFEST" "$MANIFEST"
fi

if [[ -z ${BENCHCTL_BINARY:-} ]]; then
	go build -trimpath -o "$BENCH" ./internal/benchmark/cmd/benchctl
fi
if [[ -z ${KEEPER_BINARY:-} ]]; then
	go build -trimpath -o "$KEEPER" ./cmd/server
fi
[[ -x $BENCH ]] || { printf 'benchctl is not executable: %s\n' "$BENCH" >&2; exit 1; }
[[ -x $KEEPER ]] || { printf 'Keeper is not executable: %s\n' "$KEEPER" >&2; exit 1; }

"$BENCH" plan --manifest "$MANIFEST" --output "$PLAN"

if [[ ! -f $DATASET_DIR/app.db && ! -f $DATASET_DIR/app.db.zst ]]; then
	if [[ $PREPARE_DATASET != 1 ]]; then
		printf 'canonical dataset is missing: %s\n' "$DATASET_DIR" >&2
		printf 'set PREPARE_DATASET=1 once to generate it without Keeper resource limits\n' >&2
		exit 1
	fi
	"$BENCH" generate \
		--manifest "$MANIFEST" \
		--database "$DATASET_DIR/app.db" \
		--result "$DATASET_DIR/dataset.json"
fi

[[ -f $DATASET_DIR/dataset.json ]] || {
	printf 'dataset metadata is missing: %s\n' "$DATASET_DIR/dataset.json" >&2
	exit 1
}
if [[ -f $DATASET_DIR/app.db ]]; then
	"$BENCH" validate --database "$DATASET_DIR/app.db" >/dev/null
fi

RECOMMENDED_RATIO=$(jq -r '.search.recommended_capacity_ratio' "$MANIFEST")
{
	printf 'os=%s\n' "$(. /etc/os-release && printf '%s %s' "$NAME" "$VERSION_ID")"
	printf 'kernel=%s\n' "$(uname -r)"
	printf 'arch=amd64\n'
	printf 'cpu_model=%s\n' "$(lscpu | awk -F: '$1 ~ /^Model name/ {sub(/^[[:space:]]+/, "", $2); print $2; exit}')"
	printf 'online_cpus=%s\n' "$(getconf _NPROCESSORS_ONLN)"
	printf 'memory_kib=%s\n' "$(awk '$1 == "MemTotal:" {print $2}' /proc/meminfo)"
	printf 'go=%s\n' "$(go version)"
	printf 'gcc=%s\n' "$(gcc --version | awk 'NR == 1')"
	printf 'sqlite=%s\n' "$(sqlite3 --version)"
	printf 'redis=%s\n' "$(redis-server --version)"
} >"$CONTROLLER_DIR/environment.txt"

if [[ ! -f $RESULTS ]]; then
	printf 'cpu\tingestion_eps\tdashboard_eps\trecommended_eps\tingestion_cpu_percent\tingestion_peak_memory_mib\tingestion_core_p95_ms\tingestion_overall_p99_ms\tdashboard_core_p95_ms\tdashboard_overall_p99_ms\tdashboard_slowest_path\tdashboard_slowest_p99_ms\tdurable_ratio\tbacklog_end\tcheckpoint_lag\tidentity_pending\tshared_driver\thard_result\tdashboard_result\n' >"$RESULTS"
fi

cell_id() {
	printf 'capacity-reference-2m-%sc-unlimited\n' "$1"
}

result_path() {
	local run_id=$1 cell=$2
	printf '%s/runs/%s/cells/%s/result.json\n' "$ROOT" "$run_id" "$cell"
}

run_benchmark() {
	local run_id=$1 cell=$2 result
	shift 2
	result=$(result_path "$run_id" "$cell")
	if [[ ! -f $result ]]; then
		"$BENCH" run \
			--manifest "$MANIFEST" \
			--plan "$PLAN" \
			--root "$ROOT" \
			--run-id "$run_id" \
			--keeper "$KEEPER" \
			--redis-port "$REDIS_PORT" \
			--app-port "$APP_PORT" \
			--cells "$cell" \
			--max-duration 30m \
			"$@" || true
	fi
	LAST_RESULT=$result
}

next_passing_rate() {
	local search_result=$1 upper=$2 mode=$3
	if [[ $mode == hard ]]; then
		jq -r --argjson upper "$upper" '
			[.attempts[] | select(.phase == "search" and .rate_per_second < $upper and .report.evaluation.hard_pass) | .rate_per_second] | max // 0
		' "$search_result"
	else
		jq -r --argjson upper "$upper" '
			[.attempts[] | select(.phase == "search" and .rate_per_second < $upper and .report.evaluation.interactive_pass) | .rate_per_second] | max // 0
		' "$search_result"
	fi
}

run_fixed() {
	local cpu=$1 label=$2 rate=$3 cell run_id
	cell=$(cell_id "$cpu")
	run_id="${CONTROLLER_ID}-${cpu}c-${label}-${rate}"
	# hard 模式仍完整采集 Dashboard；它允许预期的 Dashboard 边界失败保留为有效 ingestion 证据。
	run_benchmark "$run_id" "$cell" \
		--fixed-rate "$rate" \
		--fixed-duration 5m \
		--fixed-pass hard
}

formal_cpu() {
	local cpu=$1 cell search_id search_result hard_rate dashboard_rate hard_result dashboard_result
	local pass interactive low_pass high_fail midpoint tolerance short_low
	local peak cpu_percent hard_core_p95 hard_overall_p99 dashboard_core_p95 dashboard_overall_p99
	local slowest_path slowest_p99 durable_ratio backlog checkpoint identity shared recommended hard_ref dashboard_ref
	cell=$(cell_id "$cpu")
	search_id="${CONTROLLER_ID}-${cpu}c-search"
	printf '%sc search_start\n' "$cpu" >>"$CONTROLLER_DIR/controller.log"
	run_benchmark "$search_id" "$cell" --search-duration 20s
	search_result=$LAST_RESULT
	if [[ ! -f $search_result ]]; then
		printf '%sc search_result_missing\n' "$cpu" >>"$CONTROLLER_DIR/controller.log"
		return
	fi
	hard_rate=$(jq -r '.capacity.hard_events_per_second // 0' "$search_result")
	dashboard_rate=$(jq -r '.capacity.interactive_events_per_second // 0' "$search_result")
	printf '%sc calibrated ingestion=%s dashboard=%s\n' "$cpu" "$hard_rate" "$dashboard_rate" >>"$CONTROLLER_DIR/controller.log"

	hard_result=
	low_pass=0
	high_fail=0
	while (( hard_rate > 0 )); do
		run_fixed "$cpu" hard "$hard_rate"
		[[ -f $LAST_RESULT ]] || break
		pass=$(jq -r '.attempts[-1].report.evaluation.hard_pass // false' "$LAST_RESULT")
		printf '%sc hard rate=%s pass=%s\n' "$cpu" "$hard_rate" "$pass" >>"$CONTROLLER_DIR/controller.log"
		if [[ $pass == true ]]; then
			low_pass=$hard_rate
			hard_result=$LAST_RESULT
			(( high_fail > 0 )) || break
			tolerance=$((low_pass / 10))
			(( tolerance >= 25 )) || tolerance=25
			(( high_fail - low_pass > tolerance )) || break
			midpoint=$(((low_pass + high_fail) / 2))
			hard_rate=$((((midpoint + 12) / 25) * 25))
			(( hard_rate > low_pass && hard_rate < high_fail )) || break
			continue
		fi
		high_fail=$hard_rate
		if (( low_pass > 0 )); then
			midpoint=$(((low_pass + high_fail) / 2))
			hard_rate=$((((midpoint + 12) / 25) * 25))
			(( hard_rate > low_pass && hard_rate < high_fail )) || break
		else
			short_low=$(next_passing_rate "$search_result" "$hard_rate" hard)
			if (( short_low <= 0 )); then
				hard_rate=0
				continue
			fi
			midpoint=$(((short_low + high_fail) / 2))
			hard_rate=$((((midpoint + 12) / 25) * 25))
			if (( hard_rate <= short_low || hard_rate >= high_fail )); then
				hard_rate=$short_low
			fi
		fi
	done
	hard_rate=$low_pass
	if [[ -z $hard_result ]]; then
		printf '%sc no_five_minute_ingestion_capacity\n' "$cpu" >>"$CONTROLLER_DIR/controller.log"
		return
	fi

	interactive=$(jq -r '.attempts[-1].report.evaluation.interactive_pass // false' "$hard_result")
	dashboard_result=$hard_result
	if [[ $interactive == true ]]; then
		dashboard_rate=$hard_rate
	else
		if (( dashboard_rate <= 0 || dashboard_rate >= hard_rate )); then
			dashboard_rate=$(next_passing_rate "$search_result" "$hard_rate" interactive)
		fi
		dashboard_result=
		while (( dashboard_rate > 0 )); do
			run_fixed "$cpu" dashboard "$dashboard_rate"
			[[ -f $LAST_RESULT ]] || break
			pass=$(jq -r '.attempts[-1].report.evaluation.interactive_pass // false' "$LAST_RESULT")
			printf '%sc dashboard rate=%s pass=%s\n' "$cpu" "$dashboard_rate" "$pass" >>"$CONTROLLER_DIR/controller.log"
			if [[ $pass == true ]]; then
				dashboard_result=$LAST_RESULT
				break
			fi
			dashboard_rate=$(next_passing_rate "$search_result" "$dashboard_rate" interactive)
		done
		if [[ -z $dashboard_result ]]; then
			dashboard_rate=0
			dashboard_result=-
		fi
	fi

	peak=$(jq -r '(.attempts[-1].peak_resource.memory_peak_bytes // .attempts[-1].resource.memory_peak_bytes // 0) / 1048576' "$hard_result")
	cpu_percent=$(jq -r '.attempts[-1].resource.cpu_utilization_percent // 0' "$hard_result")
	hard_core_p95=$(jq -r '.attempts[-1].report.core_latency.p95_ms // 0' "$hard_result")
	hard_overall_p99=$(jq -r '.attempts[-1].report.latency.p99_ms // 0' "$hard_result")
	durable_ratio=$(jq -r 'if .attempts[-1].report.metrics.offered_events > 0 then .attempts[-1].report.metrics.durable_events / .attempts[-1].report.metrics.offered_events else 0 end' "$hard_result")
	backlog=$(jq -r '.attempts[-1].report.metrics.backlog_end // 0' "$hard_result")
	checkpoint=$(jq -r '.attempts[-1].report.metrics.checkpoint_lag // 0' "$hard_result")
	identity=$(jq -r '.attempts[-1].report.metrics.identity_pending // 0' "$hard_result")
	shared=$(jq -r '.shared_driver // false' "$hard_result")
	recommended=$(awk -v rate="$dashboard_rate" -v ratio="$RECOMMENDED_RATIO" 'BEGIN { printf "%d", rate * ratio }')
	if [[ $dashboard_result == - ]]; then
		dashboard_core_p95=0
		dashboard_overall_p99=0
		slowest_path=-
		slowest_p99=0
	else
		dashboard_core_p95=$(jq -r '.attempts[-1].report.core_latency.p95_ms // 0' "$dashboard_result")
		dashboard_overall_p99=$(jq -r '.attempts[-1].report.latency.p99_ms // 0' "$dashboard_result")
		slowest_path=$(jq -r '.attempts[-1].report.latency_by_path | to_entries | max_by(.value.p99_ms) | .key // "-"' "$dashboard_result")
		slowest_p99=$(jq -r '.attempts[-1].report.latency_by_path | to_entries | max_by(.value.p99_ms) | .value.p99_ms // 0' "$dashboard_result")
	fi
	hard_ref=${hard_result#"$ROOT"/}
	if [[ $dashboard_result == - ]]; then
		dashboard_ref=-
	else
		dashboard_ref=${dashboard_result#"$ROOT"/}
	fi
	printf '%s\t%s\t%s\t%s\t%.3f\t%.3f\t%.3f\t%.3f\t%.3f\t%.3f\t%s\t%.3f\t%.6f\t%s\t%s\t%s\t%s\t%s\t%s\n' \
		"$cpu" "$hard_rate" "$dashboard_rate" "$recommended" "$cpu_percent" "$peak" "$hard_core_p95" "$hard_overall_p99" \
		"$dashboard_core_p95" "$dashboard_overall_p99" "$slowest_path" "$slowest_p99" "$durable_ratio" "$backlog" "$checkpoint" "$identity" \
		"$shared" "$hard_ref" "$dashboard_ref" >>"$RESULTS"
	printf '%sc complete ingestion=%s dashboard=%s recommended=%s peak=%.2fMiB\n' "$cpu" "$hard_rate" "$dashboard_rate" "$recommended" "$peak" >>"$CONTROLLER_DIR/controller.log"
}

for cpu in "${cpus[@]}"; do
	if awk -F '\t' -v cpu="$cpu" 'NR > 1 && $1 == cpu { found=1 } END { exit !found }' "$RESULTS"; then
		continue
	fi
	formal_cpu "$cpu"
done

printf 'completed_at\t%s\n' "$(date --iso-8601=seconds)" >>"$CONTROLLER_DIR/controller.log"
{
	printf '# Capacity benchmark summary\n\n'
	printf '| CPU | Ingestion max | Dashboard max | Recommended | Ingestion CPU | Peak memory | Dashboard core p95 | Dashboard overall p99 | Shared driver |\n'
	printf '| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |\n'
	awk -F '\t' 'NR > 1 { printf "| %sC | %s events/s | %s events/s | %s events/s | %.1f%% | %.1f MiB | %.1fms | %.1fms | %s |\n", $1, $2, $3, $4, $5, $6, $9, $10, $17 }' "$RESULTS"
} >"$CONTROLLER_DIR/summary.md"
printf 'capacity results: %s\n' "$RESULTS"
printf 'capacity summary: %s\n' "$CONTROLLER_DIR/summary.md"
