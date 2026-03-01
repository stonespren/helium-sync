#!/usr/bin/env bash
#
# helium-sync setup script
# Idempotent setup for helium-sync
#
set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Defaults
CONFIG_DIR="${HOME}/.config/helium-sync"
STATE_DIR="${HOME}/.local/state/helium-sync"
CONFIG_FILE="${CONFIG_DIR}/config.json"
DEVICE_ID_FILE="${CONFIG_DIR}/device_id"
SYSTEMD_USER_DIR="${HOME}/.config/systemd/user"
BINARY_PATH="/usr/bin/helium-sync"
DEFAULT_HELIUM_DIR="${HOME}/.config/net.imput.helium"
DEFAULT_SYNC_INTERVAL=15
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "${SCRIPT_DIR}")"

# State
NON_INTERACTIVE=false
CONFIG_OVERRIDE=""
UNINSTALL=false

usage() {
    cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Options:
  --non-interactive    Run without prompts (requires --config or existing config)
  --config <file>      Use specified config file for non-interactive setup
  --uninstall          Remove helium-sync configuration and systemd units
  -h, --help           Show this help message
EOF
}

log_info() { echo -e "${GREEN}[INFO]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
log_step() { echo -e "${BLUE}[STEP]${NC} $*"; }

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --non-interactive)
            NON_INTERACTIVE=true
            shift
            ;;
        --config)
            CONFIG_OVERRIDE="$2"
            shift 2
            ;;
        --uninstall)
            UNINSTALL=true
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

prompt() {
    local var_name="$1"
    local prompt_text="$2"
    local default_val="${3:-}"

    if [[ "${NON_INTERACTIVE}" == "true" ]]; then
        if [[ -n "${default_val}" ]]; then
            eval "${var_name}='${default_val}'"
        else
            log_error "Non-interactive mode requires a default for: ${prompt_text}"
            exit 1
        fi
        return
    fi

    if [[ -n "${default_val}" ]]; then
        read -rp "$(echo -e "${BLUE}?${NC}") ${prompt_text} [${default_val}]: " input
        eval "${var_name}='${input:-${default_val}}'"
    else
        read -rp "$(echo -e "${BLUE}?${NC}") ${prompt_text}: " input
        eval "${var_name}='${input}'"
    fi
}

# Interactive selector with arrow keys and j/k navigation
# Usage: interactive_select RESULT_VAR "prompt" option1 option2 ...
interactive_select() {
    local _result_var="$1"
    local _prompt="$2"
    shift 2
    local -a _options=("$@")
    local _count=${#_options[@]}
    local _selected=0

    if [[ ${_count} -eq 0 ]]; then
        log_error "No options provided to selector."
        return 1
    fi

    # If only one option, auto-select
    if [[ ${_count} -eq 1 ]]; then
        eval "${_result_var}=0"
        return 0
    fi

    echo -e "${BLUE}?${NC} ${_prompt}"
    echo -e "  ${YELLOW}Use ↑/↓ or k/j to move, Enter to select${NC}"
    echo

    # Hide cursor
    tput civis 2>/dev/null || true

    # Render function
    _render_options() {
        # Move up to overwrite previous render (skip on first render)
        if [[ ${1:-0} -eq 1 ]]; then
            for (( i=0; i<_count; i++ )); do
                tput cuu1 2>/dev/null
                tput el 2>/dev/null
            done
        fi
        for (( i=0; i<_count; i++ )); do
            if [[ ${i} -eq ${_selected} ]]; then
                echo -e "  ${GREEN}❯ ${_options[${i}]}${NC}"
            else
                echo -e "    ${_options[${i}]}"
            fi
        done
    }

    _render_options 0

    while true; do
        # Read a single character (raw mode)
        IFS= read -rsn1 key
        case "${key}" in
            $'\x1b')  # Escape sequence (arrow keys)
                read -rsn2 -t 0.1 seq || true
                case "${seq}" in
                    '[A') # Up arrow
                        if (( _selected > 0 )); then _selected=$((_selected - 1)); fi
                        ;;
                    '[B') # Down arrow
                        if (( _selected < _count - 1 )); then _selected=$((_selected + 1)); fi
                        ;;
                esac
                ;;
            'k'|'K')  # Vim up
                if (( _selected > 0 )); then _selected=$((_selected - 1)); fi
                ;;
            'j'|'J')  # Vim down
                if (( _selected < _count - 1 )); then _selected=$((_selected + 1)); fi
                ;;
            '')  # Enter
                break
                ;;
        esac
        _render_options 1
    done

    # Show cursor
    tput cnorm 2>/dev/null || true
    echo

    eval "${_result_var}=${_selected}"
}

