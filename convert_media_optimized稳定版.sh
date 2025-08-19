#!/opt/homebrew/bin/bash
set -eo pipefail
# Ensure Bash version is 4 or higher for associative arrays and other features.
if (( BASH_VERSINFO[0] < 4 )); then
    printf "⚠️ \033[1;31m错误:\033[0m 此脚本需要 Bash 版本 4 或更高。\n"
    printf "在 macOS 上，通过 Homebrew 安装更新的 Bash：\033[1;34mbrew install bash\033[0m\n"
    printf "然后使用新 Bash 运行脚本，例如：\033[1;32m/opt/homebrew/bin/bash %s\033[0m\n" "$0"
    exit 1
fi

# --- Script Configuration & Globals ---
VERSION="12.2.0-RELIABLE"
MODE="" TARGET_DIR=""
LOG_DIR="" CONVERSION_LOG="" REPORT_FILE=""
# macOS M-Chip Optimization: Default jobs to 100% of performance cores, or 75% of total cores as a fallback.
if [[ "$(uname)" == "Darwin" ]]; then
    CONCURRENT_JOBS=$(sysctl -n hw.perflevel0.physicalcpu 2>/dev/null || sysctl -n hw.activecpu 2>/dev/null | awk '{print int($1*0.75)}' || echo 4)
else
    CONCURRENT_JOBS=$(nproc 2>/dev/null | awk '{print int($1*0.75)}' || echo 4)
fi
if (( CONCURRENT_JOBS == 0 )); then CONCURRENT_JOBS=4; fi

ENABLE_HW_ACCEL=1 ENABLE_BACKUPS=1 SORT_ORDER="size"
TEMP_DIR="" RESULTS_DIR="" INDEX_FILE="" MEMORY_WATCHDOG_PID="" THROTTLE_FILE=""
RUN_STARTED=0
ALL_FILES_COUNT=0 SUCCESS_COUNT=0 FAIL_COUNT=0 SKIP_COUNT=0 RESUMED_COUNT=0
SIZE_REDUCED=0 SIZE_INCREASED=0 SIZE_UNCHANGED=0
TOTAL_SAVED=0 TOTAL_SIZE_INCREASED_SUM=0 SMART_DECISIONS_COUNT=0 LOSSLESS_WINS_COUNT=0 QUALITY_ANALYSIS_COUNT=0
declare -a FAILED_FILES=() QUALITY_STATS=() LOG_BUFFER=()
MAX_BACKUP_FILES=200 MAX_LOG_SIZE=20971520
MAX_VIDEO_TASK_SECONDS=${MAX_VIDEO_TASK_SECONDS:-1800}

# --- Terminal Colors & Styles ---
BOLD=$'\033[1m' DIM=$'\033[2m' RESET=$'\033[0m' CLEAR_LINE=$'\r\033[K'
COLOR_BLUE=$'\033[38;5;39m' COLOR_CYAN=$'\033[38;5;45m' COLOR_GREEN=$'\033[38;5;47m' COLOR_YELLOW=$'\033[38;5;220m'
COLOR_ORANGE=$'\033[38;5;202m' COLOR_RED=$'\033[38;5;196m' COLOR_VIOLET=$'\033[38;5;129m'
COLOR_SUCCESS=$COLOR_GREEN COLOR_INFO=$COLOR_BLUE COLOR_WARN=$COLOR_YELLOW COLOR_ERROR=$COLOR_RED
COLOR_PROMPT=$COLOR_CYAN COLOR_HIGHLIGHT=$COLOR_VIOLET COLOR_STATS=$COLOR_ORANGE COLOR_SUBTLE=$'\033[38;5;242m'

# Disable colors if not a TTY
if [[ ! -t 1 ]]; then
    for var in BOLD DIM RESET CLEAR_LINE COLOR_BLUE COLOR_CYAN COLOR_GREEN COLOR_YELLOW COLOR_ORANGE COLOR_RED COLOR_VIOLET COLOR_SUCCESS COLOR_INFO COLOR_WARN COLOR_ERROR COLOR_PROMPT COLOR_HIGHLIGHT COLOR_STATS COLOR_SUBTLE; do
        declare "$var"=""
    done
fi

# --- Core Utility & Cleanup Functions ---
ffmpeg_quiet() { ffmpeg -hide_banner -v error "$@"; }

cleanup_handler() {
    local exit_status=$?
    # Only show interrupt message if the main processing has started
    if [[ $RUN_STARTED -eq 1 ]]; then
        # Check if the script was interrupted by the user (e.g., Ctrl+C)
        if [[ "$exit_status" -ne 0 && "$exit_status" -ne 130 ]]; then # 130 is exit code for Ctrl+C
            printf "\n${CLEAR_LINE}${COLOR_WARN}⚠️ 脚本因错误中断(代码: $exit_status)，正在进行最后的清理工作...${RESET}\n"
        elif [[ "$exit_status" -eq 130 ]]; then
             printf "\n${CLEAR_LINE}${COLOR_WARN}⚠️ 用户中断，正在清理...${RESET}\n"
        fi
    fi
    # Stop watchdog and any background jobs
    if [[ -n "$MEMORY_WATCHDOG_PID" ]] && kill -0 "$MEMORY_WATCHDOG_PID" 2>/dev/null; then kill "$MEMORY_WATCHDOG_PID" 2>/dev/null || true; fi
    local pids=$(jobs -p 2>/dev/null || echo "")
    if [[ -n "$pids" ]]; then
        echo "$pids" | xargs -r kill -TERM 2>/dev/null || true
        sleep 0.5
        pids=$(jobs -p 2>/dev/null || echo "")
        [[ -n "$pids" ]] && echo "$pids" | xargs -r kill -KILL 2>/dev/null || true
    fi
    flush_log_buffer
    [[ -n "${TEMP_DIR:-}" && -d "${TEMP_DIR:-}" ]] && rm -rf "$TEMP_DIR" 2>/dev/null || true
    
    if [[ $RUN_STARTED -eq 1 && "$exit_status" -ne 0 ]]; then printf "${COLOR_SUCCESS}✅ 清理完成。${RESET}\n"; fi
}
trap cleanup_handler EXIT INT TERM


# --- Logging & File Info ---
init_logging() {
    local timestamp=$(date +"%Y%m%d_%H%M%S")
    LOG_DIR="$TARGET_DIR" # Keep logs in the target directory for easy access
    CONVERSION_LOG="$LOG_DIR/${MODE}_conversion_${timestamp}.txt"
    REPORT_FILE="$LOG_DIR/${MODE}_conversion_report_${timestamp}.txt"
    {
        echo "媒体转换日志 - $(date)"
        echo "版本: $VERSION"
        echo "模式: $MODE"
        echo "目标目录: $TARGET_DIR"
        echo "并发数: $CONCURRENT_JOBS"
        echo "硬件加速: $([ $ENABLE_HW_ACCEL -eq 1 ] && echo "启用" || echo "禁用")"
        echo "====================================="
    } > "$CONVERSION_LOG"
}

get_file_size() { [[ -f "$1" ]] && stat -f%z "$1" 2>/dev/null || echo "0"; }

