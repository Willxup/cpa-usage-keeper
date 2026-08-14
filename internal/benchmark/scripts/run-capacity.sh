#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPOSITORY_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd)
export TZ=Asia/Shanghai

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
CPU_LIST=${CPU_LIST:-1,2,4}
PREPARE_DATASET=${PREPARE_DATASET:-0}
CONTROLLER_ID=${CONTROLLER_ID:-capacity-v1-$(date -u +%Y%m%d-%H%M%S)}
REDIS_PORT=${REDIS_PORT:-16379}
APP_PORT=${APP_PORT:-18080}
CONTROLLER_DIR=$ROOT/runs/$CONTROLLER_ID
RESULTS=$CONTROLLER_DIR/controller.tsv
RESULTS_HEADER=$'cpu\tingestion_5m_pass_eps\tingestion_lowest_fail_eps\tdashboard_5m_pass_eps\tdashboard_lowest_fail_eps\trecommended_eps\tingestion_cpu_percent\tingestion_peak_memory_mib\tdashboard_peak_memory_mib\tingestion_core_p95_ms\tingestion_core_p99_ms\tdashboard_core_p95_ms\tdashboard_core_p99_ms\tdashboard_analysis_latency_p50_ms\tdashboard_analysis_latency_p95_ms\tdashboard_analysis_latency_p99_ms\tdashboard_analysis_latency_max_ms\tdashboard_analysis_latency_samples\tdashboard_analysis_latency_errors\tdashboard_analysis_latency_status\tdashboard_slowest_core_path\tdashboard_slowest_core_p99_ms\tdurable_ratio\tbacklog_end\tcheckpoint_lag\tidentity_pending\tshared_driver\thard_result\tdashboard_result'

case $(readlink -m -- "$ROOT") in
	/|/root|"${HOME:-/root}") printf 'BENCHMARK_ROOT is too broad: %s\n' "$ROOT" >&2; exit 1 ;;
esac
if [[ ! $CONTROLLER_ID =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]*$ ]]; then
	printf 'CONTROLLER_ID must contain only letters, digits, dot, underscore, or hyphen\n' >&2
	exit 1
fi

for command in go gcc jq lscpu redis-server sha256sum sqlite3 stat systemd-run systemctl taskset; do
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

mkdir -p "$ROOT/bin" "$ROOT/config" "$CONTROLLER_DIR"
if [[ $SOURCE_MANIFEST != "$MANIFEST" ]]; then
	cp -- "$SOURCE_MANIFEST" "$MANIFEST"
fi
if ! DATASET_ID=$(jq -er '.dataset.id | select(type == "string" and length > 0)' "$MANIFEST"); then
	printf 'manifest dataset.id is missing or invalid: %s\n' "$MANIFEST" >&2
	exit 1
fi
if [[ ! $DATASET_ID =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ || $DATASET_ID == *--* ]]; then
	printf 'manifest dataset.id must be a lowercase public ID: %s\n' "$DATASET_ID" >&2
	exit 1
fi
DATASET_DIR=$ROOT/datasets/$DATASET_ID
mkdir -p "$DATASET_DIR"

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
if [[ ! -f $DATASET_DIR/app.db && -f $DATASET_DIR/app.db.zst ]] && ! command -v zstd >/dev/null; then
	printf 'compressed canonical dataset requires zstd: %s\n' "$DATASET_DIR/app.db.zst" >&2
	exit 1
fi

DATASET_METADATA=$DATASET_DIR/dataset.json
[[ -f $DATASET_METADATA ]] || {
	printf 'dataset metadata is missing: %s\n' "$DATASET_DIR/dataset.json" >&2
	exit 1
}
if [[ -f $DATASET_DIR/app.db ]]; then
	DATASET_SOURCE=$DATASET_DIR/app.db
else
	DATASET_SOURCE=$DATASET_DIR/app.db.zst
fi
DATASET_VALIDATION=$CONTROLLER_DIR/dataset-validation.json
if [[ ! -f $DATASET_VALIDATION ]]; then
	validation_tmp=$DATASET_VALIDATION.tmp
	"$BENCH" validate \
		--database "$DATASET_SOURCE" \
		--manifest "$MANIFEST" \
		--metadata "$DATASET_METADATA" >"$validation_tmp"
	mv -- "$validation_tmp" "$DATASET_VALIDATION"