confirm() {
    local prompt_text="$1"
    local default="${2:-n}"

    if [[ "${NON_INTERACTIVE}" == "true" ]]; then
        return 0
    fi

    local yn
    if [[ "${default}" == "y" ]]; then
        read -rp "$(echo -e "${BLUE}?${NC}") ${prompt_text} [Y/n]: " yn
        yn="${yn:-y}"
    else
        read -rp "$(echo -e "${BLUE}?${NC}") ${prompt_text} [y/N]: " yn
        yn="${yn:-n}"
    fi

    case "${yn}" in
        [Yy]*) return 0 ;;
        *) return 1 ;;
    esac
}

# ============================================================
# Uninstall
# ============================================================
do_uninstall() {
    log_step "Uninstalling helium-sync..."

    # Stop and disable timer
    systemctl --user stop helium-sync.timer 2>/dev/null || true
    systemctl --user disable helium-sync.timer 2>/dev/null || true
    systemctl --user stop helium-sync.service 2>/dev/null || true
    systemctl --user disable helium-sync.service 2>/dev/null || true

    # Remove systemd units
    rm -f "${SYSTEMD_USER_DIR}/helium-sync.service"
    rm -f "${SYSTEMD_USER_DIR}/helium-sync.timer"
    systemctl --user daemon-reload 2>/dev/null || true

    # Remove config
    if confirm "Remove configuration directory (${CONFIG_DIR})?" "n"; then
        rm -rf "${CONFIG_DIR}"
        log_info "Config directory removed."
    fi

    # Remove state
    if confirm "Remove state/log directory (${STATE_DIR})?" "n"; then
        rm -rf "${STATE_DIR}"
        log_info "State directory removed."
    fi

    # Remove binary
    if [[ -f "${BINARY_PATH}" ]]; then
        if confirm "Remove binary (${BINARY_PATH})?" "n"; then
            sudo rm -f "${BINARY_PATH}"
            log_info "Binary removed."
        fi
    fi

    log_info "Uninstall complete."
    exit 0
}

if [[ "${UNINSTALL}" == "true" ]]; then
    do_uninstall
fi

# ============================================================
# Load config override if provided
# ============================================================
if [[ -n "${CONFIG_OVERRIDE}" ]]; then
    if [[ ! -f "${CONFIG_OVERRIDE}" ]]; then
        log_error "Config file not found: ${CONFIG_OVERRIDE}"
        exit 1
    fi
    log_info "Loading config from: ${CONFIG_OVERRIDE}"
    # Read values from JSON config override
    _cfg_bucket=$(jq -r '.s3_bucket // empty' "${CONFIG_OVERRIDE}" 2>/dev/null || true)
    _cfg_region=$(jq -r '.s3_region // empty' "${CONFIG_OVERRIDE}" 2>/dev/null || true)
    _cfg_profile=$(jq -r '.aws_profile // empty' "${CONFIG_OVERRIDE}" 2>/dev/null || true)
    _cfg_helium=$(jq -r '.helium_dir // empty' "${CONFIG_OVERRIDE}" 2>/dev/null || true)
    _cfg_interval=$(jq -r '.sync_interval_minutes // empty' "${CONFIG_OVERRIDE}" 2>/dev/null || true)
    _cfg_sse=$(jq -r '.sse_s3 // empty' "${CONFIG_OVERRIDE}" 2>/dev/null || true)
    [[ -n "${_cfg_bucket}" ]] && S3_BUCKET="${_cfg_bucket}"
    [[ -n "${_cfg_region}" ]] && AWS_REGION="${_cfg_region}"
    [[ -n "${_cfg_profile}" ]] && AWS_PROFILE_NAME="${_cfg_profile}"
    [[ -n "${_cfg_helium}" ]] && HELIUM_DIR="${_cfg_helium}"
    [[ -n "${_cfg_interval}" ]] && SYNC_INTERVAL="${_cfg_interval}"
    [[ -n "${_cfg_sse}" ]] && SSE_ENABLED="${_cfg_sse}"