flush_log_buffer() {
    if [[ ${#LOG_BUFFER[@]} -gt 0 ]]; then
        printf "%s\n" "${LOG_BUFFER[@]}" >> "$CONVERSION_LOG" 2>/dev/null || true
        LOG_BUFFER=()
    fi
}

log_message() {
    local level="$1" message="$2" timestamp=$(date "+%Y-%m-%d %H:%M:%S")
    LOG_BUFFER+=("[$timestamp] [$level] $message")
    if [[ ${#LOG_BUFFER[@]} -ge 50 ]]; then flush_log_buffer; fi
}

get_mime_type() { 
    local file="$1"
    local mime
    mime=$(file --mime-type -b "$file" 2>/dev/null || echo "unknown")
    if [[ "$mime" == "application/octet-stream" ]]; then
        local ext="${file##*.}"
        case "${ext,,}" in # case-insensitive extension check
            webm|mp4|avi|mov|mkv|flv|wmv|m4v|3gp|ogv|ts|mts|m2ts) mime="video/$ext";;
            jpg|jpeg|png|gif|bmp|tiff|webp|heic|heif|jxl|avif) mime="image/$ext";;
        esac
    fi
    echo "$mime"
}

# --- Intelligent Analysis & Pre-computation Functions ---
is_animated() {
    local mime=$(get_mime_type "$1")
    case "$mime" in
        image/gif|image/webp|image/avif)
            local frames=$(ffprobe -v quiet -select_streams v:0 -show_entries stream=nb_frames -of csv=p=0 "$1" 2>/dev/null || echo "1")
            [[ "$frames" =~ ^[0-9]+$ && $frames -gt 1 ]];;
        *) return 1;;
    esac
}

is_live_photo() {
    local basename=$(basename "$1") dir=$(dirname "$1")
    if [[ "$basename" =~ ^IMG_[0-9]+\.HEIC$ ]]; then
        [[ -f "${dir}/${basename%.HEIC}.MOV" ]]
    else return 1; fi
}

is_spatial_image() {
    local mime=$(get_mime_type "$1")
    if [[ "$mime" == "image/heif" || "$mime" == "image/heic" ]]; then
        exiftool -s -s -s -ProjectionType "$1" 2>/dev/null | grep -q -E 'equirectangular|cubemap' 2>/dev/null
    else return 1; fi
}

create_safe_temp_dir() {
    local base_dir="${TEMP_DIR:-/tmp}"
    local temp_dir
    temp_dir=$(mktemp -d "$base_dir/conv_XXXXXXXX")
    if [[ -z "$temp_dir" || ! -d "$temp_dir" ]]; then
        temp_dir="$base_dir/conv_$$_$(date +%s)_$(shuf -i 1000-9999 -n 1)"
        mkdir -p "$temp_dir" || return 1
    fi
    echo "$temp_dir"
}

create_backup() {
    if [[ $ENABLE_BACKUPS -eq 0 ]]; then return 1; fi
    local file="$1" backup_dir="$2"
    [[ ! -f "$file" ]] && return 1
    local filename=$(basename "$file")
    local backup_path="$backup_dir/${filename%.*}_$(date +%s).bak"
    cp "$file" "$backup_path" 2>/dev/null && echo "$backup_path" || return 1
}

cleanup_old_files() {
    local dir="$1" max_files="$2" pattern="$3"
    [[ ! -d "$dir" ]] && return
    local file_count
    file_count=$(find "$dir" -name "$pattern" -type f 2>/dev/null | wc -l | tr -d ' ')
    if [[ $file_count -gt $max_files ]]; then
        find "$dir" -name "$pattern" -type f -printf '%T@ %p\n' 2>/dev/null | sort -n | head -n $((file_count - max_files + 10)) | cut -d' ' -f2- | xargs -r rm -f 2>/dev/null || true
    fi
}

get_adaptive_threshold() {
    local mime="$1" size="$2"
    case "$mime" in
        image/gif) if [[ $((size >> 21)) -gt 0 ]]; then echo "20"; else echo "35"; fi;;
        image/png|image/bmp) echo "25";; video/*) echo "50";; *) echo "30";;
    esac
}

backup_metadata() {
    if command -v exiftool >/dev/null 2>&1; then
        exiftool -TagsFromFile "$1" -all:all --icc_profile -overwrite_original -preserve "$2" >/dev/null 2>>"$CONVERSION_LOG" || log_message "WARN" "元数据迁移可能不完整: $(basename "$1")"
    fi
    local src_time
    src_time=$(stat -f%m "$1" 2>/dev/null || echo "0")
    if [[ "$src_time" != "0" ]]; then
        touch -m -r "$1" "$2" 2>/dev/null || true
    fi
}

preserve_file_times_from_src_to_dst() {
    local src="$1" dst="$2"
    [[ ! -e "$src" || ! -e "$dst" ]] && return 0
    local src_mtime
    src_mtime=$(stat -f%m "$src" 2>/dev/null || echo "0")
    if [[ "$src_mtime" != "0" ]]; then
        touch -m -t "$(date -r "$src_mtime" "+%Y%m%d%H%M.%S")" "$dst" 2>/dev/null || true
    fi
    if command -v SetFile >/dev/null 2>&1; then
        local src_btime
        src_btime=$(stat -f%B "$src" 2>/dev/null || echo "0")
        if [[ "$src_btime" != "0" ]]; then
            local create_str
            create_str=$(date -r "$src_btime" "+%m/%d/%Y %H:%M:%S")
            SetFile -d "$create_str" "$dst" 2>/dev/null || true
        fi
    fi
}

ensure_even_dimensions() {
    local input="$1" output="$2"
    local dimensions
    dimensions=$(ffprobe -v quiet -select_streams v:0 -show_entries stream=width,height -of csv=s=x:p=0 "$input" 2>/dev/null || echo "0x0")
    local width height
    width=$(echo "$dimensions" | cut -d'x' -f1)
    height=$(echo "$dimensions" | cut -d'x' -f2)
    if [[ "$width" =~ ^[0-9]+$ && "$height" =~ ^[0-9]+$ && $width -gt 0 && $height -gt 0 && ($((width % 2)) -ne 0 || $((height % 2)) -ne 0) ]]; then
        log_message "INFO" "调整奇数分辨率: ${width}x${height} -> $(basename "$input")"
        if ffmpeg_quiet -y -i "$input" -vf "pad=ceil(iw/2)*2:ceil(ih/2)*2" -c:a copy "$output" 2>>"$CONVERSION_LOG"; then echo "$output"; else echo "$input"; fi
    else echo "$input"; fi
}

# --- macOS Specific Performance Functions ---
memory_watchdog() {
    THROTTLE_FILE=$(mktemp -t throttle_control.XXXXXX)
    rm -f "$THROTTLE_FILE"
    while true; do
        local pressure
        pressure=$(memory_pressure -Q | grep "System-wide memory pressure percentage" | awk '{print $5}' | sed 's/%//')
        if [[ -n "$pressure" && "$pressure" -gt 60 ]]; then
            if [[ ! -f "$THROTTLE_FILE" ]]; then
                log_message "WARN" "系统内存压力过高 (${pressure}%%), 暂停新任务..."
                touch "$THROTTLE_FILE"
            fi
        else
            if [[ -f "$THROTTLE_FILE" ]]; then
                log_message "INFO" "系统内存压力已缓解 (${pressure}%%), 恢复任务..."
                rm -f "$THROTTLE_FILE"
            fi
        fi
        sleep 5
    done
}

wait_for_memory() {
    if [[ -n "$THROTTLE_FILE" ]]; then
        while [[ -f "$THROTTLE_FILE" ]]; do sleep 2; done
    fi
}


# --- Core Conversion Logic ---
generate_lossless_image() {
    local input="$1" output="$2"
    if is_animated "$input"; then
        if ! ffmpeg_quiet -y -i "$input" -c:v libsvtav1 -qp 0 -preset 8 -pix_fmt yuv420p -f avif "$output" 2>>"$CONVERSION_LOG"; then
            log_message "ERROR" "无损动态AVIF转换失败: $(basename "$input")"
            return 1
        fi
    else
        if command -v cjxl >/dev/null 2>&1; then
            local input_ext="${input##*.}"
            if [[ "${input_ext,,}" == "avif" ]]; then
                log_message "INFO" "跳过AVIF文件的JXL转换: $(basename "$input")"
                if timeout 120 magick "$input" -quality 100 "$output" >/dev/null 2>>"$CONVERSION_LOG" 2>&1; then return 0; fi
            else
                if timeout 120 cjxl "$input" "$output" -d 0 -e 9 >/dev/null 2>>"$CONVERSION_LOG" 2>&1; then return 0;
                else
                    log_message "ERROR" "cjxl无损JXL转换失败或超时: $(basename "$input")"
                    if timeout 120 magick "$input" -quality 100 "$output" >/dev/null 2>>"$CONVERSION_LOG" 2>&1; then return 0; fi
                fi
            fi
        else
            if timeout 120 magick "$input" -quality 100 "$output" >/dev/null 2>>"$CONVERSION_LOG" 2>&1; then return 0; fi
        fi
        return 1
    fi; return 0
}

generate_first_lossy_image() {
    local input="$1" output="$2" mime
    mime=$(get_mime_type "$input")
    if is_animated "$input"; then
        local dimension_fixed_temp input_file
        dimension_fixed_temp="$TEMP_DIR/fixed_lossy_$$.${input##*.}"
        input_file=$(ensure_even_dimensions "$input" "$dimension_fixed_temp")
        if ffmpeg_quiet -y -i "$input_file" -c:v libsvtav1 -crf 32 -preset 7 -pix_fmt yuv420p -f avif "$output" 2>>"$CONVERSION_LOG"; then
            [[ "$input_file" != "$input" ]] && rm -f "$input_file"
            return 0
        fi
        [[ "$input_file" != "$input" ]] && rm -f "$input_file"
    else
        local quality=80
        case "$mime" in image/gif|image/png|image/bmp) quality=85;; image/jpeg) quality=75;; esac
        if timeout 120 magick "$input" -quality "$quality" "$output" >/dev/null 2>>"$CONVERSION_LOG" 2>&1; then return 0; fi
    fi
    log_message "ERROR" "初步有损转换失败: $(basename "$input")"
    return 1
}

make_smart_decision() {
    local orig_size="$1" lossless_size="$2" lossy_size="$3" threshold="$4"
    if [[ $lossless_size -le 0 && $lossy_size -le 0 ]]; then echo "ERROR"; return; fi
    if [[ $lossless_size -gt 0 && $lossy_size -le 0 ]]; then echo "USE_LOSSLESS_SIGNIFICANT"; return; fi
    if [[ $lossy_size -gt 0 && $lossless_size -le 0 ]]; then
        local threshold_80=$((orig_size * 4 / 5))
        if [[ $lossy_size -lt $threshold_80 ]]; then echo "USE_LOSSY_ACCEPTABLE"; else echo "EXPLORE_FURTHER"; fi
        return
    fi
    local threshold_20=$((orig_size / 5)) threshold_50=$((lossy_size / 2))
    if [[ $lossless_size -lt $threshold_20 && $lossless_size -lt $threshold_50 ]]; then echo "USE_LOSSLESS_EXTREME"; return; fi
    local gap=0
    if [[ $orig_size -gt 0 ]]; then gap=$(( (lossy_size - lossless_size) * 100 / orig_size )); fi
    if [[ $lossless_size -lt $lossy_size && $gap -gt $threshold ]]; then echo "USE_LOSSLESS_SIGNIFICANT";
    elif [[ $lossless_size -lt $lossy_size ]]; then echo "USE_LOSSLESS_BETTER";
    elif [[ $lossy_size -lt $((orig_size * 4 / 5)) ]]; then echo "USE_LOSSY_ACCEPTABLE";
    else echo "EXPLORE_FURTHER"; fi
}

unified_smart_analysis_image() {
    local input="$1" temp_output_base="$2" original_size="$3"
    local mime=$(get_mime_type "$input")
    local threshold=$(get_adaptive_threshold "$mime" "$original_size")
    local lossless_ext
    if is_animated "$input"; then
        lossless_ext="avif"
    else
        local input_ext="${input##*.}"
        if [[ "${input_ext,,}" == "avif" ]]; then
            lossless_ext="avif"
        else
            lossless_ext="jxl"
        fi
    fi
    
    local lossless_file="${temp_output_base}_lossless.${lossless_ext}" first_lossy_file="${temp_output_base}_first.avif"
    
    if [[ "$CURRENT_MODE_FOR_SUBPROCESS" == "quality" ]]; then
        generate_lossless_image "$input" "$lossless_file"
        if [[ $? -eq 0 && -f "$lossless_file" && $(get_file_size "$lossless_file") -gt 0 ]]; then
             echo "$( [[ "$lossless_ext" == "jxl" ]] && echo "JXL-Quality" || echo "AVIF-Quality")|${lossless_file}|QUALITY_LOSSLESS_FORCED"; return 0
        fi
    else
        generate_lossless_image "$input" "$lossless_file" & local lossless_pid=$!
        generate_first_lossy_image "$input" "$first_lossy_file" & local lossy_pid=$!
        wait $lossless_pid; local lossless_success=$?
        wait $lossy_pid; local lossy_success=$?
        
        local lossless_size=0; [[ $lossless_success -eq 0 && -s "$lossless_file" ]] && lossless_size=$(get_file_size "$lossless_file")
        local lossy_size=0; [[ $lossy_success -eq 0 && -s "$first_lossy_file" ]] && lossy_size=$(get_file_size "$first_lossy_file")

        local decision=$(make_smart_decision "$original_size" "$lossless_size" "$lossy_size" "$threshold")
        case "$decision" in
            "USE_LOSSLESS_EXTREME"|"USE_LOSSLESS_BETTER"|"USE_LOSSLESS_SIGNIFICANT")
                rm -f "$first_lossy_file" 2>/dev/null
                if [[ -f "$lossless_file" && $lossless_size -gt 0 ]]; then
                    echo "$([[ "$lossless_ext" == "jxl" ]] && echo "JXL-Lossless" || echo "AVIF-Lossless")|${lossless_file}|SMART_LOSSLESS"; return 0
                fi;;
            "USE_LOSSY_ACCEPTABLE")
                rm -f "$lossless_file" 2>/dev/null
                if [[ -f "$first_lossy_file" && $lossy_size -gt 0 ]]; then
                    echo "AVIF-Smart|${first_lossy_file}|SMART_LOSSY"; return 0
                fi;;
            "EXPLORE_FURTHER")
                rm -f "$lossless_file" "$first_lossy_file" 2>/dev/null
                continue_lossy_exploration "$input" "$temp_output_base" "$original_size"; return $?;;
        esac
    fi
    rm -f "$lossless_file" "$first_lossy_file" 2>/dev/null; return 1
}

continue_lossy_exploration() {
    if is_animated "$1"; then continue_animated_exploration "$@"; else continue_static_exploration "$@"; fi
}

continue_static_exploration() {
    local input="$1" temp_output_base="$2" original_size="$3"
    local quality_levels=(70 55 40); local best_file="" best_size=$original_size best_quality=""
    for q in "${quality_levels[@]}"; do
        local test_file="${temp_output_base}_q${q}.avif"
        if timeout 120 magick "$input" -quality "$q" "$test_file" >/dev/null 2>>"$CONVERSION_LOG" 2>&1 && [[ -s "$test_file" ]]; then
            local test_size=$(get_file_size "$test_file")
            if [[ $test_size -gt 0 && $test_size -lt $best_size ]]; then
                [[ -n "$best_file" ]] && rm -f "$best_file"
                best_file="$test_file"; best_size=$test_size; best_quality="AVIF-Q$q"
                if [[ $test_size -lt $((original_size * 3 / 5)) ]]; then break; fi
            else rm -f "$test_file"; fi
        fi
    done
    if [[ -n "$best_file" && -f "$best_file" && $best_size -lt $original_size ]]; then
        echo "$best_quality|${best_file}|SMART_LOSSY_EXPLORED"; return 0
    else [[ -n "$best_file" ]] && rm -f "$best_file"; return 1; fi
}

continue_animated_exploration() {
    local input="$1" temp_output_base="$2" original_size="$3"
    local crf_levels=(40 50); local best_file="" best_size=$original_size best_crf=""
    local input_file
    input_file=$(ensure_even_dimensions "$input" "$TEMP_DIR/fixed_explore_$$.${input##*.}")
    for crf in "${crf_levels[@]}"; do
        local test_file="$TEMP_DIR/test_vid_crf${crf}_$$.avif"
        if ffmpeg_quiet -y -i "$input_file" -c:v libsvtav1 -crf "$crf" -preset 7 -c:a copy -avoid_negative_ts make_zero -f avif "$test_file" 2>>"$CONVERSION_LOG"; then
            local new_size=$(get_file_size "$test_file")
            if [[ $new_size -gt 0 && $new_size -lt $best_size ]]; then
                [[ -n "$best_file" ]] && rm -f "$best_file"
                best_file="$test_file"; best_size=$new_size; best_crf="AV1-CRF$crf"
                if [[ $new_size -lt $((original_size / 2)) ]]; then break; fi
            else rm -f "$test_file"; fi
        fi
    done
    [[ "$input_file" != "$input" ]] && rm -f "$input_file"
    if [[ -n "$best_file" && -f "$best_file" ]]; then
        echo "$best_crf|${best_file}|SMART_LOSSY_EXPLORED"; return 0
    else return 1; fi
}

# --- [NEW] Video Fallback Conversion Functions ---

# Quality Mode Fallbacks
attempt_hevc_lossless() {
    local input="$1" output="$2"
    log_message "INFO" "质量模式: 尝试无损HEVC..."
    if timeout "$MAX_VIDEO_TASK_SECONDS" ffmpeg -hide_banner -v error -y -i "$input" -c:v libx265 -x265-params lossless=1 -c:a aac -b:a 192k -movflags +faststart -avoid_negative_ts make_zero "$output" 2>>"$CONVERSION_LOG"; then
        echo "HEVC-Quality(SW)|${output}|QUALITY_ANALYSIS"
        return 0
    fi
    return 1
}

attempt_av1_lossless() {
    local input="$1" output="$2"
    log_message "WARN" "无损HEVC失败, 回退到无损AV1..."
    local input_file
    input_file=$(ensure_even_dimensions "$input" "${2%.mov}_av1_fixed.mov")
    if timeout "$MAX_VIDEO_TASK_SECONDS" ffmpeg -hide_banner -v error -y -i "$input_file" -c:v libsvtav1 -qp 0 -preset 8 -c:a copy -movflags +faststart -avoid_negative_ts make_zero "$output" 2>>"$CONVERSION_LOG"; then
        [[ "$input_file" != "$input" ]] && rm -f "$input_file"
        echo "AV1-Lossless-Fallback|${output}|QUALITY_FALLBACK"
        return 0
    fi
     [[ "$input_file" != "$input" ]] && rm -f "$input_file"
    return 1
}

# Efficiency Mode Fallbacks
attempt_hevc_lossy() {
    local input="$1" output="$2"
    log_message "INFO" "效率模式: 尝试HEVC (CRF28)..."
    local input_file
    input_file=$(ensure_even_dimensions "$input" "${2%.mov}_hevc_fixed.mov")
    if timeout "$MAX_VIDEO_TASK_SECONDS" ffmpeg -hide_banner -v error -y -i "$input_file" -c:v libx265 -crf 28 -preset medium -c:a aac -b:a 128k -movflags +faststart -avoid_negative_ts make_zero "$output" 2>>"$CONVERSION_LOG"; then
         [[ "$input_file" != "$input" ]] && rm -f "$input_file"
        echo "HEVC-CRF28|${output}|LOSSY_HEVC"
        return 0
    fi
     [[ "$input_file" != "$input" ]] && rm -f "$input_file"
    return 1
}

attempt_av1_lossy() {
    local input="$1" output="$2"
    log_message "WARN" "HEVC转换失败, 回退到AV1 (CRF35)..."
    local input_file
    input_file=$(ensure_even_dimensions "$input" "${2%.mov}_av1_fixed.mov")
    if timeout "$MAX_VIDEO_TASK_SECONDS" ffmpeg -hide_banner -v error -y -i "$input_file" -c:v libsvtav1 -crf 35 -preset 7 -c:a aac -b:a 128k -movflags +faststart -avoid_negative_ts make_zero "$output" 2>>"$CONVERSION_LOG"; then
         [[ "$input_file" != "$input" ]] && rm -f "$input_file"
        echo "AV1-CRF35-Fallback|${output}|LOSSY_FALLBACK_AV1"
        return 0
    fi
     [[ "$input_file" != "$input" ]] && rm -f "$input_file"
    return 1
}

attempt_remux() {
    local input="$1" output="$2"
    log_message "WARN" "编码失败, 回退到仅封装复制 (REMUX)..."
    if timeout "$MAX_VIDEO_TASK_SECONDS" ffmpeg -hide_banner -v error -y -i "$input" -c copy -map 0 -movflags +faststart -avoid_negative_ts make_zero "$output" 2>>"$CONVERSION_LOG"; then
        echo "REMUX-Copy|${output}|REPAIR_FALLBACK_REMUX"
        return 0
    fi
    return 1
}

attempt_video_only() {
    local input="$1" output="$2"
    log_message "WARN" "封装复制失败, 最终尝试仅导出视频..."
    local input_file
    input_file=$(ensure_even_dimensions "$input" "${2%.mov}_vidonly_fixed.mov")
    if timeout "$MAX_VIDEO_TASK_SECONDS" ffmpeg -hide_banner -v error -y -i "$input_file" -c:v libx265 -crf 28 -preset medium -an -movflags +faststart -avoid_negative_ts make_zero "$output" 2>>"$CONVERSION_LOG"; then
         [[ "$input_file" != "$input" ]] && rm -f "$input_file"
        echo "HEVC-VideoOnly|${output}|AUDIO_STRIP_FALLBACK"
        return 0
    fi
     [[ "$input_file" != "$input" ]] && rm -f "$input_file"
    return 1
}

# --- [REVISED] Video Conversion Master Functions ---

convert_video_with_fallbacks() {
    local input="$1" temp_output_base="$2" current_mode="$3"
    local temp_file="" result=""

    if [[ "$current_mode" == "quality" ]]; then
        # Quality Mode Fallback Chain: Lossless HEVC -> Lossless AV1 -> Remux
        temp_file="${temp_output_base}_quality.mov"
        result=$(attempt_hevc_lossless "$input" "$temp_file")
        if [[ $? -ne 0 || ! -s "$temp_file" ]]; then
            rm -f "$temp_file"
            temp_file="${temp_output_base}_fallback_q_av1.mov"
            result=$(attempt_av1_lossless "$input" "$temp_file")
        fi
        if [[ $? -ne 0 || ! -s "$temp_file" ]]; then
            rm -f "$temp_file"
            temp_file="${temp_output_base}_fallback_q_remux.mov"
            result=$(attempt_remux "$input" "$temp_file")
        fi
    else
        # Efficiency Mode Fallback Chain: HEVC -> AV1 -> Remux -> Video-Only
        temp_file="${temp_output_base}_efficiency.mov"
        result=$(attempt_hevc_lossy "$input" "$temp_file")
        if [[ $? -ne 0 || ! -s "$temp_file" ]]; then
            rm -f "$temp_file"
            temp_file="${temp_output_base}_fallback_e_av1.mov"
            result=$(attempt_av1_lossy "$input" "$temp_file")
        fi
        if [[ $? -ne 0 || ! -s "$temp_file" ]]; then
            rm -f "$temp_file"
            temp_file="${temp_output_base}_fallback_e_remux.mov"
            result=$(attempt_remux "$input" "$temp_file")
        fi
        if [[ $? -ne 0 || ! -s "$temp_file" ]]; then
            rm -f "$temp_file"
            temp_file="${temp_output_base}_fallback_e_video_only.mov"
            result=$(attempt_video_only "$input" "$temp_file")
        fi
    fi

    if [[ -n "$result" && -s "$(echo "$result" | cut -d'|' -f2)" ]]; then
        echo "$result"
        return 0
    fi
    return 1
}


attempt_repair() {
    local input="$1" output="$2"
    log_message "INFO" "尝试修复损坏的媒体文件: $(basename "$input")"
    if ffmpeg_quiet -y -err_detect ignore_err -i "$input" -c copy "$output" 2>>"$CONVERSION_LOG"; then
        log_message "SUCCESS" "文件修复成功 (可能): $(basename "$output")"
        echo "$output"
    else
        log_message "ERROR" "文件修复失败: $(basename "$input")"
        return 1
    fi
}

should_skip_file() {
    local file="$1"; local basename
    basename=$(basename "$file")
    if is_live_photo "$file" || is_spatial_image "$file"; then
        log_message "INFO" "跳过特殊图片 (Live Photo/空间图片): $basename"; return 0
    fi
    local mime
    mime=$(get_mime_type "$file");
    if [[ "$mime" == "unknown" ]]; then log_message "INFO" "跳过未知MIME类型: $basename"; return 0; fi

    local ext_lc
    ext_lc="${file##*.}"; ext_lc="${ext_lc,,}"
    # [MODIFICATION] Add more unsupported project file extensions
    case "$ext_lc" in
        psd|psb|ai|indd|aep|prproj|aegraphic|sketch|fig|blend|kra|clip|xcf)
            log_message "INFO" "跳过不支持的工程文件格式: $basename ($ext_lc)"; return 0;;
    esac
    
    case "$mime" in
        image/*) local target_ext; target_ext=$([[ "$CURRENT_MODE_FOR_SUBPROCESS" == "quality" ]] && echo "jxl" || echo "avif");;
        video/*) local target_ext="mov";;
        *) log_message "INFO" "跳过不支持的MIME类型: $basename ($mime)"; return 0;;
    esac
    if [[ "${file##*.}" == "$target_ext" ]]; then log_message "INFO" "文件已是目标格式: $basename"; return 0; fi
    local target_filename="${file%.*}.$target_ext"
    if [[ -f "$target_filename" && "$file" != "$target_filename" ]]; then
        log_message "INFO" "跳过，目标文件已存在: $(basename "$target_filename")"; return 0
    fi; return 1
}

process_file() {
    wait_for_memory
    local file="$1" force_mode="$2"
    local basename
    basename=$(basename "$file")
    log_message "INFO" "开始处理: $basename (模式: ${force_mode:-$CURRENT_MODE_FOR_SUBPROCESS})"
    local current_mode=${force_mode:-$CURRENT_MODE_FOR_SUBPROCESS}
    export CURRENT_MODE_FOR_SUBPROCESS="$current_mode"

    local result_filename
    result_filename=$(echo -n "$file" | shasum | awk '{print $1}')
    local result_file="$RESULTS_DIR/$result_filename"
    local original_size
    original_size=$(get_file_size "$file")
    
    local safe_temp_dir
    safe_temp_dir=$(create_safe_temp_dir) || {
        log_message "ERROR" "无法创建临时目录: $basename"
        echo "FAIL|$basename" > "$result_file"
        return 1
    }
    local temp_output_base="$safe_temp_dir/conv_$(echo "$basename" | tr ' ' '_')"
    
    local result="" temp_file="" quality_stat="" decision_tag="NONE"

    if [[ "$current_mode" == "repair" ]]; then
        local repaired_file
        repaired_file=$(attempt_repair "$file" "${temp_output_base}_repaired.${file##*.}")
        if [[ $? -eq 0 && -n "$repaired_file" && -s "$repaired_file" ]]; then
            file="$repaired_file"
            original_size=$(get_file_size "$file")
            decision_tag="REPAIRED"
            current_mode="efficiency"
            export CURRENT_MODE_FOR_SUBPROCESS="efficiency"
        else
            log_message "ERROR" "修复后处理失败: $basename"; echo "FAIL|$basename|REPAIR_FAILED" > "$result_file";
            rm -rf "$safe_temp_dir" 2>/dev/null; return 1
        fi
    fi

    local mime
    mime=$(get_mime_type "$file")
    if [[ "$mime" == video/* ]]; then
        result=$(convert_video_with_fallbacks "$file" "$temp_output_base" "$current_mode")
    else
        result=$(unified_smart_analysis_image "$file" "$temp_output_base" "$original_size")
    fi
    
    if [[ -n "$result" ]]; then
        quality_stat=$(echo "$result" | cut -d'|' -f1)
        temp_file=$(echo "$result" | cut -d'|' -f2)
        local new_decision_tag
        new_decision_tag=$(echo "$result" | cut -d'|' -f3)
        if [[ "$decision_tag" == "REPAIRED" ]]; then
            decision_tag="REPAIRED_${new_decision_tag}"
        else
            decision_tag=$new_decision_tag
        fi

        local new_size
        new_size=$(get_file_size "$temp_file")
        if [[ $new_size -gt 0 ]]; then
            local should_replace=0 size_change_type=""
            if [[ "$current_mode" == "quality" ]]; then
                should_replace=1
                if [[ $new_size -lt $original_size ]]; then size_change_type="REDUCED"
                elif [[ $new_size -gt $original_size ]]; then size_change_type="INCREASED"
                else size_change_type="UNCHANGED"; fi
            else
                if [[ $new_size -lt $original_size ]]; then should_replace=1; size_change_type="REDUCED";
                elif [[ $new_size -eq $original_size ]]; then should_replace=1; size_change_type="UNCHANGED";
                else size_change_type="INCREASED"; fi
            fi
            
            if [[ $should_replace -eq 1 ]]; then
                local backup_dir="$TARGET_DIR/.backups"
                mkdir -p "$backup_dir" 2>/dev/null || true
                if [[ -n "$(create_backup "$file" "$backup_dir")" ]]; then
                     log_message "INFO" "创建备份: $(basename "$file")"
                     cleanup_old_files "$backup_dir" "$MAX_BACKUP_FILES" "*.bak"
                fi
                backup_metadata "$file" "$temp_file"
                local target_file="${file%.*}.${temp_file##*.}"
                mv "$temp_file" "$target_file"
                preserve_file_times_from_src_to_dst "$file" "$target_file"
                if [[ "$file" != "$target_file" && ! "$file" =~ "_repaired" ]]; then rm -f "$file"; fi
                log_message "SUCCESS" "$(basename "$target_file") | $(numfmt --to=iec "$original_size") -> $(numfmt --to=iec "$new_size") | $quality_stat"
                echo "SUCCESS|$original_size|$new_size|$quality_stat|$decision_tag|$size_change_type" > "$result_file"
            else
                log_message "WARN" "转换后文件增大，不替换: $basename ($(numfmt --to=iec "$original_size") -> $(numfmt --to=iec "$new_size"))"
                echo "SKIP|$basename|$size_change_type" > "$result_file"; rm -f "$temp_file" 2>/dev/null
            fi
        else
            rm -f "$temp_file" 2>/dev/null; log_message "ERROR" "转换后文件大小无效: $basename"; echo "FAIL|$basename" > "$result_file"
        fi
    else
        log_message "ERROR" "核心转换过程失败: $basename"; echo "FAIL|$basename" > "$result_file"
    fi
    if [[ "$file" =~ "_repaired" ]]; then rm -f "$file"; fi
    rm -rf "$safe_temp_dir" 2>/dev/null || true
}

# --- Indexing, Progress, Auto-Mode, and Main Loops ---
index_files() {
    RESUMED_COUNT=0
    local all_found_files
    
    mapfile -d $'\0' all_found_files < <(find "$TARGET_DIR" -type f -print0)
    local total_found=${#all_found_files[@]}
    if [[ $total_found -eq 0 ]]; then return 1; fi

    echo -e "  发现 ${COLOR_VIOLET}${total_found}${RESET} 个文件，正在建立索引 (此过程可能需要一些时间)..."
    local temp_index_file="$TEMP_DIR/unsorted_index.tmp"
    : > "$temp_index_file"
    
    for i in "${!all_found_files[@]}"; do
        local file="${all_found_files[$i]}"
        # Exclude files in our own working directories
        if [[ "$file" == *"/.backups/"* || "$file" == *"/.media_conversion_results/"* ]]; then
            continue
        fi
        show_progress "$((i+1))" "$total_found"

        local result_filename
        result_filename=$(echo -n "$file" | shasum | awk '{print $1}')
        if [[ -f "$RESULTS_DIR/$result_filename" ]]; then
            ((RESUMED_COUNT++))
            continue
        fi
        
        local size
        size=$(get_file_size "$file")
        if [[ $SORT_ORDER == "quality" ]]; then
            local dims w h area quality_score
            dims=$(ffprobe -v quiet -select_streams v:0 -show_entries stream=width,height -of csv=p=0 "$file" 2>/dev/null || echo "0,0")
            w=$(echo "$dims" | cut -d, -f1)
            h=$(echo "$dims" | cut -d, -f2)
            area=$((w*h))
            quality_score=1000
            if [[ $area -gt 0 ]]; then quality_score=$(( size * 1000 / area )); fi
            echo "$quality_score|$file" >> "$temp_index_file"
        else
            echo "$size|$file" >> "$temp_index_file"
        fi
    done
    
    if [[ -s "$temp_index_file" ]]; then
        sort -n "$temp_index_file" | cut -d'|' -f2- > "$INDEX_FILE"
        rm "$temp_index_file"
    else
        : > "$INDEX_FILE"
    fi
}

handle_user_choice() {
    local files_array_name="$1" default_action="$2" prompt_msg="$3" action1_msg="$4" action2_msg="$5" action3_msg="$6"
    local -n files_ref="$files_array_name"
    local file_count=${#files_ref[@]}
    if (( file_count > 0 )); then
        if [[ -t 0 ]]; then
            echo -e "\n${BOLD}${COLOR_PROMPT}${prompt_msg}${RESET}"
            echo -e "  ${action1_msg}"
            echo -e "  ${action2_msg}"
            echo -e "  ${action3_msg}"
            local choice
            if ! read -t 10 -p "  请输入您的选择 [10秒后默认 $default_action]: " choice; then
                choice="$default_action"
                echo "$default_action"
            fi
            case "$choice" in
                1) for f in "${files_ref[@]}"; do [[ -n "$f" ]] && rm -v "$f"; done;;
                2|3) local action_mode=$([[ "$choice" == "2" && "$files_array_name" == "quality_corrupt" ]] && echo "repair" || echo "skip")
                     action_mode=$([[ "$choice" == "3" && "$files_array_name" == "quality_very_low" ]] && echo "repair" || echo "$action_mode")
                     if [[ "$action_mode" == "repair" ]]; then
                         for f in "${files_ref[@]}"; do [[ -n "$f" ]] && echo "repair|$f" >> "$temp_queue_file"; done
                     fi;;
                *) echo "跳过...";;
            esac
        else
            echo -e "\n${DIM}${COLOR_SUBTLE}非交互环境检测到 ${file_count} 个特殊文件，默认跳过。${RESET}"
        fi
    fi
}

auto_mode_analysis() {
    RUN_STARTED=1
    declare -a quality_high=() quality_medium=() quality_low=() quality_very_low=() quality_corrupt=() quality_unsupported=()
    echo -e "  ${BOLD}${COLOR_PROMPT}🔎 [Auto Mode]${RESET} 正在进行深度质量扫描..."

    mapfile -d $'\0' all_files < <(find "$TARGET_DIR" -type f -print0)
    local total_files=${#all_files[@]}
    if [[ $total_files -eq 0 ]]; then echo -e "${COLOR_YELLOW}⚠️ 未发现媒体文件。${RESET}"; return 1; fi

    for i in "${!all_files[@]}"; do
        local file="${all_files[$i]}"
        if [[ "$file" == *"/.backups/"* || "$file" == *"/.media_conversion_results/"* ]]; then
            continue
        fi
        show_progress "$((i+1))" "$total_files"
        local mime
        mime="$(get_mime_type "$file")"
        if [[ "$mime" != image/* && "$mime" != video/* ]]; then
            quality_unsupported+=("$file")
            continue
        fi
        if command -v ffprobe >/dev/null 2>&1; then
            if ! ffprobe -v quiet -i "$file" >/dev/null 2>&1; then
                quality_corrupt+=("$file")
                continue
            fi
        fi
        local dims w h
        dims=$(ffprobe -v quiet -select_streams v:0 -show_entries stream=width,height -of csv=p=0 "$file" 2>/dev/null || echo "0,0")
        w=$(echo "$dims" | cut -d, -f1 | tr -d '\r')
        h=$(echo "$dims" | cut -d, -f2 | tr -d '\r')
        [[ ! "$w" =~ ^[0-9]+$ ]] && w=0
        [[ ! "$h" =~ ^[0-9]+$ ]] && h=0
        local area=$(( w * h ))
        local size
        size=$(get_file_size "$file")
        local bpp100=0
        if [[ $area -gt 0 ]]; then bpp100=$(( size * 800 / area )); fi
        
        local is_very_low=0
        if [[ "$mime" == image/* && $area -ge 250000 && $bpp100 -lt 15 ]]; then is_very_low=1; fi
        if [[ "$mime" == "image/gif" && $area -ge 100000 ]]; then
            local frames
            frames=$(ffprobe -v quiet -select_streams v:0 -show_entries stream=nb_frames -of csv=p=0 "$file" 2>/dev/null || echo "1")
            [[ ! "$frames" =~ ^[0-9]+$ ]] && frames=1
            if [[ $frames -le 5 && $bpp100 -lt 12 ]]; then is_very_low=1; fi
        fi

        if [[ $is_very_low -eq 1 ]]; then
            quality_very_low+=("$file")
            continue
        fi
        case "$mime" in
            image/png|image/bmp|image/tiff) quality_high+=("$file");;
            image/jpeg|video/mp4|video/mov|video/quicktime) quality_medium+=("$file");;
            image/gif|video/webm|video/avi) quality_low+=("$file");;
            *) quality_medium+=("$file");;
        esac
    done
    
    local count_high=${#quality_high[@]} count_medium=${#quality_medium[@]} count_low=${#quality_low[@]}
    local count_corrupt=${#quality_corrupt[@]} count_unsupported=${#quality_unsupported[@]} count_very_low=${#quality_very_low[@]}
    ALL_FILES_COUNT=$((count_high + count_medium + count_low + count_corrupt + count_unsupported + count_very_low))

    echo -e "${CLEAR_LINE}\n${BOLD}${COLOR_BLUE}📊 ================= 质量扫描报告 =================${RESET}"
    echo -e "  ${COLOR_SUCCESS}高画质文件: ${count_high} (将使用 Quality Mode)${RESET}"
    echo -e "  ${COLOR_ORANGE}中等画质文件: ${count_medium} (将使用 Efficiency Mode)${RESET}"
    echo -e "  ${COLOR_YELLOW}低画质文件: ${count_low} (将使用 Efficiency Mode)${RESET}"
    echo -e "  ${COLOR_RED}疑似损坏文件: ${count_corrupt}${RESET}"
    echo -e "  ${DIM}${COLOR_SUBTLE}不支持的文件: ${count_unsupported} (将自动跳过)${RESET}"
    echo -e "  ${COLOR_WARN}极差质量候选: ${count_very_low}${RESET}"
    echo -e "----------------------------------------------------"
    
    local temp_queue_file="$TEMP_DIR/process_queue.txt"
    : > "$temp_queue_file"
    
    handle_user_choice "quality_corrupt" "3" "对于 ${count_corrupt} 个疑似损坏的文件, 请选择操作:" \
        "[1] ${COLOR_RED}删除 (Delete)${RESET}" \
        "[2] ${COLOR_YELLOW}尝试修复并转换 (Attempt Repair)${RESET}" \
        "[3] ${DIM}${COLOR_SUBTLE}跳过 (Skip) [默认]${RESET}"

    handle_user_choice "quality_very_low" "2" "对于 ${count_very_low} 个极差质量的文件, 请选择操作:" \
        "[1] ${COLOR_RED}删除 (Delete)${RESET}" \
        "[2] ${DIM}${COLOR_SUBTLE}跳过 (Skip) [默认]${RESET}" \
        "[3] ${COLOR_YELLOW}试图修复并转换 (Attempt Repair)${RESET}"
    
    if (( count_unsupported > 0 )); then
        log_message "INFO" "自动跳过 ${count_unsupported} 个不支持的文件。"
    fi
    
    echo -e "\n${BOLD}${COLOR_PROMPT}即将根据扫描结果自动处理其余文件...${RESET}"
    RESUMED_COUNT=0
    add_to_queue() {
        local mode="$1" file="$2"
        [[ -z "$file" ]] && return
        local result_filename
        result_filename=$(echo -n "$file" | shasum | awk '{print $1}')
        if [[ -f "$RESULTS_DIR/$result_filename" ]]; then
            ((RESUMED_COUNT++))
        else
            if [[ "$SORT_ORDER" == "size" ]]; then
                local s
                s=$(get_file_size "$file")
                echo "$s|$mode|$file"
            else
                echo "0|$mode|$file"
            fi
        fi
    }

    local temp_order_file="${TEMP_DIR}/auto_queue_order.txt"
    : > "$temp_order_file"
    for f in "${quality_high[@]}"; do add_to_queue "quality" "$f"; done >> "$temp_order_file"
    for f in "${quality_medium[@]}"; do add_to_queue "efficiency" "$f"; done >> "$temp_order_file"
    for f in "${quality_low[@]}"; do add_to_queue "efficiency" "$f"; done >> "$temp_order_file"
    
    sort -n "$temp_order_file" | while IFS='|' read -r _sz _m _p; do
        echo "$_m|$_p" >> "$temp_queue_file"
    done
    rm -f "$temp_order_file" 2>/dev/null || true

    local total_to_run
    total_to_run=$(wc -l < "$temp_queue_file" | tr -d ' ')
    ALL_FILES_COUNT=$(( total_to_run + RESUMED_COUNT ))

    if (( total_to_run == 0 )); then
        echo -e "${DIM}${COLOR_SUBTLE}无需处理的新文件，跳过执行阶段。${RESET}"
        return 0
    fi
    echo -e "${CLEAR_LINE}  发现 ${COLOR_VIOLET}${total_to_run}${RESET} 个待处理文件 (${COLOR_INFO}${RESUMED_COUNT} 个文件已跳过${RESET})，准备启动...🚀"

    if [[ "$(uname)" == "Darwin" ]]; then memory_watchdog & MEMORY_WATCHDOG_PID=$!; fi
    
    export_functions_for_subprocesses
    local baseline_results_count
    baseline_results_count=$(find "$RESULTS_DIR" -type f 2>/dev/null | wc -l | tr -d ' ')
    tr '\n' '\0' < "$temp_queue_file" | xargs -0 -P "$CONCURRENT_JOBS" -I {} bash -c 'run_file_processing_auto "$@"' _ {} & local worker_pid=$!
    ( 
      local total_to_process=$total_to_run
      while true; do 
        local current_count=$(find "$RESULTS_DIR" -type f 2>/dev/null | wc -l | tr -d ' ')
        local completed=$(( current_count - baseline_results_count ))
        if [[ $completed -lt 0 ]]; then completed=0; fi
        if [[ $completed -gt $total_to_process ]]; then completed=$total_to_process; fi
        show_progress "$completed" "$total_to_process"
        if [[ $completed -ge $total_to_process ]] || ! kill -0 "$worker_pid" 2>/dev/null; then break; fi
        sleep 0.2
      done 
    ) & local progress_pid=$!
    if kill -0 "$worker_pid" 2>/dev/null; then
        echo -en "${CLEAR_LINE}${DIM}${COLOR_SUBTLE}100%% 完成，正在收尾... 请稍候${RESET}"
    fi
    wait "$worker_pid"; local worker_status=$?
    kill "$progress_pid" 2>/dev/null || true; wait "$progress_pid" 2>/dev/null || true
    if [[ $worker_status -ne 0 ]]; then log_message "WARN" "并发任务中有失败，但流程将继续。"; fi

    echo -e "${CLEAR_LINE}"
    echo -e "  ${BOLD}${COLOR_PROMPT}✅ ${COLOR_SUCCESS}自动模式处理完成${RESET}"
    flush_log_buffer
    return 0
}

main_conversion_loop() {
    echo -e "  ${BOLD}${COLOR_PROMPT}🔎 [1/3]${RESET} 扫描媒体文件并建立索引...${RESET}"
    if ! index_files; then echo -e "${COLOR_YELLOW}⚠️ 未发现媒体文件。${RESET}"; return 1; fi

    local files_to_run_count
    files_to_run_count=$(wc -l < "$INDEX_FILE" | tr -d ' ')
    ALL_FILES_COUNT=$((files_to_run_count + RESUMED_COUNT))
    
    if [[ $files_to_run_count -eq 0 ]]; then
        echo -e "${COLOR_GREEN}✅ 所有文件均已处理过，无需操作。${RESET}"
        aggregate_results; generate_report; cat "$REPORT_FILE"
        return 0
    fi
    echo -e "${CLEAR_LINE}  发现 ${COLOR_VIOLET}${files_to_run_count}${RESET} 个待处理文件 (${COLOR_INFO}${RESUMED_COUNT} 个文件已跳过${RESET})，准备启动...🚀"
    echo -e "  ${BOLD}${COLOR_PROMPT}⚙️ [2/3]${RESET} 开始统一智能转换 (并发数: ${COLOR_BLUE}${CONCURRENT_JOBS}${RESET})..."
    RUN_STARTED=1
    
    if [[ "$(uname)" == "Darwin" ]]; then memory_watchdog & MEMORY_WATCHDOG_PID=$!; fi
    
    export_functions_for_subprocesses
    export CURRENT_MODE_FOR_SUBPROCESS="$MODE"
    local baseline_results_count
    baseline_results_count=$(find "$RESULTS_DIR" -type f 2>/dev/null | wc -l | tr -d ' ')
    tr '\n' '\0' < "$INDEX_FILE" | xargs -0 -P "$CONCURRENT_JOBS" -I {} bash -c 'run_file_processing_single_mode "$@"' _ {} & local worker_pid=$!
    ( 
      local total_to_process=$files_to_run_count
      while true; do 
        local current_count=$(find "$RESULTS_DIR" -type f 2>/dev/null | wc -l | tr -d ' ')
        local completed=$(( current_count - baseline_results_count ))
        if [[ $completed -lt 0 ]]; then completed=0; fi
        if [[ $completed -gt $total_to_process ]]; then completed=$total_to_process; fi
        show_progress "$completed" "$total_to_process"
        if [[ $completed -ge $total_to_process ]] || ! kill -0 "$worker_pid" 2>/dev/null; then break; fi
        sleep 0.2
      done 
    ) & local progress_pid=$!
    if kill -0 "$worker_pid" 2>/dev/null; then
        echo -en "${CLEAR_LINE}${DIM}${COLOR_SUBTLE}100%% 完成，正在收尾... 请稍候${RESET}"
    fi
    wait "$worker_pid"; local worker_status=$?
    kill "$progress_pid" 2>/dev/null || true; wait "$progress_pid" 2>/dev/null || true
    if [[ $worker_status -ne 0 ]]; then log_message "WARN" "并发任务中有失败，但流程将继续。"; fi
    
    echo -e "${CLEAR_LINE}"
    echo -e "  ${BOLD}${COLOR_PROMPT}✅ [2/3]${RESET} ${COLOR_SUCCESS}所有文件处理完成${RESET}"
    echo -e "  ${BOLD}${COLOR_PROMPT}📊 [3/3]${RESET} 正在汇总结果并生成报告...${RESET}"
    flush_log_buffer
}

# --- UI, Argument Parsing & Main Entry ---
aggregate_results() {
    if [ ! -d "$RESULTS_DIR" ] || [ -z "$(ls -A "$RESULTS_DIR")" ]; then return; fi
    local awk_output
    awk_output=$(cat "$RESULTS_DIR"/* 2>/dev/null | awk -F'|' '
        BEGIN {
            success = 0; fail = 0; skip = 0;
            reduced = 0; increased = 0; unchanged = 0;
            saved = 0; increased_sum = 0;
            smart_decisions = 0; lossless_wins = 0; quality_analysis = 0;
        }
        $1 == "SUCCESS" {
            success++;
            orig = $2; new = $3;
            size_change = $6;
            if (size_change == "REDUCED") { reduced++; saved += orig - new; }
            else if (size_change == "INCREASED") { increased++; increased_sum += new - orig; }
            else if (size_change == "UNCHANGED") { unchanged++; }
            quality_stats[$4]++;
            decision = $5;
            if (decision ~ /^REPAIRED_/) { quality_stats["REPAIRED"]++; sub(/^REPAIRED_/, "", decision); }
            if (decision == "SMART_LOSSLESS") { smart_decisions++; lossless_wins++; }
            else if (decision == "SMART_LOSSY" || decision == "SMART_LOSSY_EXPLORED") { smart_decisions++; }
            else if (decision == "QUALITY_ANALYSIS") { quality_analysis++; }
            else if (decision == "QUALITY_LOSSLESS_OPTIMAL") { quality_analysis++; lossless_wins++; }
            else if (decision == "QUALITY_LOSSLESS_FORCED") { quality_analysis++; }
        }
        $1 == "FAIL" { fail++; print "failed_file:" $2; }
        $1 == "SKIP" { skip++; }
        END {
            print "SUCCESS_COUNT=" success; print "FAIL_COUNT=" fail; print "SKIP_COUNT=" skip;
            print "SIZE_REDUCED=" reduced; print "SIZE_INCREASED=" increased; print "SIZE_UNCHANGED=" unchanged;
            print "TOTAL_SAVED=" saved; print "TOTAL_SIZE_INCREASED_SUM=" increased_sum;
            print "SMART_DECISIONS_COUNT=" smart_decisions; print "LOSSLESS_WINS_COUNT=" lossless_wins;
            print "QUALITY_ANALYSIS_COUNT=" quality_analysis;
            for (stat in quality_stats) { print "quality_stat:" stat ":" quality_stats[stat]; }
        }
    ')
    while IFS= read -r line; do
        if [[ "$line" == *=* ]]; then eval "$line"
        elif [[ "$line" == failed_file:* ]]; then FAILED_FILES+=("$(echo "$line" | cut -d: -f2-)")
        elif [[ "$line" == quality_stat:* ]]; then
            local stat_name stat_count
            stat_name=$(echo "$line" | cut -d: -f2); stat_count=$(echo "$line" | cut -d: -f3)
            for ((i=0; i<stat_count; i++)); do QUALITY_STATS+=("$stat_name"); done
        fi
    done <<< "$awk_output"
}

generate_report() {
    local total_processed=$((SUCCESS_COUNT + FAIL_COUNT + SKIP_COUNT))
    local success_pct=0; 
    if [[ $total_processed -gt $SKIP_COUNT ]]; then
        local effective_total=$((total_processed - SKIP_COUNT))
        if [[ $effective_total -gt 0 ]]; then
            success_pct=$(awk -v s="$SUCCESS_COUNT" -v t="$effective_total" 'BEGIN {printf "%.0f", s/t*100}')
        fi
    fi
    
    local quality_summary
    quality_summary=$(printf "%s\n" "${QUALITY_STATS[@]}" | sort | uniq -c | sort -rn | awk '{printf "%s(%s) ", $2, $1}' || echo "无")
    local saved_space_str
    saved_space_str=$(numfmt --to=iec-i --suffix=B --format="%.2f" "$TOTAL_SAVED" 2>/dev/null || echo "$TOTAL_SAVED B")
    local increased_space_str
    increased_space_str=$(numfmt --to=iec-i --suffix=B --format="%.2f" "$TOTAL_SIZE_INCREASED_SUM" 2>/dev/null || echo "$TOTAL_SIZE_INCREASED_SUM B")
    local net_saved=$((TOTAL_SAVED - TOTAL_SIZE_INCREASED_SUM))
    local net_saved_str
    net_saved_str=$(numfmt --to=iec-i --suffix=B --format="%.2f" "$net_saved" 2>/dev/null || echo "$net_saved B")

    (
    echo -e "${BOLD}${COLOR_BLUE}📊 ================= 媒体转换最终报告 =================${RESET}"
    echo
    echo -e "${DIM}${COLOR_SUBTLE}📁 目录: ${TARGET_DIR}${RESET}"
    echo -e "${DIM}${COLOR_SUBTLE}⚙️ 模式: ${MODE}${RESET}    ${DIM}${COLOR_SUBTLE}🚀 版本: ${VERSION}${RESET}"
    echo -e "${DIM}${COLOR_SUBTLE}⏰ 完成: $(date)${RESET}"
    echo
    echo -e "${BOLD}${COLOR_CYAN}--- 概览 ---${RESET}"
    echo -e "  ${COLOR_VIOLET}总计扫描: ${ALL_FILES_COUNT} 文件${RESET}"
    echo -e "  ${COLOR_SUCCESS}✅ 成功转换: ${SUCCESS_COUNT} (${success_pct}%%)${RESET}"
    echo -e "  ${COLOR_ERROR}❌ 转换失败: ${FAIL_COUNT}${RESET}"
    echo -e "  ${DIM}${COLOR_SUBTLE}⏩ 主动跳过: ${SKIP_COUNT}${RESET}"
    echo -e "  ${COLOR_INFO}🔄 断点续传: ${RESUMED_COUNT} (已处理)${RESET}"
    echo
    
    if [[ "$MODE" != "quality" ]]; then
        local smart_pct=0; [[ $SUCCESS_COUNT -gt 0 ]] && smart_pct=$(awk -v s="$SMART_DECISIONS_COUNT" -v t="$SUCCESS_COUNT" 'BEGIN {printf "%.0f", s/t*100}')
        echo -e "${BOLD}${COLOR_CYAN}--- 智能效率优化统计 ---${RESET}"
        echo -e "  🧠 智能决策文件: ${SMART_DECISIONS_COUNT} (${smart_pct}%% of 成功)"
        echo -e "  💎 无损优势识别: ${LOSSLESS_WINS_COUNT}"
        echo
    else
        echo -e "${BOLD}${COLOR_CYAN}--- 质量模式分析 ---${RESET}"
        echo -e "  🎨 质量分析文件: ${QUALITY_ANALYSIS_COUNT}"
        echo -e "  🏆 无损最优识别: ${LOSSLESS_WINS_COUNT}"
        echo
    fi
    
    echo -e "${BOLD}${COLOR_CYAN}--- 大小变化统计 ---${RESET}"
    echo -e "  📉 体积减小文件: ${SIZE_REDUCED}"
    echo -e "  📈 体积增大文件: ${SIZE_INCREASED}"
    echo -e "  📊 体积不变文件: ${SIZE_UNCHANGED}"
    echo -e "  ${BOLD}💰 总空间节省: ${COLOR_SUCCESS}${saved_space_str}${RESET}"
    if [[ $SIZE_INCREASED -gt 0 ]]; then
        echo -e "  ${BOLD}📈 总空间增加: ${COLOR_WARN}${increased_space_str}${RESET}"
        echo -e "  ${BOLD}🎯 净空间变化: ${COLOR_INFO}${net_saved_str}${RESET}"
    fi
    echo
    echo -e "${BOLD}${COLOR_CYAN}--- 编码质量分布 ---${RESET}"
    echo -e "  ${quality_summary}"
    echo
    echo -e "------------------------------------------"
    echo -e "${DIM}${COLOR_SUBTLE}📄 详细日志: ${CONVERSION_LOG}${RESET}"
    ) > "$REPORT_FILE"
     if [[ ${#FAILED_FILES[@]} -gt 0 ]]; then
        echo -e "\n${COLOR_ERROR}${BOLD}❌ 失败文件列表:${RESET}" >> "$REPORT_FILE"
        printf "  • %s\n" "${FAILED_FILES[@]}" >> "$REPORT_FILE"
    fi
}

show_progress() {
    local current=$1 total=${2:-0}
    if [[ $total -eq 0 ]]; then return; fi
    if [[ $current -lt 0 ]]; then current=0; fi
    if [[ $current -gt $total ]]; then current=$total; fi
    local pct=$(( current * 100 / total ))
    local term_width
    term_width=$(tput cols 2>/dev/null || echo 80)
    local width=$(( term_width - 30 )); [[ $width -lt 20 ]] && width=20; [[ $width -gt 50 ]] && width=50
    local filled_len=$(( width * pct / 100 ))
    local bar
    bar=$(printf "%${filled_len}s" | tr ' ' '█')
    local empty
    empty=$(printf "%$((width - filled_len))s" | tr ' ' '░')
    local emojis=("⳿" "⌛" "⚙️" "🚀")
    local emoji_index=$(( current % 4 ))
    echo -en "${CLEAR_LINE}[${COLOR_INFO}${bar}${RESET}${DIM}${empty}${RESET}] ${BOLD}${pct}%%${RESET} (${COLOR_HIGHLIGHT}${current}${RESET}/${COLOR_HIGHLIGHT}${total}${RESET}) ${emojis[$emoji_index]}"
}

progress_from_results_dir() {
    local current_count
    current_count=$(find "$RESULTS_DIR" -type f 2>/dev/null | wc -l | tr -d ' ')
    local total_to_process=$((ALL_FILES_COUNT - RESUMED_COUNT))
    if [[ $total_to_process -lt 0 ]]; then total_to_process=0; fi
    if [[ $current_count -gt $total_to_process ]]; then current_count=$total_to_process; fi
    show_progress "$current_count" "$total_to_process"
    local flush_interval=50
    if [[ $((current_count % flush_interval)) -eq 0 ]]; then flush_log_buffer; fi
}

run_file_processing_single_mode() {
    if should_skip_file "$1"; then
        local result_filename
        result_filename=$(echo -n "$1" | shasum | awk '{print $1}')
        echo "SKIP|$(basename "$1")" > "$RESULTS_DIR/$result_filename"
    else 
        process_file "$1" ""
    fi
}

run_file_processing_auto() {
    local mode file
    mode=$(echo "$1" | cut -d'|' -f1)
    file=$(echo "$1" | cut -d'|' -f2-)
    export CURRENT_MODE_FOR_SUBPROCESS="$mode"
    run_file_processing_single_mode "$file"
}


export_functions_for_subprocesses() {
    export -f log_message get_mime_type is_animated is_live_photo is_spatial_image get_file_size backup_metadata preserve_file_times_from_src_to_dst
    export -f create_safe_temp_dir create_backup cleanup_old_files get_adaptive_threshold ensure_even_dimensions memory_watchdog wait_for_memory
    export -f generate_lossless_image generate_first_lossy_image make_smart_decision unified_smart_analysis_image
    export -f continue_lossy_exploration continue_static_exploration continue_animated_exploration
    export -f attempt_hevc_lossless attempt_av1_lossless attempt_hevc_lossy attempt_av1_lossy attempt_remux attempt_video_only
    export -f convert_video_with_fallbacks attempt_repair
    export -f should_skip_file process_file run_file_processing_single_mode run_file_processing_auto
    export -f ffmpeg_quiet
    export LOG_DIR CONVERSION_LOG REPORT_FILE TARGET_DIR CONCURRENT_JOBS ENABLE_HW_ACCEL ENABLE_BACKUPS SORT_ORDER TEMP_DIR RESULTS_DIR
    export CURRENT_MODE_FOR_SUBPROCESS MAX_VIDEO_TASK_SECONDS
}

show_help() {
    cat << EOF
${BOLD}${COLOR_BLUE}🚀 媒体批量转换脚本 v$VERSION (高可靠稳定内核版)${RESET}
${DIM}${COLOR_SUBTLE}融合了v12的智能分析与v8的稳定执行引擎，为大规模任务提供极致可靠性。${RESET}
用法: $0 [选项] <目录路径>
${BOLD}${COLOR_CYAN}主要模式:${RESET}
  --mode <type>     转换模式:
                    '${COLOR_GREEN}quality${RESET}'    - 质量优先，保证最佳画质。
                    '${COLOR_ORANGE}efficiency${RESET}' - 高效压缩，寻求体积与质量的平衡。
                    '${COLOR_VIOLET}auto${RESET}'       - ${BOLD}推荐!${RESET} 自动扫描、分类并为文件选择最佳模式。
${BOLD}${COLOR_CYAN}其他选项:${RESET}
  --jobs <N>        并发任务数 (默认: 自动检测)
  --no-hw-accel     禁用硬件加速
  --no-backup       禁用原文件备份
  --sort-by <type>  处理顺序: 'size' (默认) 或 'quality'
  --help            显示此帮助信息
EOF
}

parse_arguments() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --mode) MODE="$2"; shift 2;;
            --jobs) CONCURRENT_JOBS="$2"; shift 2;;
            --no-hw-accel) ENABLE_HW_ACCEL=0; shift;;
            --no-backup) ENABLE_BACKUPS=0; shift;;
            --sort-by) SORT_ORDER="$2"; shift 2;;
            --help) show_help; exit 0;;
            -*) printf "${COLOR_RED}❌ 未知选项:\033[0m %s\n" "$1"; show_help; exit 1;;
            *) if [[ -z "$TARGET_DIR" ]]; then TARGET_DIR="$1"; fi; shift;;
        esac
    done
}

interactive_mode() {
    echo -e "${BOLD}${COLOR_PROMPT}🚀 欢迎使用媒体批量转换脚本 ${COLOR_SUCCESS}v${VERSION}${RESET}"
    echo -e "${DIM}${COLOR_SUBTLE}高可靠稳定内核版 - 专为稳定性打造${RESET}"
    echo -e "====================================================\n"
    while [[ -z "${TARGET_DIR:-}" || ! -d "$TARGET_DIR" ]]; do
        echo -e "  ${BOLD}${COLOR_PROMPT}请将目标文件夹拖拽至此, 然后按 Enter: ${RESET}\c"
        read -r raw_path
        TARGET_DIR=$(echo "$raw_path" | sed "s/^'//;s/'$//;s/^\"//;s/\"$//;s/\\ *$//")
        if [[ -z "$TARGET_DIR" || ! -d "$TARGET_DIR" ]]; then
            echo -e "${CLEAR_LINE}${COLOR_YELLOW}⚠️ 无效的目录，请重新输入。${RESET}"
        fi
    done
    if [[ -z "${MODE:-}" ]]; then
        echo -e "\n  ${BOLD}${COLOR_PROMPT}请选择转换模式: ${RESET}"
        echo -e "  ${COLOR_GREEN}[1]${RESET} ${BOLD}质量模式 (quality)${RESET}"
        echo -e "  ${COLOR_ORANGE}[2]${RESET} ${BOLD}效率模式 (efficiency)${RESET}"
        echo -e "  ${COLOR_VIOLET}[3]${RESET} ${BOLD}自动模式 (auto) ${DIM}${COLOR_SUBTLE}- ${BOLD}推荐!${RESET}"
        local mode_choice
        echo -e "  ${DIM}${COLOR_SUBTLE}[默认] 按回车键选择${RESET} ${COLOR_VIOLET}自动模式${RESET}\n  请输入您的选择 (1/2/3): \c"
        read -r mode_choice
        case "$mode_choice" in 1) MODE="quality";; 2) MODE="efficiency";; *) MODE="auto";; esac
    fi
}

validate_inputs() {
    if [[ -z "${TARGET_DIR:-}" || ! -d "$TARGET_DIR" ]]; then return 1; fi
    if command -v realpath >/dev/null; then
        TARGET_DIR=$(realpath "$TARGET_DIR")
    else
        TARGET_DIR=$(cd "$TARGET_DIR" && pwd)
    fi
    if [[ "$MODE" != "quality" && "$MODE" != "efficiency" && "$MODE" != "auto" ]]; then return 1; fi
    if [[ "$SORT_ORDER" != "size" && "$SORT_ORDER" != "quality" ]]; then echo "Invalid sort order"; return 1; fi
    return 0
}

check_dependencies() {
    local deps=("ffmpeg" "magick" "exiftool" "ffprobe" "file" "stat" "shasum" "awk" "tput" "numfmt")
    local missing_deps=()
    for dep in "${deps[@]}"; do
        if ! command -v "$dep" >/dev/null; then missing_deps+=("$dep"); fi
    done
    if [[ ${#missing_deps[@]} -gt 0 ]]; then
        echo -e "${COLOR_RED}❌ 错误: 缺少以下依赖命令：${RESET}"
        echo -e "  • ${missing_deps[@]}"
        echo -e "\n${COLOR_YELLOW}💡 在 macOS 上安装依赖：${RESET}"
        echo -e "  ${COLOR_BLUE}brew install ffmpeg imagemagick exiftool coreutils gnu-sed${RESET}"
        exit 1
    fi
    if ! ffmpeg -encoders 2>/dev/null | grep -q libsvtav1; then
        echo -e "${COLOR_YELLOW}⚠️ 警告: ffmpeg 未支持 libsvtav1 编码器。${RESET}"
    fi
    if ! command -v cjxl >/dev/null; then
        echo -e "${COLOR_YELLOW}⚠️ 警告: 未找到 cjxl (JPEG XL) 命令。${RESET}"
        echo -e "${DIM}${COLOR_SUBTLE}无损图片压缩将回退到 AVIF。推荐安装：${COLOR_BLUE}brew install jpeg-xl${RESET}"
    fi
}

execute_conversion_task() {
    SUCCESS_COUNT=0; FAIL_COUNT=0; SKIP_COUNT=0; RESUMED_COUNT=0
    SIZE_REDUCED=0; SIZE_INCREASED=0; SIZE_UNCHANGED=0; TOTAL_SAVED=0; TOTAL_SIZE_INCREASED_SUM=0
    SMART_DECISIONS_COUNT=0; LOSSLESS_WINS_COUNT=0; QUALITY_ANALYSIS_COUNT=0
    FAILED_FILES=(); QUALITY_STATS=(); LOG_BUFFER=()
    
    TEMP_DIR=$(mktemp -d);
    RESULTS_DIR="$TARGET_DIR/.media_conversion_results"; mkdir -p "$RESULTS_DIR"
    INDEX_FILE="$TEMP_DIR/file_index.txt"

    init_logging
    log_message "INFO" "转换任务启动 - 目录: $TARGET_DIR, 模式: $MODE, 版本: $VERSION"
    
    if [[ "$MODE" == "auto" ]]; then
        auto_mode_analysis
    else
        main_conversion_loop
    fi
    if [[ $? -ne 0 && "$MODE" != "auto" ]]; then return; fi
    
    aggregate_results
    generate_report
    
    echo -e "\n${COLOR_SUCCESS}${BOLD}================== 全部任务完成 ✅ ==================${RESET}\n"
    cat "$REPORT_FILE"
    echo
}

interactive_session_loop() {
    while true; do
        TARGET_DIR=""; MODE=""
        interactive_mode
        if ! validate_inputs; then 
            echo -e "${COLOR_RED}❌ 配置验证失败。正在返回主菜单...${RESET}"
            sleep 1; continue
        fi
        echo -e "\n${BOLD}${COLOR_CYAN}--- 配置确认 ---${RESET}"
        echo -e "  ${DIM}${COLOR_SUBTLE}目标:${RESET} ${COLOR_BLUE}${TARGET_DIR}${RESET}"
        echo -e "  ${DIM}${COLOR_SUBTLE}模式:${RESET} ${BOLD}${COLOR_HIGHLIGHT}${MODE}${RESET}"
        echo -e "  ${DIM}${COLOR_SUBTLE}并发:${RESET} ${COLOR_VIOLET}${CONCURRENT_JOBS}${RESET}"
        local accel_status=$([[ $ENABLE_HW_ACCEL -eq 1 ]] && echo "${COLOR_SUCCESS}启用 ✅${RESET}" || echo "${COLOR_YELLOW}禁用 ❌${RESET}")
        echo -e "  ${DIM}${COLOR_SUBTLE}加速:${RESET} ${accel_status}"
        echo -e "--------------------"
        
        local confirm_choice
        echo -e "  确认并开始执行吗？(${BOLD}Y${RESET}/${DIM}n${RESET}，回车即Y): \c"
        read -r confirm_choice
        confirm_choice=$(echo "$confirm_choice" | tr -d ' ' | tr '[:upper:]' '[:lower:]')
        if [[ "$confirm_choice" == "n" ]]; then
            echo -e "${COLOR_YELLOW}ℹ️ 操作已取消，返回主菜单。${RESET}"; continue
        fi
        
        echo
        execute_conversion_task
        
        echo -e "${BOLD}${COLOR_CYAN}=== 转换任务完成 ===${RESET}"
        local continue_choice
        echo -e "是否继续进行新的转换任务？(${BOLD}Y${RESET}/${DIM}n${RESET}，回车即Y): \c"
        read -r continue_choice
        continue_choice=$(echo "$continue_choice" | tr -d ' ' | tr '[:upper:]' '[:lower:]')
        if [[ "$continue_choice" == "n" ]]; then
            echo -e "${COLOR_SUCCESS}感谢使用媒体批量转换脚本！👋${RESET}"; break
        fi
        echo -e "\n\n"
    done
}

main() {
    check_dependencies
    if [[ $# -eq 0 ]]; then
        interactive_session_loop
    else
        parse_arguments "$@"
        if ! validate_inputs; then show_help; exit 1; fi
        execute_conversion_task
    fi
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi