#!/usr/bin/env bash
set -Eeuo pipefail

umask 077

readonly MINIMUM_AWS_CLI_VERSION="2.35.0"
readonly READY_ATTEMPTS=60
readonly READY_INTERVAL_SECONDS=5

payload_file=""

cleanup() {
  if [[ -n "${payload_file}" && -f "${payload_file}" ]]; then
    rm -f -- "${payload_file}"
  fi
}

trap cleanup EXIT
trap 'exit 1' HUP INT TERM

fail() {
  printf 'AgentCore MMDSv2 compatibility bridge failed: %s\n' "$1" >&2
  exit 1
}

require_aws_cli() {
  command -v aws >/dev/null 2>&1 || fail "AWS CLI v${MINIMUM_AWS_CLI_VERSION} or newer is required"

  local version_output
  local version
  version_output="$(aws --version 2>&1)" || fail "unable to determine the AWS CLI version"
  version="${version_output#aws-cli/}"
  version="${version%% *}"
  if [[ ! "${version}" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+) ]]; then
    fail "unable to parse the AWS CLI version"
  fi

  local major="${BASH_REMATCH[1]}"
  local minor="${BASH_REMATCH[2]}"
  if ((major < 2 || (major == 2 && minor < 35))); then
    fail "AWS CLI v${MINIMUM_AWS_CLI_VERSION} or newer is required"
  fi
}

validate_runtime_id() {
  local runtime_id="$1"
  if [[ ! "${runtime_id}" =~ ^[[:alpha:]][[:alnum:]_]{0,99}-[[:alnum:]]{10}$ ]]; then
    fail "the AgentCore runtime identifier is invalid"
  fi
}

validate_region() {
  local region="$1"
  if [[ ! "${region}" =~ ^[a-z]{2}-[a-z0-9-]+-[0-9]+$ ]]; then
    fail "the AWS region is invalid"
  fi
}

is_true() {
  [[ "$1" == [Tt][Rr][Uu][Ee] ]]
}

runtime_snapshot() {
  local runtime_id="$1"
  local region="$2"
  AWS_PAGER="" aws --no-cli-pager --region "${region}" \
    bedrock-agentcore-control get-agent-runtime \
    --agent-runtime-id "${runtime_id}" \
    --query '[agentRuntimeVersion,status,metadataConfiguration.requireMMDSV2]' \
    --output text
}

wait_for_ready_mmdsv2() {
  local runtime_id="$1"
  local region="$2"
  local attempt
  local snapshot
  local runtime_version
  local runtime_status
  local requires_mmdsv2

  for ((attempt = 1; attempt <= READY_ATTEMPTS; attempt++)); do
    snapshot="$(runtime_snapshot "${runtime_id}" "${region}")" || fail "unable to read the AgentCore runtime"
    read -r runtime_version runtime_status requires_mmdsv2 <<<"${snapshot}"

    case "${runtime_status}" in
      READY)
        # UpdateAgentRuntime is asynchronous. A short-lived READY snapshot can
        # still describe the previous version before the control plane exposes
        # UPDATING or the new MMDSv2-enabled READY version.
        if is_true "${requires_mmdsv2}"; then
          if [[ ! "${runtime_version}" =~ ^[1-9][0-9]{0,4}$ ]]; then
            fail "the AgentCore runtime version is invalid"
          fi
          printf '%s\n' "${runtime_version}"
          return 0
        fi
        ;;
      CREATE_FAILED | UPDATE_FAILED | DELETING)
        fail "the AgentCore runtime entered a terminal state"
        ;;
    esac

    sleep "${READY_INTERVAL_SECONDS}"
  done

  fail "the AgentCore runtime did not become MMDSv2-ready before the deadline"
}

ensure_mmdsv2() {
  local runtime_id="${AGENTCORE_RUNTIME_ID:-}"
  local region="${AWS_REGION:-}"
  local update_input="${AGENTCORE_UPDATE_INPUT:-}"
  local snapshot
  local runtime_version
  local runtime_status
  local requires_mmdsv2

  validate_runtime_id "${runtime_id}"
  validate_region "${region}"
  [[ -n "${update_input}" ]] || fail "the complete AgentCore update input is required"
  [[ "${update_input}" == *'"metadataConfiguration":{"requireMMDSV2":true}'* ]] || \
    fail "the AgentCore update input must require MMDSv2"

  snapshot="$(runtime_snapshot "${runtime_id}" "${region}")" || fail "unable to read the AgentCore runtime"
  read -r runtime_version runtime_status requires_mmdsv2 <<<"${snapshot}"
  if is_true "${requires_mmdsv2}"; then
    wait_for_ready_mmdsv2 "${runtime_id}" "${region}" >/dev/null
    return 0
  fi
  case "${runtime_status}" in
    CREATE_FAILED | UPDATE_FAILED | DELETING)
      fail "the AgentCore runtime cannot be updated from its current state"
      ;;
  esac

  payload_file="$(mktemp "${TMPDIR:-/tmp}/agentcore-mmdsv2.XXXXXX")"
  printf '%s' "${update_input}" >"${payload_file}"

  AWS_PAGER="" aws --no-cli-pager --region "${region}" \
    bedrock-agentcore-control update-agent-runtime \
    --cli-input-json "file://${payload_file}" \
    --output json >/dev/null

  wait_for_ready_mmdsv2 "${runtime_id}" "${region}" >/dev/null
}

read_version() {
  local runtime_id="${1:-}"
  local region="${2:-}"
  local bridge_id="${3:-}"
  local runtime_version

  validate_runtime_id "${runtime_id}"
  validate_region "${region}"
  [[ -n "${bridge_id}" ]] || fail "the Terraform MMDSv2 bridge identity is required"

  runtime_version="$(wait_for_ready_mmdsv2 "${runtime_id}" "${region}")"
  printf '{"agent_runtime_version":"%s"}\n' "${runtime_version}"
}

main() {
  require_aws_cli
  case "${1:-}" in
    ensure)
      [[ "$#" -eq 1 ]] || fail "ensure does not accept positional arguments"
      ensure_mmdsv2
      ;;
    read-version)
      [[ "$#" -eq 4 ]] || fail "read-version requires runtime ID, region, and bridge identity"
      read_version "$2" "$3" "$4"
      ;;
    *)
      fail "expected ensure or read-version mode"
      ;;
  esac
}

main "$@"