fi

# ============================================================
# Step 1: Check dependencies
# ============================================================
log_step "Checking dependencies..."

check_cmd() {
    if ! command -v "$1" &>/dev/null; then
        return 1
    fi
    return 0
}

MISSING_DEPS=()
for dep in aws jq; do
    if ! check_cmd "${dep}"; then
        MISSING_DEPS+=("${dep}")
    fi
done

if [[ ${#MISSING_DEPS[@]} -gt 0 ]]; then
    log_warn "Missing dependencies: ${MISSING_DEPS[*]}"
    if check_cmd pacman; then
        PKG_MAP=([aws]="aws-cli-v2" [jq]="jq")
        PKGS=()
        for dep in "${MISSING_DEPS[@]}"; do
            PKGS+=("${PKG_MAP[${dep}]:-${dep}}")
        done
        if confirm "Install missing packages (${PKGS[*]}) via pacman?" "y"; then
            sudo pacman -S --needed --noconfirm "${PKGS[@]}"
        else
            log_error "Required dependencies missing. Aborting."
            exit 1
        fi
    else
        log_error "Please install: ${MISSING_DEPS[*]}"
        exit 1
    fi
fi

log_info "All dependencies satisfied."

# ============================================================
# Step 2-3: AWS credentials
# ============================================================
log_step "Configuring AWS credentials..."

# Preserve values loaded from --config override
_PRELOADED_AWS_PROFILE="${AWS_PROFILE_NAME:-}"
_PRELOADED_AWS_REGION="${AWS_REGION:-}"

# Helper: prompt for new AWS credentials and save them
_add_new_aws_credentials() {
    prompt AWS_ACCESS_KEY "AWS Access Key ID"
    if [[ "${NON_INTERACTIVE}" == "false" ]]; then
        read -rsp "$(echo -e "${BLUE}?${NC}") AWS Secret Access Key: " AWS_SECRET_KEY
        echo
    else
        log_error "Cannot prompt for secret key in non-interactive mode."
        exit 1
    fi
    prompt AWS_PROFILE_NAME "AWS profile name" "default"

    mkdir -p "${HOME}/.aws"
    chmod 700 "${HOME}/.aws"

    # Append profile to credentials
    cat >> "${HOME}/.aws/credentials" <<CRED

[${AWS_PROFILE_NAME}]
aws_access_key_id = ${AWS_ACCESS_KEY}
aws_secret_access_key = ${AWS_SECRET_KEY}
CRED
    chmod 600 "${HOME}/.aws/credentials"
    log_info "AWS credentials saved."
}

if [[ -n "${_PRELOADED_AWS_PROFILE}" ]]; then
    AWS_PROFILE_NAME="${_PRELOADED_AWS_PROFILE}"
    log_info "Using AWS profile from config: ${AWS_PROFILE_NAME}"
else
    # Gather existing profiles (if any)
    PROFILE_ARRAY=()
    if [[ -f "${HOME}/.aws/credentials" ]]; then
        PROFILES=$(aws configure list-profiles 2>/dev/null || echo "")
        while IFS= read -r line; do
            [[ -n "${line}" ]] && PROFILE_ARRAY+=("${line}")
        done <<< "${PROFILES}"
    fi

    if [[ "${NON_INTERACTIVE}" == "true" ]]; then
        # Non-interactive: use first existing profile or fail
        if [[ ${#PROFILE_ARRAY[@]} -gt 0 ]]; then
            AWS_PROFILE_NAME="${PROFILE_ARRAY[0]}"
        else
            log_error "No AWS credentials found and running non-interactive. Aborting."
            exit 1
        fi
    else
        # Build menu options: existing profiles + "Add new credentials"
        MENU_OPTIONS=()
        for p in "${PROFILE_ARRAY[@]}"; do
            MENU_OPTIONS+=("${p}")
        done
        MENU_OPTIONS+=("Add new credentials")

        interactive_select CRED_IDX "Select AWS credentials" "${MENU_OPTIONS[@]}"

        if [[ ${CRED_IDX} -eq $((${#MENU_OPTIONS[@]} - 1)) ]]; then
            # User chose "Add new credentials"
            _add_new_aws_credentials
        else
            AWS_PROFILE_NAME="${PROFILE_ARRAY[${CRED_IDX}]}"
        fi
    fi
    log_info "Using AWS profile: ${AWS_PROFILE_NAME}"
fi

# ============================================================
# Step 4: Region
# ============================================================
log_step "Configuring AWS region..."

if [[ -n "${_PRELOADED_AWS_REGION}" ]]; then
    AWS_REGION="${_PRELOADED_AWS_REGION}"
    log_info "Using AWS region from config: ${AWS_REGION}"
else
    DEFAULT_REGION=$(aws configure get region --profile "${AWS_PROFILE_NAME}" 2>/dev/null || echo "us-east-1")
    prompt AWS_REGION "AWS region" "${DEFAULT_REGION}"
fi

export AWS_PROFILE="${AWS_PROFILE_NAME}"
export AWS_DEFAULT_REGION="${AWS_REGION}"

# ============================================================
# Step 5-6: S3 bucket
# ============================================================
log_step "Configuring S3 bucket..."

if [[ -n "${S3_BUCKET:-}" ]]; then
    log_info "Using S3 bucket from config: ${S3_BUCKET}"
else
    EXISTING_BUCKETS=$(aws s3 ls --profile "${AWS_PROFILE_NAME}" 2>/dev/null | awk '{print $3}' || true)
    S3_BUCKET=""

    if [[ -n "${EXISTING_BUCKETS}" ]] && [[ "${NON_INTERACTIVE}" == "false" ]]; then
    echo "Available S3 buckets:"
    BUCKET_ARRAY=()
    while IFS= read -r line; do
        [[ -n "${line}" ]] && BUCKET_ARRAY+=("${line}")
    done <<< "${EXISTING_BUCKETS}"

    for i in "${!BUCKET_ARRAY[@]}"; do
        echo "  $((i + 1))) ${BUCKET_ARRAY[${i}]}"
    done
    echo "  $((${#BUCKET_ARRAY[@]} + 1))) Create new bucket"

    prompt BUCKET_IDX "Select bucket number"
    IDX=$((BUCKET_IDX - 1))

    if [[ ${IDX} -ge 0 && ${IDX} -lt ${#BUCKET_ARRAY[@]} ]]; then
        S3_BUCKET="${BUCKET_ARRAY[${IDX}]}"
    else
        prompt S3_BUCKET "New bucket name"
        log_info "Creating bucket: ${S3_BUCKET}..."
        if [[ "${AWS_REGION}" == "us-east-1" ]]; then
            aws s3 mb "s3://${S3_BUCKET}" --profile "${AWS_PROFILE_NAME}" --region "${AWS_REGION}"
        else
            aws s3 mb "s3://${S3_BUCKET}" --profile "${AWS_PROFILE_NAME}" --region "${AWS_REGION}" \
                --create-bucket-configuration LocationConstraint="${AWS_REGION}"
        fi
    fi
else
    prompt S3_BUCKET "S3 bucket name"
    # Try to create if it doesn't exist
    if ! aws s3 ls "s3://${S3_BUCKET}" --profile "${AWS_PROFILE_NAME}" --region "${AWS_REGION}" &>/dev/null; then
        if confirm "Bucket '${S3_BUCKET}' doesn't exist. Create it?" "y"; then
            if [[ "${AWS_REGION}" == "us-east-1" ]]; then
                aws s3 mb "s3://${S3_BUCKET}" --profile "${AWS_PROFILE_NAME}" --region "${AWS_REGION}"
            else
                aws s3 mb "s3://${S3_BUCKET}" --profile "${AWS_PROFILE_NAME}" --region "${AWS_REGION}" \
                    --create-bucket-configuration LocationConstraint="${AWS_REGION}"
            fi
        fi
    fi
fi
fi # end S3_BUCKET check

# ============================================================
log_step "Validating S3 access..."

if aws s3 ls "s3://${S3_BUCKET}" --profile "${AWS_PROFILE_NAME}" --region "${AWS_REGION}" &>/dev/null; then
    log_info "S3 access validated."
else
    log_error "Cannot access bucket: ${S3_BUCKET}"
    exit 1
fi

# ============================================================
# Step 8: Helium config path
# ============================================================
log_step "Configuring Helium browser path..."

if [[ -n "${HELIUM_DIR:-}" ]]; then
    log_info "Using Helium dir from config: ${HELIUM_DIR}"
else
    prompt HELIUM_DIR "Helium browser config path" "${DEFAULT_HELIUM_DIR}"
fi

if [[ ! -d "${HELIUM_DIR}" ]]; then
    log_warn "Directory not found: ${HELIUM_DIR}"
    if ! confirm "Continue anyway?" "n"; then
        exit 1
    fi
fi

# ============================================================
# Step 9: Detect profiles
# ============================================================
log_step "Detecting Helium profiles..."

DETECTED_PROFILES=()
if [[ -f "${HELIUM_DIR}/Local State" ]]; then
    # Parse Local State for profiles
    PROFILE_NAMES=$(jq -r '.profile.info_cache // {} | keys[]' "${HELIUM_DIR}/Local State" 2>/dev/null || true)
    while IFS= read -r name; do
        if [[ -n "${name}" && -d "${HELIUM_DIR}/${name}" ]]; then
            DETECTED_PROFILES+=("${name}")
        fi
    done <<< "${PROFILE_NAMES}"
fi

# Fallback: check common directories
if [[ ${#DETECTED_PROFILES[@]} -eq 0 ]]; then
    for name in "Default" "Profile 1" "Profile 2" "Profile 3"; do
        if [[ -d "${HELIUM_DIR}/${name}" ]]; then
            DETECTED_PROFILES+=("${name}")
        fi
    done
fi

if [[ ${#DETECTED_PROFILES[@]} -gt 0 ]]; then
    log_info "Found profiles: ${DETECTED_PROFILES[*]}"
else
    log_warn "No profiles detected. They will be synced when created."
fi

# ============================================================
# Step 10: Sync interval
# ============================================================
log_step "Configuring sync interval..."

if [[ -n "${SYNC_INTERVAL:-}" ]]; then
    log_info "Using sync interval from config: ${SYNC_INTERVAL} minutes"
else
    prompt SYNC_INTERVAL "Sync interval in minutes" "${DEFAULT_SYNC_INTERVAL}"
fi

# Validate numeric
if ! [[ "${SYNC_INTERVAL}" =~ ^[0-9]+$ ]] || [[ "${SYNC_INTERVAL}" -lt 1 ]]; then
    log_warn "Invalid interval, using default: ${DEFAULT_SYNC_INTERVAL}"
    SYNC_INTERVAL="${DEFAULT_SYNC_INTERVAL}"
fi

# ============================================================
# SSE-S3 support
# ============================================================
if [[ -n "${SSE_ENABLED:-}" ]]; then
    log_info "Using SSE setting from config: ${SSE_ENABLED}"
else
    SSE_ENABLED=false
    if confirm "Enable server-side encryption (SSE-S3)?" "y"; then
        SSE_ENABLED=true
    fi
fi

# ============================================================
# Write config
# ============================================================
log_step "Writing configuration..."

mkdir -p "${CONFIG_DIR}" "${STATE_DIR}"
chmod 700 "${CONFIG_DIR}" "${STATE_DIR}"

cat > "${CONFIG_FILE}" <<EOF
{
  "helium_dir": "${HELIUM_DIR}",
  "s3_bucket": "${S3_BUCKET}",
  "s3_region": "${AWS_REGION}",
  "aws_profile": "${AWS_PROFILE_NAME}",
  "sync_interval_minutes": ${SYNC_INTERVAL},
  "log_level": "info",
  "sse_s3": ${SSE_ENABLED}
}
EOF
chmod 600 "${CONFIG_FILE}"
log_info "Config written to ${CONFIG_FILE}"

# Generate device ID if not exists
if [[ ! -f "${DEVICE_ID_FILE}" ]]; then
    DEVICE_ID=$(cat /proc/sys/kernel/random/uuid 2>/dev/null || uuidgen 2>/dev/null || python3 -c "import uuid; print(uuid.uuid4())")
    echo -n "${DEVICE_ID}" > "${DEVICE_ID_FILE}"
    chmod 600 "${DEVICE_ID_FILE}"
    log_info "Device ID generated: ${DEVICE_ID}"
else
    log_info "Device ID exists: $(cat "${DEVICE_ID_FILE}")"
fi

# ============================================================
# Build binary if not installed
# ============================================================
if [[ ! -f "${BINARY_PATH}" ]]; then
    log_step "Building helium-sync binary..."
    if check_cmd go; then
        pushd "${PROJECT_DIR}" > /dev/null
        CGO_ENABLED=0 go build -o helium-sync ./cmd/helium-sync/
        sudo install -Dm755 helium-sync "${BINARY_PATH}"
        rm -f helium-sync
        popd > /dev/null
        log_info "Binary installed to ${BINARY_PATH}"
    else
        log_warn "Go not found. Please build and install the binary manually."
    fi
fi

# ============================================================
# Step 11: Create and enable systemd timer
# ============================================================
log_step "Setting up systemd timer..."

mkdir -p "${SYSTEMD_USER_DIR}"

# Install service
cat > "${SYSTEMD_USER_DIR}/helium-sync.service" <<EOF
[Unit]
Description=Helium Browser Profile Sync
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=${BINARY_PATH} sync --background
Nice=19
IOSchedulingClass=idle
MemoryMax=100M
CPUQuota=25%
Environment=AWS_PROFILE=${AWS_PROFILE_NAME}
Environment=AWS_DEFAULT_REGION=${AWS_REGION}

[Install]
WantedBy=default.target
EOF

# Install timer
cat > "${SYSTEMD_USER_DIR}/helium-sync.timer" <<EOF
[Unit]
Description=Helium Browser Profile Sync Timer
After=network-online.target

[Timer]
OnBootSec=2min
OnUnitActiveSec=${SYNC_INTERVAL}min
Persistent=true
RandomizedDelaySec=30

[Install]
WantedBy=timers.target
EOF

systemctl --user daemon-reload
systemctl --user enable helium-sync.timer
systemctl --user start helium-sync.timer

log_info "Systemd timer enabled (every ${SYNC_INTERVAL} minutes)."

# ============================================================
# Step 12: Offer initial sync
# ============================================================
if [[ ${#DETECTED_PROFILES[@]} -gt 0 ]]; then
    if confirm "Run initial sync now?" "y"; then
        log_step "Running initial sync..."
        "${BINARY_PATH}" sync || log_warn "Initial sync had issues. Check logs with: helium-sync logs"
    fi
fi

# ============================================================
# Done
# ============================================================
echo
log_info "Setup complete!"
echo
echo "Commands:"
echo "  helium-sync sync       - Manual sync"
echo "  helium-sync status     - Show sync status"
echo "  helium-sync restore    - Restore from S3"
echo "  helium-sync config     - Show/edit configuration"
echo "  helium-sync logs       - View recent logs"
echo "  helium-sync --disable  - Disable automatic sync"
echo "  helium-sync --enable   - Enable automatic sync"