fi

CONTROLLER_INPUTS=$CONTROLLER_DIR/controller-inputs.sha256
inputs_digest=$({
	sha256sum "$SCRIPT_DIR/run-capacity.sh" "$MANIFEST" "$PLAN" "$BENCH" "$KEEPER" "$DATASET_METADATA" "$DATASET_VALIDATION"
	stat --printf='dataset_source=%s:%Y:%i\n' "$DATASET_SOURCE"
	printf 'cpu_list=%s\nredis_port=%s\napp_port=%s\nsearch_strategy=staircase-v1\nsearch_duration=20s\nfixed_duration=5m\n' "$CPU_LIST" "$REDIS_PORT" "$APP_PORT"
} | sha256sum | awk '{print $1}')
if [[ -f $CONTROLLER_INPUTS ]]; then
	if [[ $(<"$CONTROLLER_INPUTS") != "$inputs_digest" ]]; then
		printf 'controller inputs changed; use a new CONTROLLER_ID instead of mixing results\n' >&2
		exit 1
	fi
elif [[ -f $RESULTS && $(awk 'END {print NR}' "$RESULTS") -gt 1 ]]; then
	printf 'existing controller results have no provenance signature; use a new CONTROLLER_ID\n' >&2
	exit 1
else
	printf '%s\n' "$inputs_digest" >"$CONTROLLER_INPUTS"
fi

RECOMMENDED_RATIO=$(jq -r '.search.recommended_capacity_ratio' "$MANIFEST")
{
	printf 'os=%s\n' "$(. /etc/os-release && printf '%s %s' "$NAME" "$VERSION_ID")"
	printf 'kernel=%s\n' "$(uname -r)"
	printf 'arch=amd64\n'
	printf 'timezone=%s\n' "$TZ"
	printf 'cpu_model=%s\n' "$(lscpu | awk -F: '$1 ~ /^Model name/ {sub(/^[[:space:]]+/, "", $2); print $2; exit}')"
	printf 'online_cpus=%s\n' "$(getconf _NPROCESSORS_ONLN)"
	printf 'memory_kib=%s\n' "$(awk '$1 == "MemTotal:" {print $2}' /proc/meminfo)"
	printf 'go=%s\n' "$(go version)"
	printf 'gcc=%s\n' "$(gcc --version | awk 'NR == 1')"
	printf 'sqlite=%s\n' "$(sqlite3 --version)"
	printf 'redis=%s\n' "$(redis-server --version)"
} >"$CONTROLLER_DIR/environment.txt"

if [[ -f $RESULTS ]]; then
	IFS= read -r existing_results_header <"$RESULTS"
	if [[ $existing_results_header != "$RESULTS_HEADER" ]]; then
		printf 'controller result header changed; use a new CONTROLLER_ID\n' >&2
		exit 1
	fi
else
	printf '%s\n' "$RESULTS_HEADER" >"$RESULTS"
fi

cell_id() {
	local cpu=$1
	jq -er --argjson cpu "$cpu" '
		[.cells[] | select(.resource.cpu == $cpu)] as $cells |
		if ($cells | length) == 1 then $cells[0].id
		else error("plan must contain exactly one cell for the selected CPU") end
	' "$PLAN"
}

result_path() {
	local run_id=$1 cell=$2
	printf '%s/runs/%s/cells/%s/result.json\n' "$ROOT" "$run_id" "$cell"
}

run_benchmark() {
	local run_id=$1 cell=$2 kind=$3 result status=0
	shift 3
	result=$(result_path "$run_id" "$cell")
	"$BENCH" resume \
		--manifest "$MANIFEST" \
		--plan "$PLAN" \
		--root "$ROOT" \
		--run-id "$run_id" \
		--keeper "$KEEPER" \
		--redis-port "$REDIS_PORT" \
		--app-port "$APP_PORT" \
		--cells "$cell" \
		--max-duration 30m \
		--dataset-validation "$DATASET_VALIDATION" \
		"$@" || status=$?
	if [[ ! -f $result ]]; then
		printf 'benchmark result is missing after %s: %s\n' "$kind" "$result" >&2
		return 1
	fi
	if jq -e '[.attempts[]? | select((.report.metrics.panic // false) or ((.error // "") != "" and ((.report.metrics.oom // false) | not)) or ((.report.publish_error // "") != ""))] | length > 0' "$result" >/dev/null; then
		printf 'benchmark infrastructure failure during %s: %s\n' "$kind" "$result" >&2
		return 1
	fi
	if ! jq -e '.status == "completed"' "$result" >/dev/null; then
		if [[ $kind != fixed ]] || ! jq -e '(.attempts | length) > 0 and ((.error // "") | test("^(hard|interactive) soak failed at [0-9]+ events/s:"))' "$result" >/dev/null; then
			printf 'benchmark cell failed during %s: %s\n' "$kind" "$result" >&2
			return 1
		fi
	fi
	if (( status != 0 )); then
		if [[ $kind != fixed ]] || ! jq -e '(.attempts | length) > 0 and ((.error // "") | test("^(hard|interactive) soak failed at [0-9]+ events/s:"))' "$result" >/dev/null; then
			printf 'benchmark command failed during %s with status %s: %s\n' "$kind" "$status" "$result" >&2
			return "$status"
		fi
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

midpoint_rate() {
	local cell=$1 low=$2 high=$3
	jq -r --arg cell "$cell" --argjson low "$low" --argjson high "$high" '
		[.cells[] | select(.id == $cell) | .rates_per_second[] | select(. > $low and . < $high)] as $rates |
		($rates | length) as $count |
		if $count == 0 then 0 else $rates[($count / 2 | floor)] end
	' "$PLAN"
}

next_hard_rate() {
	local cell=$1 low=$2 high=$3 midpoint candidate
	midpoint=$(((low + high) / 2))
	candidate=$((((midpoint + 12) / 25) * 25))
	# 低于 25 events/s 时，固定步进可能落到区间端点；改用 manifest 中的真实候选继续收敛。
	if (( candidate <= low || candidate >= high )); then
		candidate=$(midpoint_rate "$cell" "$low" "$high")
	fi
	printf '%s\n' "$candidate"
}

run_fixed() {
	local cpu=$1 label=$2 rate=$3 cell run_id
	cell=$(cell_id "$cpu")
	run_id="${CONTROLLER_ID}-${cpu}c-${label}-${rate}"
	# hard 模式仍完整采集 Dashboard；它允许预期的 Dashboard 边界失败保留为有效 ingestion 证据。
	run_benchmark "$run_id" "$cell" fixed \
		--fixed-rate "$rate" \
		--fixed-duration 5m \
		--fixed-pass hard
}

formal_cpu() {
	local cpu=$1 cell search_id search_result hard_rate dashboard_rate hard_result dashboard_result
	local pass interactive low_pass high_fail tolerance short_low hard_fail dashboard_fail minimum_rate
	local dashboard_low dashboard_high dashboard_next
	local ingestion_peak dashboard_peak cpu_percent hard_core_p95 hard_core_p99 dashboard_core_p95 dashboard_core_p99
	local analysis_p50 analysis_p95 analysis_p99 analysis_max analysis_samples analysis_errors analysis_status
	local slowest_path slowest_p99 durable_ratio backlog checkpoint identity shared recommended hard_ref dashboard_ref
	cell=$(cell_id "$cpu")
	minimum_rate=$(jq -er --arg cell "$cell" '.cells[] | select(.id == $cell) | .rates_per_second[0]' "$PLAN")
	search_id="${CONTROLLER_ID}-${cpu}c-search"
	printf '%sc search_start\n' "$cpu" >>"$CONTROLLER_DIR/controller.log"
	run_benchmark "$search_id" "$cell" search --search-duration 20s
	search_result=$LAST_RESULT
	if [[ ! -f $search_result ]]; then
		printf '%sc search_result_missing\n' "$cpu" >>"$CONTROLLER_DIR/controller.log"
		return 1
	fi
	hard_rate=$(jq -r '.capacity.hard_events_per_second // 0' "$search_result")
	dashboard_rate=$(jq -r '.capacity.interactive_events_per_second // 0' "$search_result")
	hard_fail=$(jq -r '.capacity.lowest_hard_failure_events_per_second // 0' "$search_result")
	dashboard_fail=$(jq -r '.capacity.lowest_interactive_failure_events_per_second // 0' "$search_result")
	printf '%sc calibrated ingestion=%s..<%s dashboard=%s..<%s\n' "$cpu" "$hard_rate" "$hard_fail" "$dashboard_rate" "$dashboard_fail" >>"$CONTROLLER_DIR/controller.log"

	hard_result=
	low_pass=0
	high_fail=0
	if (( hard_fail > hard_rate )); then
		high_fail=$hard_fail
	fi
	while (( hard_rate > 0 )); do
		run_fixed "$cpu" hard "$hard_rate"
		[[ -f $LAST_RESULT ]] || break
		pass=$(jq -r '.attempts[-1].report.evaluation.hard_pass // false' "$LAST_RESULT")
		printf '%sc hard rate=%s pass=%s\n' "$cpu" "$hard_rate" "$pass" >>"$CONTROLLER_DIR/controller.log"
		if [[ $pass == true ]]; then
			low_pass=$hard_rate
			hard_result=$LAST_RESULT
			(( high_fail > 0 )) || break
			if (( high_fail <= 25 )); then
				tolerance=0
			else
				tolerance=$((low_pass / 10))
				(( tolerance >= 25 )) || tolerance=25
			fi
			(( high_fail - low_pass > tolerance )) || break
			hard_rate=$(next_hard_rate "$cell" "$low_pass" "$high_fail")
			(( hard_rate > low_pass && hard_rate < high_fail )) || break
			continue
		fi
		high_fail=$hard_rate
		if (( hard_fail <= 0 || hard_rate < hard_fail )); then
			hard_fail=$hard_rate
		fi
		if (( dashboard_fail <= 0 || hard_rate < dashboard_fail )); then
			dashboard_fail=$hard_rate
		fi
		if (( low_pass > 0 )); then
			hard_rate=$(next_hard_rate "$cell" "$low_pass" "$high_fail")
			(( hard_rate > low_pass && hard_rate < high_fail )) || break
		else
			short_low=$(next_passing_rate "$search_result" "$hard_rate" hard)
			if (( short_low <= 0 )); then
				hard_rate=0
				continue
			fi
			hard_rate=$(next_hard_rate "$cell" "$short_low" "$high_fail")
			if (( hard_rate <= short_low || hard_rate >= high_fail )); then
				hard_rate=$short_low
			fi
		fi
	done
	hard_rate=$low_pass
	if [[ -z $hard_result ]]; then
		printf '%sc no_five_minute_ingestion_capacity\n' "$cpu" >>"$CONTROLLER_DIR/controller.log"
		return 1
	fi

	interactive=$(jq -r '.attempts[-1].report.evaluation.interactive_pass // false' "$hard_result")
	dashboard_result=$hard_result
	if [[ $interactive == true ]]; then
		dashboard_rate=$hard_rate
	else
		# hard_result 已是五分钟 Dashboard 失败上界；短测失败只用于选首个候选，不进入正式区间。
		dashboard_low=0
		dashboard_high=$hard_rate
		dashboard_fail=$hard_rate
		if (( dashboard_rate <= 0 || dashboard_rate >= hard_rate )); then
			dashboard_rate=$(next_passing_rate "$search_result" "$hard_rate" interactive)
		fi
		if (( dashboard_rate <= 0 && minimum_rate < hard_rate )); then
			dashboard_rate=$minimum_rate
		fi
		dashboard_result=
		while (( dashboard_rate > 0 )); do
			run_fixed "$cpu" dashboard "$dashboard_rate"
			[[ -f $LAST_RESULT ]] || break
			pass=$(jq -r '.attempts[-1].report.evaluation.interactive_pass // false' "$LAST_RESULT")
			printf '%sc dashboard rate=%s pass=%s\n' "$cpu" "$dashboard_rate" "$pass" >>"$CONTROLLER_DIR/controller.log"
			if [[ $pass == true ]]; then
				dashboard_low=$dashboard_rate
				dashboard_result=$LAST_RESULT
				dashboard_next=$(midpoint_rate "$cell" "$dashboard_low" "$dashboard_high")
				if (( dashboard_next <= 0 )); then
					break
				fi
				dashboard_rate=$dashboard_next
				continue
			fi
			dashboard_high=$dashboard_rate
			if (( dashboard_fail <= 0 || dashboard_rate < dashboard_fail )); then
				dashboard_fail=$dashboard_rate
			fi
			if (( dashboard_low > 0 )); then
				dashboard_rate=$(midpoint_rate "$cell" "$dashboard_low" "$dashboard_high")
			else
				short_low=$(next_passing_rate "$search_result" "$dashboard_rate" interactive)
				if (( short_low <= 0 && minimum_rate < dashboard_rate )); then
					dashboard_rate=$minimum_rate
				else
					dashboard_rate=$short_low
				fi
			fi
		done
		dashboard_rate=$dashboard_low
		if [[ -z $dashboard_result ]]; then
			dashboard_result=-
		fi
	fi
	# 短测可能受随机抖动影响；正式结果只报告严格高于五分钟通过点的失败上界。
	if (( hard_fail <= hard_rate )); then
		hard_fail=0
	fi
	if (( dashboard_fail <= dashboard_rate )); then
		dashboard_fail=0
	fi

	ingestion_peak=$(jq -r '(.attempts[-1].peak_resource.memory_peak_bytes // .attempts[-1].resource.memory_peak_bytes // 0) / 1048576' "$hard_result")
	cpu_percent=$(jq -r '.attempts[-1].resource.cpu_utilization_percent // 0' "$hard_result")
	hard_core_p95=$(jq -r '.attempts[-1].report.core_latency.p95_ms // 0' "$hard_result")
	hard_core_p99=$(jq -r '.attempts[-1].report.core_latency.p99_ms // 0' "$hard_result")
	durable_ratio=$(jq -r 'if .attempts[-1].report.metrics.offered_events > 0 then .attempts[-1].report.metrics.durable_events / .attempts[-1].report.metrics.offered_events else 0 end' "$hard_result")
	backlog=$(jq -r '.attempts[-1].report.metrics.backlog_end // 0' "$hard_result")
	checkpoint=$(jq -r '.attempts[-1].report.metrics.checkpoint_lag // 0' "$hard_result")
	identity=$(jq -r '.attempts[-1].report.metrics.identity_pending // 0' "$hard_result")
	shared=$(jq -r '.shared_driver // false' "$hard_result")
	recommended=$(awk -v rate="$dashboard_rate" -v ratio="$RECOMMENDED_RATIO" 'BEGIN { printf "%d", rate * ratio }')
	if [[ $dashboard_result == - ]]; then
		dashboard_peak=0
		dashboard_core_p95=0
		dashboard_core_p99=0
		analysis_p50=0
		analysis_p95=0
		analysis_p99=0
		analysis_max=0
		analysis_samples=0
		analysis_errors=0
		analysis_status=not_measured
		slowest_path=-
		slowest_p99=0
	else
		dashboard_peak=$(jq -r '(.attempts[-1].peak_resource.memory_peak_bytes // .attempts[-1].resource.memory_peak_bytes // 0) / 1048576' "$dashboard_result")
		dashboard_core_p95=$(jq -r '.attempts[-1].report.core_latency.p95_ms // 0' "$dashboard_result")
		dashboard_core_p99=$(jq -r '.attempts[-1].report.core_latency.p99_ms // 0' "$dashboard_result")
		analysis_p50=$(jq -r '.attempts[-1].report.analysis_latency.p50_ms // 0' "$dashboard_result")
		analysis_p95=$(jq -r '.attempts[-1].report.analysis_latency.p95_ms // 0' "$dashboard_result")
		analysis_p99=$(jq -r '.attempts[-1].report.analysis_latency.p99_ms // 0' "$dashboard_result")
		analysis_max=$(jq -r '.attempts[-1].report.analysis_latency.max_ms // 0' "$dashboard_result")
		analysis_samples=$(jq -r '.attempts[-1].report.analysis_latency.samples // 0' "$dashboard_result")
		analysis_errors=$(jq -r '.attempts[-1].report.metrics.analysis_latency_errors // 0' "$dashboard_result")
		analysis_status=$(jq -r 'if .attempts[-1].report.metrics.analysis_latency_requests == 0 then "not_measured" elif .attempts[-1].report.evaluation.analysis_latency_pass then "passed" else "failed" end' "$dashboard_result")
		slowest_path=$(jq -r '.attempts[-1].report.latency_by_path | to_entries | map(select(.key != "/api/v1/usage/analysis/latency?range=30d")) | if length == 0 then "-" else max_by(.value.p99_ms).key end' "$dashboard_result")
		slowest_p99=$(jq -r '.attempts[-1].report.latency_by_path | to_entries | map(select(.key != "/api/v1/usage/analysis/latency?range=30d")) | if length == 0 then 0 else max_by(.value.p99_ms).value.p99_ms end' "$dashboard_result")
	fi
	hard_ref=${hard_result#"$ROOT"/}
	if [[ $dashboard_result == - ]]; then
		dashboard_ref=-
	else
		dashboard_ref=${dashboard_result#"$ROOT"/}
	fi
	printf '%s\t%s\t%s\t%s\t%s\t%s\t%.3f\t%.3f\t%.3f\t%.3f\t%.3f\t%.3f\t%.3f\t%.3f\t%.3f\t%.3f\t%.3f\t%s\t%s\t%s\t%s\t%.3f\t%.6f\t%s\t%s\t%s\t%s\t%s\t%s\n' \
		"$cpu" "$hard_rate" "$hard_fail" "$dashboard_rate" "$dashboard_fail" "$recommended" "$cpu_percent" "$ingestion_peak" "$dashboard_peak" "$hard_core_p95" "$hard_core_p99" \
		"$dashboard_core_p95" "$dashboard_core_p99" "$analysis_p50" "$analysis_p95" "$analysis_p99" "$analysis_max" "$analysis_samples" "$analysis_errors" "$analysis_status" "$slowest_path" "$slowest_p99" "$durable_ratio" "$backlog" "$checkpoint" "$identity" \
		"$shared" "$hard_ref" "$dashboard_ref" >>"$RESULTS"
	printf '%sc complete ingestion=%s..<%s dashboard=%s..<%s recommended=%s ingestion_peak=%.2fMiB dashboard_peak=%.2fMiB\n' \
		"$cpu" "$hard_rate" "$hard_fail" "$dashboard_rate" "$dashboard_fail" "$recommended" "$ingestion_peak" "$dashboard_peak" >>"$CONTROLLER_DIR/controller.log"
}

for cpu in "${cpus[@]}"; do
	if awk -F '\t' -v cpu="$cpu" 'NR > 1 && $1 == cpu { found=1 } END { exit !found }' "$RESULTS"; then
		continue
	fi
	formal_cpu "$cpu"
done

for cpu in "${cpus[@]}"; do
	rows=$(awk -F '\t' -v cpu="$cpu" 'NR > 1 && $1 == cpu { count++ } END { print count+0 }' "$RESULTS")
	if [[ $rows != 1 ]]; then
		printf 'controller result completeness failed for %sc: rows=%s\n' "$cpu" "$rows" >&2
		exit 1
	fi
done

printf 'completed_at\t%s\n' "$(date --iso-8601=seconds)" >>"$CONTROLLER_DIR/controller.log"
{
	printf '# Capacity benchmark summary\n\n'
	printf '| CPU | Ingestion 5m pass | Lowest ingestion fail | Dashboard 5m pass | Lowest Dashboard fail | Recommended | Ingestion CPU | Ingestion peak memory | Dashboard peak memory | Dashboard core p95 | Dashboard core p99 | Analysis Latency samples / p99 / status | Shared driver |\n'
	printf '| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |\n'
	awk -F '\t' 'NR > 1 { printf "| %sC | %s events/s | %s events/s | %s events/s | %s events/s | %s events/s | %.1f%% | %.1f MiB | %.1f MiB | %.1fms | %.1fms | %s / %.1fms / %s | %s |\n", $1, $2, $3, $4, $5, $6, $7, $8, $9, $12, $13, $18, $16, $20, $27 }' "$RESULTS"
} >"$CONTROLLER_DIR/summary.md"
printf 'capacity results: %s\n' "$RESULTS"
printf 'capacity summary: %s\n' "$CONTROLLER_DIR/summary.md"
