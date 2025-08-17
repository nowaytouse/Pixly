#!/bin/bash
set -eo pipefail
if (( BASH_VERSINFO[0] < 4 )); then
    printf "⚠️ \033[1;31m错误:\033[0m 此脚本需要 Bash 版本 4 或更高。\n"
    printf "在 macOS 上，通过 Homebrew 安装更新的 Bash：\033[1;34mbrew install bash\033[0m\n"
    printf "然后使用新 Bash 运行脚本，例如：\033[1;32m/opt/homebrew/bin/bash %s\033[0m\n" "$0"
    exit 1
fi

VERSION="8.8.0-COMPRESSED-FIX"
LOG_DIR="" CONVERSION_LOG="" REPORT_FILE="" MODE="" TARGET_DIR=""
CONCURRENT_JOBS=$(sysctl -n hw.ncpu 2>/dev/null | awk '{print int($1*0.75)}' || echo "4")
if (( CONCURRENT_JOBS == 0 )); then CONCURRENT_JOBS=4; fi
ENABLE_HW_ACCEL=1 TEMP_DIR="" RESULTS_DIR="" ALL_FILES_COUNT=0
SUCCESS_COUNT=0 FAIL_COUNT=0 SKIP_COUNT=0 SIZE_REDUCED=0 SIZE_INCREASED=0 SIZE_UNCHANGED=0
TOTAL_SAVED=0 TOTAL_SIZE_INCREASED_SUM=0 SMART_DECISIONS_COUNT=0 LOSSLESS_WINS_COUNT=0 QUALITY_ANALYSIS_COUNT=0
declare -a FAILED_FILES=() QUALITY_STATS=() LOG_BUFFER=()

BOLD='\033[1m' DIM='\033[2m' RESET='\033[0m' CLEAR_LINE="\r\033[K"
COLOR_BLUE='\033[38;5;39m' COLOR_CYAN='\033[38;5;45m' COLOR_GREEN='\033[38;5;47m' COLOR_YELLOW='\033[38;5;220m'
COLOR_ORANGE='\033[38;5;202m' COLOR_RED='\033[38;5;196m' COLOR_GRAY='\033[38;5;242m' COLOR_LIGHT_GRAY='\033[38;5;250m'
COLOR_VIOLET='\033[38;5;129m' COLOR_SUCCESS=$COLOR_GREEN COLOR_INFO=$COLOR_BLUE COLOR_WARN=$COLOR_YELLOW
COLOR_ERROR=$COLOR_RED COLOR_PROMPT=$COLOR_CYAN COLOR_HIGHLIGHT=$COLOR_VIOLET COLOR_STATS=$COLOR_ORANGE COLOR_SUBTLE=$COLOR_GRAY

ffmpeg_quiet() { ffmpeg -hide_banner -v error "$@"; }

cleanup() {
    printf "\n${CLEAR_LINE}${COLOR_WARN}⚠️ 脚本已中断，正在进行最后的清理工作...${RESET}\n"
    local pids=$(jobs -p 2>/dev/null || echo "")
    if [[ -n "$pids" ]]; then
        echo "$pids" | xargs -r kill -TERM 2>/dev/null || true
        sleep 1
        pids=$(jobs -p 2>/dev/null || echo "")
        [[ -n "$pids" ]] && echo "$pids" | xargs -r kill -KILL 2>/dev/null || true
    fi
    flush_log_buffer
    [[ -n "${TEMP_DIR:-}" && -d "${TEMP_DIR:-}" ]] && rm -rf "$TEMP_DIR" 2>/dev/null || true
    rm -f /tmp/conv_* /tmp/fixed_* /tmp/test_* 2>/dev/null || true
    printf "${COLOR_SUCCESS}✅ 清理完成。${RESET}\n"
}
trap cleanup EXIT INT TERM

init_logging() {
    local timestamp=$(date +"%Y%m%d_%H%M%S")
    LOG_DIR="$TARGET_DIR"
    CONVERSION_LOG="$LOG_DIR/${MODE}_conversion_${timestamp}.txt"
    REPORT_FILE="$LOG_DIR/${MODE}_conversion_report_${timestamp}.txt"
    if [[ ! -f "$CONVERSION_LOG" ]]; then
        cat > "$CONVERSION_LOG" << EOF
媒体转换日志 - $(date)
模式: $MODE (统一智能分析引擎)
目标目录: $TARGET_DIR
并发数: $CONCURRENT_JOBS
硬件加速: $([ $ENABLE_HW_ACCEL -eq 1 ] && echo "启用" || echo "禁用")
分析策略: 双路径对比 + 模式感知决策
=====================================
EOF
    fi
}

flush_log_buffer() {
    if [[ ${#LOG_BUFFER[@]} -gt 0 ]]; then
        printf "%s\n" "${LOG_BUFFER[@]}" >> "$CONVERSION_LOG" 2>/dev/null || true
        LOG_BUFFER=()
    fi
}

log_message() {
    local level="$1" message="$2" timestamp=$(date "+%Y-%m-%d %H:%M:%S")
    LOG_BUFFER+=("[$timestamp] [$level] $message")
    if [[ ${#LOG_BUFFER[@]} -ge 15 ]]; then flush_log_buffer; fi
}

get_mime_type() { file --mime-type -b "$1" 2>/dev/null || echo "unknown"; }

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
        local mov_file="${dir}/${basename%.HEIC}.MOV"
        [[ -f "$mov_file" ]]
    else return 1; fi
}

is_spatial_image() {
    local mime=$(get_mime_type "$1")
    if [[ "$mime" == "image/heif" || "$mime" == "image/heic" ]]; then
        exiftool -s -s -s -ProjectionType "$1" 2>/dev/null | grep -q -E 'equirectangular|cubemap' 2>/dev/null
    else return 1; fi
}

get_file_size() { [[ -f "$1" ]] && stat -f%z "$1" 2>/dev/null || echo "0"; }

get_adaptive_threshold() {
    local mime="$1" size="$2"
    case "$mime" in
        image/gif) if [[ $size -gt 2097152 ]]; then echo "20"; else echo "35"; fi;;
        image/png|image/bmp) echo "25";;
        video/*) echo "50";;
        *) echo "30";;
    esac
}

estimate_complexity() {
    local file="$1" mime=$(get_mime_type "$file")
    case "$mime" in
        image/gif)
            local frames=$(ffprobe -v quiet -select_streams v:0 -show_entries stream=nb_frames -of csv=p=0 "$file" 2>/dev/null || echo "1")
            if [[ $frames -gt 50 ]]; then echo "HIGH"; elif [[ $frames -gt 10 ]]; then echo "MEDIUM"; else echo "LOW"; fi;;
        image/png|image/bmp) echo "LOW";;
        *) echo "MEDIUM";;
    esac
}

backup_metadata() {
    if command -v exiftool >/dev/null 2>&1; then
        exiftool -TagsFromFile "$1" -all:all --icc_profile -overwrite_original "$2" >/dev/null 2>>"$CONVERSION_LOG" || log_message "WARN" "元数据迁移可能不完整: $(basename "$1")"
    fi
    local src_time=$(stat -f%m "$1" 2>/dev/null || echo "0")
    if [[ "$src_time" != "0" ]]; then
        touch -t "$(date -r "$src_time" "+%Y%m%d%H%M.%S")" "$2" 2>/dev/null || true
    fi
}

ensure_even_dimensions() {
    local input="$1" output="$2"
    local dimensions=$(ffprobe -v quiet -select_streams v:0 -show_entries stream=width,height -of csv=s=x:p=0 "$input" 2>/dev/null || echo "0x0")
    local width=$(echo "$dimensions" | cut -d'x' -f1) height=$(echo "$dimensions" | cut -d'x' -f2)
    if [[ "$width" =~ ^[0-9]+$ && "$height" =~ ^[0-9]+$ && $width -gt 0 && $height -gt 0 && ($((width % 2)) -ne 0 || $((height % 2)) -ne 0) ]]; then
        log_message "INFO" "调整奇数分辨率: ${width}x${height} -> $(basename "$input")"
        if ffmpeg_quiet -y -i "$input" -vf "pad=ceil(iw/2)*2:ceil(ih/2)*2" -c:a copy "$output" 2>>"$CONVERSION_LOG"; then
            echo "$output"
        else
            log_message "ERROR" "分辨率调整失败: $(basename "$input")"
            echo "$input"
        fi
    else echo "$input"; fi
}

generate_lossless_image() {
    local input="$1" output="$2"
    if is_animated "$input"; then
        if ! ffmpeg_quiet -y -i "$input" -c:v libsvtav1 -qp 0 -preset 8 -pix_fmt yuv420p "$output" 2>>"$CONVERSION_LOG"; then
            log_message "ERROR" "无损动态AVIF转换失败: $(basename "$input")"
            return 1
        fi
    else
        if ! magick "$input" -quality 100 "$output" >/dev/null 2>>"$CONVERSION_LOG"; then
            log_message "ERROR" "无损静态AVIF转换失败: $(basename "$input")"
            return 1
        fi
    fi; return 0
}

generate_first_lossy_image() {
    local input="$1" output="$2" mime=$(get_mime_type "$input")
    if is_animated "$input"; then
        local dimension_fixed_temp="$TEMP_DIR/fixed_lossy_$$.${input##*.}" input_file
        input_file=$(ensure_even_dimensions "$input" "$dimension_fixed_temp")
        if ffmpeg_quiet -y -i "$input_file" -c:v libsvtav1 -crf 30 -preset 8 -pix_fmt yuv420p "$output" 2>>"$CONVERSION_LOG"; then
            [[ "$input_file" != "$input" ]] && rm -f "$input_file"
            return 0
        fi
        [[ "$input_file" != "$input" ]] && rm -f "$input_file"
    else
        local quality=80
        case "$mime" in image/gif|image/png|image/bmp) quality=85;; image/jpeg) quality=75;; esac
        if magick "$input" -quality "$quality" "$output" >/dev/null 2>>"$CONVERSION_LOG"; then return 0; fi
    fi
    log_message "ERROR" "初步有损转换失败: $(basename "$input")"
    return 1
}

make_smart_decision() {
    local orig_size="$1" lossless_size="$2" lossy_size="$3" threshold="$4"
    if [[ $lossless_size -le 0 && $lossy_size -le 0 ]]; then echo "ERROR"; return; fi
    if [[ $lossless_size -gt 0 && $lossy_size -le 0 ]]; then echo "USE_LOSSLESS_SIGNIFICANT"; return; fi
    if [[ $lossy_size -gt 0 && $lossless_size -le 0 ]]; then
        if [[ $lossy_size -lt $((orig_size * 80 / 100)) ]]; then echo "USE_LOSSY_ACCEPTABLE"; else echo "EXPLORE_FURTHER"; fi
        return
    fi
    if [[ $lossless_size -lt $((orig_size / 5)) && $lossless_size -lt $((lossy_size / 2)) ]]; then
        echo "USE_LOSSLESS_EXTREME"; return
    fi
    local gap=0
    if [[ $orig_size -gt 0 ]]; then gap=$(( (lossy_size - lossless_size) * 100 / orig_size )); fi
    if [[ $lossless_size -lt $lossy_size && $gap -gt $threshold ]]; then
        echo "USE_LOSSLESS_SIGNIFICANT"
    elif [[ $lossless_size -lt $lossy_size ]]; then
        echo "USE_LOSSLESS_BETTER"
    elif [[ $lossy_size -lt $((orig_size * 80 / 100)) ]]; then
        echo "USE_LOSSY_ACCEPTABLE"
    else echo "EXPLORE_FURTHER"; fi
}

unified_smart_analysis_image() {
    local input="$1" temp_output_base="$2" original_size="$3"
    local mime=$(get_mime_type "$input") threshold=$(get_adaptive_threshold "$mime" "$original_size") complexity=$(estimate_complexity "$input")
    local lossless_file="${temp_output_base}_lossless.avif" first_lossy_file="${temp_output_base}_first.avif"
    generate_lossless_image "$input" "$lossless_file" & local lossless_pid=$!
    generate_first_lossy_image "$input" "$first_lossy_file" & local lossy_pid=$!
    wait $lossless_pid; local lossless_success=$?
    wait $lossy_pid; local lossy_success=$?
    local lossless_size=0; [[ $lossless_success -eq 0 && -f "$lossless_file" ]] && lossless_size=$(get_file_size "$lossless_file")
    local lossy_size=0; [[ $lossy_success -eq 0 && -f "$first_lossy_file" ]] && lossy_size=$(get_file_size "$first_lossy_file")
    
    local decision_tag=""
    if [[ "$MODE" == "quality" ]]; then
        if [[ $lossless_size -gt 0 && $lossless_size -lt $lossy_size ]]; then decision_tag="QUALITY_LOSSLESS_OPTIMAL"
        else decision_tag="QUALITY_LOSSLESS_FORCED"; fi
        rm -f "$first_lossy_file" 2>/dev/null
        if [[ -f "$lossless_file" && $lossless_size -gt 0 ]]; then
            local quality_type="AVIF-Quality"; [[ "$decision_tag" == "QUALITY_LOSSLESS_OPTIMAL" ]] && quality_type+="-Optimal"
            echo "$quality_type|${lossless_file}|${decision_tag}"; return 0
        fi
    else
        local decision=$(make_smart_decision "$original_size" "$lossless_size" "$lossy_size" "$threshold")
        case "$decision" in
            "USE_LOSSLESS_EXTREME"|"USE_LOSSLESS_BETTER"|"USE_LOSSLESS_SIGNIFICANT")
                rm -f "$first_lossy_file" 2>/dev/null
                if [[ -f "$lossless_file" && $lossless_size -gt 0 ]]; then
                    echo "AVIF-Lossless|${lossless_file}|SMART_LOSSLESS"; return 0
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
        if magick "$input" -quality "$q" "$test_file" >/dev/null 2>>"$CONVERSION_LOG" && [[ -f "$test_file" ]]; then
            local test_size=$(get_file_size "$test_file")
            if [[ $test_size -gt 0 && $test_size -lt $best_size ]]; then
                [[ -n "$best_file" ]] && rm -f "$best_file"
                best_file="$test_file"; best_size=$test_size; best_quality="AVIF-Q$q"
                if [[ $test_size -lt $((original_size * 60 / 100)) ]]; then break; fi
            else rm -f "$test_file"; fi
        fi
    done
    if [[ -n "$best_file" && -f "$best_file" && $best_size -lt $original_size ]]; then
        echo "$best_quality|${best_file}|SMART_LOSSY_EXPLORED"; return 0
    else
        [[ -n "$best_file" ]] && rm -f "$best_file"; return 1
    fi
}

continue_animated_exploration() {
    local input="$1" temp_output_base="$2" original_size="$3"
    local crf_levels=(40 50); local best_file="" best_size=$original_size best_crf=""
    local dimension_fixed_temp="$TEMP_DIR/fixed_explore_$$.${input##*.}"
    local input_file=$(ensure_even_dimensions "$input" "$dimension_fixed_temp")
    for crf in "${crf_levels[@]}"; do
        local test_file="$TEMP_DIR/test_vid_crf${crf}_$$.mp4"
        if ffmpeg_quiet -y -i "$input_file" -c:v libsvtav1 -crf "$crf" -preset 7 -g 240 -c:a copy -avoid_negative_ts make_zero "$test_file" 2>>"$CONVERSION_LOG"; then
            local new_size=$(get_file_size "$test_file")
            if [[ $new_size -gt 0 && $new_size -lt $best_size ]]; then
                [[ -n "$best_file" ]] && rm -f "$best_file"
                best_file="$test_file"; best_size=$new_size; best_crf="AV1-CRF$crf"
                if [[ $new_size -lt $((original_size * 50 / 100)) ]]; then break; fi
            else rm -f "$test_file"; fi
        fi
    done
    [[ "$input_file" != "$input" ]] && rm -f "$input_file"
    if [[ -n "$best_file" && -f "$best_file" ]]; then
        echo "$best_crf|${best_file}|SMART_LOSSY_EXPLORED"; return 0
    else return 1; fi
}

convert_video_quality_mode() {
    local input="$1" temp_output="$2"; local codec_opts=""
    if [[ $ENABLE_HW_ACCEL -eq 1 ]]; then
        codec_opts="-c:v hevc_videotoolbox -allow_sw 1 -q:v 0"
        if ffmpeg_quiet -y -i "$input" $codec_opts -c:a copy -movflags +faststart -avoid_negative_ts make_zero "$temp_output" 2>>"$CONVERSION_LOG"; then
            echo "HEVC-Quality(HW)|${temp_output}|QUALITY_ANALYSIS"; return 0
        fi
        log_message "WARN" "硬件HEVC无损加速失败，尝试软件编码: $(basename "$1")"
    fi
    codec_opts="-c:v libx265 -x265-params lossless=1"
    if ffmpeg_quiet -y -i "$input" $codec_opts -c:a copy -movflags +faststart -avoid_negative_ts make_zero "$temp_output" 2>>"$CONVERSION_LOG"; then
        echo "HEVC-Quality(SW)|${temp_output}|QUALITY_ANALYSIS"; return 0
    fi; return 1
}

convert_video_efficiency_mode() {
    local input="$1" temp_output_base="$2" original_size="$3"
    local lossless_file="${temp_output_base}_lossless.mov" first_lossy_file="${temp_output_base}_first.mp4"
    
    (ffmpeg_quiet -y -i "$input" -c:v libx265 -x265-params lossless=1 -c:a copy -movflags +faststart -avoid_negative_ts make_zero "$lossless_file" 2>>"$CONVERSION_LOG") &
    local lossless_pid=$!
    
    local dimension_fixed_temp="$TEMP_DIR/fixed_lossy_vid_$$.${input##*.}"
    local input_file=$(ensure_even_dimensions "$input" "$dimension_fixed_temp")
    (ffmpeg_quiet -y -i "$input_file" -c:v libsvtav1 -crf 32 -preset 7 -g 240 -c:a copy -avoid_negative_ts make_zero "$first_lossy_file" 2>>"$CONVERSION_LOG") &
    local lossy_pid=$!

    wait $lossless_pid; local lossless_success=$?
    wait $lossy_pid; local lossy_success=$?
    [[ "$input_file" != "$input" ]] && rm -f "$input_file"

    local lossless_size=0; [[ $lossless_success -eq 0 && -f "$lossless_file" ]] && lossless_size=$(get_file_size "$lossless_file")
    local lossy_size=0; [[ $lossy_success -eq 0 && -f "$first_lossy_file" ]] && lossy_size=$(get_file_size "$first_lossy_file")
    
    local threshold=$(get_adaptive_threshold "video/*" "$original_size")
    local decision=$(make_smart_decision "$original_size" "$lossless_size" "$lossy_size" "$threshold")
    
    case "$decision" in
        "USE_LOSSLESS_EXTREME"|"USE_LOSSLESS_BETTER"|"USE_LOSSLESS_SIGNIFICANT")
            rm -f "$first_lossy_file" 2>/dev/null
            if [[ -f "$lossless_file" && $lossless_size -gt 0 ]]; then
                echo "HEVC-Lossless|${lossless_file}|SMART_LOSSLESS"; return 0
            fi;;
        "USE_LOSSY_ACCEPTABLE")
            rm -f "$lossless_file" 2>/dev/null
            if [[ -f "$first_lossy_file" && $lossy_size -gt 0 ]]; then
                echo "AV1-CRF32|${first_lossy_file}|SMART_LOSSY"; return 0
            fi;;
        "EXPLORE_FURTHER")
            rm -f "$lossless_file" "$first_lossy_file" 2>/dev/null
            local second_lossy_file="${temp_output_base}_second.mp4"
            local further_input_file=$(ensure_even_dimensions "$input" "$dimension_fixed_temp")
            if ffmpeg_quiet -y -i "$further_input_file" -c:v libsvtav1 -crf 45 -preset 7 -g 240 -c:a copy -avoid_negative_ts make_zero "$second_lossy_file" 2>>"$CONVERSION_LOG"; then
                [[ "$further_input_file" != "$input" ]] && rm -f "$further_input_file"
                echo "AV1-CRF45|${second_lossy_file}|SMART_LOSSY_EXPLORED"; return 0
            fi
            [[ "$further_input_file" != "$input" ]] && rm -f "$further_input_file";;
    esac
    rm -f "$lossless_file" "$first_lossy_file" 2>/dev/null; return 1
}

should_skip_file() {
    local file="$1"; local basename=$(basename "$file")
    if is_live_photo "$file" || is_spatial_image "$file"; then
        log_message "INFO" "跳过特殊图片 (Live Photo/空间图片): $basename"; return 0
    fi
    local mime=$(get_mime_type "$file"); local target_ext
    if [[ "$mime" == "unknown" ]]; then log_message "INFO" "跳过未知MIME类型: $basename"; return 0; fi
    case "$mime" in
        image/*) target_ext="avif";;
        video/*) target_ext=$([[ "$MODE" == "quality" ]] && echo "mov" || echo "mp4");;
        *) log_message "INFO" "跳过不支持的MIME类型: $basename ($mime)"; return 0;;
    esac
    if [[ "${file##*.}" == "$target_ext" ]]; then log_message "INFO" "文件已是目标格式: $basename"; return 0; fi
    local target_filename="${file%.*}.$target_ext"
    if [[ -f "$target_filename" && "$file" != "$target_filename" ]]; then
        log_message "INFO" "跳过，目标文件已存在: $(basename "$target_filename")"; return 0
    fi; return 1
}

process_file() {
    local file="$1"; local basename=$(basename "$file")
    log_message "INFO" "开始处理: $basename"
    local result_filename=$(echo -n "$file" | shasum | awk '{print $1}')
    local result_file="$RESULTS_DIR/$result_filename"
    local original_size=$(get_file_size "$file")
    local mime=$(get_mime_type "$file")
    local temp_output_base="$TEMP_DIR/conv_$$_$(basename "$file" | tr ' ' '_')"
    
    local result; local quality_stat; local temp_file; local decision_tag="NONE"

    if [[ "$mime" == video/* ]]; then
        if [[ "$MODE" == "quality" ]]; then
            result=$(convert_video_quality_mode "$file" "${temp_output_base}.mov")
        else
            result=$(convert_video_efficiency_mode "$file" "$temp_output_base" "$original_size")
        fi
    else
        result=$(unified_smart_analysis_image "$file" "$temp_output_base" "$original_size")
    fi
    
    if [[ -n "$result" ]]; then
        quality_stat=$(echo "$result" | cut -d'|' -f1)
        temp_file=$(echo "$result" | cut -d'|' -f2)
        decision_tag=$(echo "$result" | cut -d'|' -f3)
        local new_size=$(get_file_size "$temp_file")
        if [[ $new_size -gt 0 ]]; then
            local should_replace=0 size_change_type=""
            if [[ "$MODE" == "quality" ]]; then
                should_replace=1
                if [[ $new_size -lt $original_size ]]; then size_change_type="REDUCED"
                elif [[ $new_size -gt $original_size ]]; then size_change_type="INCREASED"
                else size_change_type="UNCHANGED"; fi
            else
                if [[ $new_size -lt $original_size ]]; then
                    should_replace=1; size_change_type="REDUCED"
                elif [[ $new_size -eq $original_size ]]; then
                    should_replace=1; size_change_type="UNCHANGED"
                else
                    size_change_type="INCREASED"
                fi
            fi
            if [[ $should_replace -eq 1 ]]; then
                backup_metadata "$file" "$temp_file"
                local target_file="${file%.*}.${temp_file##*.}"
                mv "$temp_file" "$target_file"
                if [[ "$file" != "$target_file" ]]; then rm -f "$file"; fi
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
    rm -f "$temp_output_base"* 2>/dev/null
}

find_media_files() { find "$1" -type f -print0; }

aggregate_results() {
    if [ ! -d "$RESULTS_DIR" ] || [ -z "$(ls -A "$RESULTS_DIR")" ]; then return; fi
    local awk_output
    awk_output=$(cat "$RESULTS_DIR"/* | awk -F'|' '
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
            if (size_change == "REDUCED") {
                reduced++;
                saved += orig - new;
            } else if (size_change == "INCREASED") {
                increased++;
                increased_sum += new - orig;
            } else if (size_change == "UNCHANGED") {
                unchanged++;
            }
            quality_stats[$4]++;
            
            decision = $5;
            if (decision == "SMART_LOSSLESS") {
                smart_decisions++;
                lossless_wins++;
            } else if (decision == "SMART_LOSSY" || decision == "SMART_LOSSY_EXPLORED") {
                smart_decisions++;
            } else if (decision == "QUALITY_ANALYSIS") {
                quality_analysis++;
            } else if (decision == "QUALITY_LOSSLESS_OPTIMAL") {
                quality_analysis++;
                lossless_wins++;
            } else if (decision == "QUALITY_LOSSLESS_FORCED") {
                quality_analysis++;
            }
        }
        $1 == "FAIL" {
            fail++;
            print "failed_file:" $2;
        }
        $1 == "SKIP" {
            skip++;
        }
        END {
            print "SUCCESS_COUNT=" success;
            print "FAIL_COUNT=" fail;
            print "SKIP_COUNT=" skip;
            print "SIZE_REDUCED=" reduced;
            print "SIZE_INCREASED=" increased;
            print "SIZE_UNCHANGED=" unchanged;
            print "TOTAL_SAVED=" saved;
            print "TOTAL_SIZE_INCREASED_SUM=" increased_sum;
            print "SMART_DECISIONS_COUNT=" smart_decisions;
            print "LOSSLESS_WINS_COUNT=" lossless_wins;
            print "QUALITY_ANALYSIS_COUNT=" quality_analysis;
            for (stat in quality_stats) {
                print "quality_stat:" stat ":" quality_stats[stat];
            }
        }
    ')
    while IFS= read -r line; do
        if [[ "$line" == *=* ]]; then
            eval "$line"
        elif [[ "$line" == failed_file:* ]]; then
            FAILED_FILES+=("$(echo "$line" | cut -d: -f2-)")
        elif [[ "$line" == quality_stat:* ]]; then
            stat_name=$(echo "$line" | cut -d: -f2)
            stat_count=$(echo "$line" | cut -d: -f3)
            for ((i=0; i<stat_count; i++)); do
                QUALITY_STATS+=("$stat_name")
            done
        fi
    done <<< "$awk_output"
}

generate_report() {
    local total=$((SUCCESS_COUNT + FAIL_COUNT + SKIP_COUNT))
    local success_pct=0; [[ $total -gt 0 ]] && success_pct=$(awk -v s="$SUCCESS_COUNT" -v t="$total" 'BEGIN {printf "%.0f", s/t*100}')
    
    if [[ $ALL_FILES_COUNT -gt 0 && $ALL_FILES_COUNT -eq $SKIP_COUNT ]]; then
        (
        echo -e "${BOLD}${COLOR_BLUE}📊 ================= 媒体转换最终报告 =================${RESET}"
        echo
        echo -e "${DIM}${COLOR_LIGHT_GRAY}📁 目录: ${TARGET_DIR}${RESET}"
        echo -e "${DIM}${COLOR_LIGHT_GRAY}⚙️ 模式: ${MODE}${RESET}    ${DIM}${COLOR_LIGHT_GRAY}🚀 版本: ${VERSION}${RESET}"
        echo -e "${DIM}${COLOR_LIGHT_GRAY}⏰ 完成: $(date)${RESET}"
        echo
        echo -e "${BOLD}${COLOR_CYAN}--- 概览 ---${RESET}"
        echo -e "  ${COLOR_VIOLET}总计扫描: ${ALL_FILES_COUNT} 文件${RESET}"
        echo -e "  ${COLOR_YELLOW}⚠️  所有文件 (${SKIP_COUNT}) 均被主动跳过。${RESET}"
        echo -e "  ${DIM}${COLOR_SUBTLE}（原因：已是目标格式或属于 Live Photo / 空间图片等特殊类型）${RESET}"
        echo
        echo -e "------------------------------------------"
        echo -e "${DIM}${COLOR_LIGHT_GRAY}📄 详细日志: ${CONVERSION_LOG}${RESET}"
        echo
        echo -e "${DIM}${COLOR_SUBTLE}🎉 太棒了! 目标目录下的所有文件都已是最佳状态，无需处理。${RESET}"
        echo -e "${DIM}${COLOR_SUBTLE}✨ 最终成功率: 100.0%% (基于智能跳过实现)${RESET}"
        ) > "$REPORT_FILE"
        return
    fi

    local quality_summary=$(printf "%s\n" "${QUALITY_STATS[@]}" | sort | uniq -c | sort -rn | awk '{printf "%s(%s) ", $2, $1}' || echo "无")
    local saved_space_str increased_space_str
    if command -v numfmt >/dev/null; then
        saved_space_str=$(numfmt --to=iec-i --suffix=B --format="%.2f" "$TOTAL_SAVED" 2>/dev/null || echo "0.00 B")
        increased_space_str=$(numfmt --to=iec-i --suffix=B --format="%.2f" "$TOTAL_SIZE_INCREASED_SUM" 2>/dev/null || echo "0.00 B")
    else
        saved_space_str="$TOTAL_SAVED B"
        increased_space_str="$TOTAL_SIZE_INCREASED_SUM B"
    fi
    
    local net_saved=$((TOTAL_SAVED - TOTAL_SIZE_INCREASED_SUM))
    local net_saved_str
    if command -v numfmt >/dev/null; then
        net_saved_str=$(numfmt --to=iec-i --suffix=B --format="%.2f" "$net_saved" 2>/dev/null || echo "0.00 B")
    else
        net_saved_str="$net_saved B"
    fi
    
    (
    echo -e "${BOLD}${COLOR_BLUE}📊 ================= 媒体转换最终报告 =================${RESET}"
    echo
    echo -e "${DIM}${COLOR_LIGHT_GRAY}📁 目录: ${TARGET_DIR}${RESET}"
    echo -e "${DIM}${COLOR_LIGHT_GRAY}⚙️ 模式: ${MODE}${RESET}    ${DIM}${COLOR_LIGHT_GRAY}🚀 版本: ${VERSION}${RESET}"
    echo -e "${DIM}${COLOR_LIGHT_GRAY}⏰ 完成: $(date)${RESET}"
    echo
    echo -e "${BOLD}${COLOR_CYAN}--- 概览 ---${RESET}"
    echo -e "  ${COLOR_VIOLET}总计扫描: ${ALL_FILES_COUNT} 文件${RESET}"
    echo -e "  ${COLOR_SUCCESS}✅ 成功转换: ${SUCCESS_COUNT} (${success_pct}%%)${RESET}"
    echo -e "  ${COLOR_ERROR}❌ 转换失败: ${FAIL_COUNT}${RESET}"
    echo -e "  ${DIM}${COLOR_SUBTLE}⏩ 主动跳过: ${SKIP_COUNT}${RESET}"
    echo
    
    if [[ "$MODE" == "efficiency" ]]; then
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
    echo -e "${DIM}${COLOR_LIGHT_GRAY}📄 详细日志: ${CONVERSION_LOG}${RESET}"
    ) > "$REPORT_FILE"

    if [[ ${#FAILED_FILES[@]} -gt 0 ]]; then
        echo -e "\n${COLOR_ERROR}${BOLD}❌ 失败文件列表:${RESET}" >> "$REPORT_FILE"
        printf "  • %s\n" "${FAILED_FILES[@]}" >> "$REPORT_FILE"
    fi
}

show_progress() {
    local current=$1 total=${2:-0}
    if [[ $total -eq 0 ]]; then return; fi
    local pct=$(( current * 100 / total ))
    local term_width=$(tput cols 2>/dev/null || echo 80)
    local width=50; if [[ $term_width -lt 80 ]]; then width=30; fi
    local filled_len=$(( width * pct / 100 ))
    local bar=$(printf "%${filled_len}s" | tr ' ' '█')
    local empty=$(printf "%$((width - filled_len))s" | tr ' ' '░')
    
    local emojis=("⳿" "⌛" "⚙️" "🚀")
    local emoji_index=$(( current % 4 ))
    
    echo -en "${CLEAR_LINE}[${COLOR_INFO}${bar}${RESET}${DIM}${empty}${RESET}] ${BOLD}${pct}%%${RESET} (${COLOR_HIGHLIGHT}${current}${RESET}/${COLOR_HIGHLIGHT}${total}${RESET}) ${emojis[$emoji_index]}"
}

update_progress() {
    local completed=$(find "$RESULTS_DIR" -name "*" -type f 2>/dev/null | wc -l | tr -d ' ')
    show_progress "$completed" "$ALL_FILES_COUNT"
    if [[ $((completed % 50)) -eq 0 ]]; then flush_log_buffer; fi
}

run_file_processing() {
    if should_skip_file "$1"; then
        local result_filename=$(echo -n "$1" | shasum | awk '{print $1}')
        echo "SKIP|$(basename "$1")" > "$RESULTS_DIR/$result_filename"
    else 
        process_file "$1"
    fi
}

main_conversion_loop() {
    echo -e "  ${BOLD}${COLOR_PROMPT}🔎 [1/3]${RESET} 扫描媒体文件...${RESET}"
    ALL_FILES_COUNT=$(find "$TARGET_DIR" -type f -print0 | grep -zc . || true)
    if [[ $ALL_FILES_COUNT -eq 0 ]]; then
        echo -e "${COLOR_YELLOW}⚠️ 无效的目录或未发现媒体文件。${RESET}"
        return 1
    fi
    echo -e "  发现 ${COLOR_VIOLET}${ALL_FILES_COUNT}${RESET} 个文件，准备启动...🚀"
    echo -e "  ${BOLD}${COLOR_PROMPT}⚙️ [2/3]${RESET} 开始统一智能转换 (并发数: ${COLOR_BLUE}${CONCURRENT_JOBS}${RESET})..."
    echo -e "${DIM}${COLOR_SUBTLE}  提示: 随时按 ${COLOR_RED}Ctrl+C${DIM}${COLOR_SUBTLE} 可中断任务并进行清理...${RESET}"
    export -f log_message get_mime_type is_animated is_live_photo is_spatial_image get_file_size backup_metadata ensure_even_dimensions
    export -f get_adaptive_threshold estimate_complexity generate_lossless_image generate_first_lossy_image make_smart_decision
    export -f unified_smart_analysis_image continue_lossy_exploration continue_static_exploration continue_animated_exploration
    export -f convert_video_quality_mode convert_video_efficiency_mode should_skip_file process_file run_file_processing
    export -f ffmpeg_quiet
    export MODE ENABLE_HW_ACCEL CONVERSION_LOG TEMP_DIR RESULTS_DIR
    ( while true; do if ! pgrep -f "xargs -0 -P $CONCURRENT_JOBS" > /dev/null; then break; fi; update_progress; sleep 0.2; done ) & local progress_pid=$!
    find_media_files "$TARGET_DIR" | xargs -0 -P "$CONCURRENT_JOBS" -I {} bash -c 'run_file_processing "$@"' _ {}
    kill "$progress_pid" 2>/dev/null || true; wait "$progress_pid" 2>/dev/null || true
    echo -e "${CLEAR_LINE}"
    echo -e "  ${BOLD}${COLOR_PROMPT}✅ [2/3]${RESET} ${COLOR_SUCCESS}所有文件处理完成${RESET}"
    echo -e "  ${BOLD}${COLOR_PROMPT}📊 [3/3]${RESET} 正在汇总结果并生成报告...${RESET}"
    flush_log_buffer
}

show_help() {
    cat << EOF
${BOLD}${COLOR_BLUE}🚀 媒体批量转换脚本 v$VERSION (统一智能分析引擎)${RESET}
${DIM}${COLOR_SUBTLE}（专注于UI和交互体验的终极稳定版本）${RESET}
用法: $0 [选项] <目录路径>
${BOLD}${COLOR_CYAN}选项:${RESET}
  --mode <type>     转换模式: '${COLOR_GREEN}quality${RESET}' (质量优先) 或 '${COLOR_ORANGE}efficiency${RESET}' (高效压缩)
  --jobs <N>        并发任务数 (默认: 系统CPU核心数*75%)
  --no-hw-accel     禁用硬件加速
  --help            显示此帮助信息
${BOLD}${COLOR_CYAN}核心特性:${RESET}
  • ${COLOR_SUCCESS}极限压缩稳定转换内核${RESET} - 基于验证版本的核心算法
  • ${COLOR_BLUE}智能双路径分析${RESET} - 无损vs有损自动决策
  • ${COLOR_YELLOW}自适应质量控制${RESET} - 基于文件类型和复杂度
  • ${DIM}${COLOR_SUBTLE}优化资源管理${RESET} - 改进的内存和并发控制
${BOLD}${COLOR_CYAN}版本更新:${RESET}
  ✨ 修复了并行处理下智能决策统计不准确的问题
  ✨ 增强了效率模式，对视频也采用无损(HEVC) vs 有损(AV1)对比决策
  ✨ 优化了效率模式的决策模型，以更好地识别无损优势场景
  ✨ 日志文件名现在包含转换模式，便于管理
  ✨ 完整的大小变化统计：减小/增大/不变分类显示
EOF
}

parse_arguments() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --mode) MODE="$2"; shift 2;;
            --jobs) CONCURRENT_JOBS="$2"; shift 2;;
            --no-hw-accel) ENABLE_HW_ACCEL=0; shift;;
            --help) show_help; exit 0;;
            -*) printf "${COLOR_RED}❌ 未知选项:\033[0m %s\n" "$1"; show_help; exit 1;;
            *) if [[ -z "$TARGET_DIR" ]]; then TARGET_DIR="$1"; fi; shift;;
        esac
    done
}

interactive_mode() {
    echo -e "${BOLD}${COLOR_PROMPT}🚀 欢迎使用媒体批量转换脚本 ${COLOR_SUCCESS}v%s${RESET}" "$VERSION"
    echo -e "${DIM}${COLOR_SUBTLE}统一智能分析引擎 - 专业、高效、稳定${RESET}"
    echo -e "====================================================\n"
    while [[ -z "${TARGET_DIR:-}" || ! -d "$TARGET_DIR" ]]; do
        echo -e "  ${BOLD}${COLOR_PROMPT}请将目标文件夹拖拽至此, 然后按 Enter: ${RESET}\c"
        read -r TARGET_DIR
        TARGET_DIR=$(echo "$TARGET_DIR" | sed "s/^'//;s/'$//;s/^\"//;s/\"$//;s/\\\\ *$//")
        if [[ -z "$TARGET_DIR" || ! -d "$TARGET_DIR" ]]; then
            echo -e "${CLEAR_LINE}${COLOR_YELLOW}⚠️ 无效的目录，请重新输入。${RESET}"
        fi
    done
    if [[ -z "${MODE:-}" ]]; then
        echo -e "\n  ${BOLD}${COLOR_PROMPT}请选择转换模式: ${RESET}"
        echo -e "  ${COLOR_GREEN}[1]${RESET} ${BOLD}质量模式 (Quality)  ${DIM}${COLOR_SUBTLE}- 保证最高画质，推荐用于存档${RESET}"
        echo -e "  ${COLOR_ORANGE}[2]${RESET} ${BOLD}效率模式 (Efficiency) ${DIM}${COLOR_SUBTLE}- 寻求最佳压缩比，推荐日常使用${RESET}"
        local mode_choice
        echo -e "  ${DIM}${COLOR_SUBTLE}[默认] 按回车键选择${RESET} ${COLOR_ORANGE}效率模式${RESET}\n  请输入您的选择 (1 或 2): \c"
        read -r mode_choice
        case "$mode_choice" in 1) MODE="quality";; *) MODE="efficiency";; esac
    fi
}

validate_inputs() {
    if [[ -z "${TARGET_DIR:-}" || ! -d "$TARGET_DIR" ]]; then return 1; fi
    TARGET_DIR=$(cd "$TARGET_DIR" && pwd)
    if [[ "$MODE" != "quality" && "$MODE" != "efficiency" ]]; then return 1; fi
    return 0
}

check_dependencies() {
    local deps=("ffmpeg" "magick" "exiftool" "ffprobe" "file" "stat" "shasum" "awk" "tput")
    local missing_deps=()
    for dep in "${deps[@]}"; do
        if ! command -v "$dep" >/dev/null; then missing_deps+=("$dep"); fi
    done
    if [[ ${#missing_deps[@]} -gt 0 ]]; then
        echo -e "${COLOR_RED}❌ 错误: 缺少以下依赖命令：${RESET}"
        echo -e "  • ${missing_deps[@]}"
        echo -e "\n${COLOR_YELLOW}💡 在 macOS 上安装依赖：${RESET}"
        echo -e "  ${COLOR_BLUE}brew install ffmpeg imagemagick exiftool${RESET}"
        exit 1
    fi
    if ! ffmpeg -encoders 2>/dev/null | grep -q libsvtav1; then
        echo -e "${COLOR_YELLOW}⚠️ 警告: ffmpeg 未支持 libsvtav1 编码器。${RESET}"
        echo -e "${DIM}${COLOR_SUBTLE}部分高效视频转换功能可能不可用。请尝试更新您的 ffmpeg。${RESET}"
    fi
}

execute_conversion_task() {
    SUCCESS_COUNT=0; FAIL_COUNT=0; SKIP_COUNT=0
    SIZE_REDUCED=0; SIZE_INCREASED=0; SIZE_UNCHANGED=0
    TOTAL_SAVED=0; TOTAL_SIZE_INCREASED_SUM=0
    SMART_DECISIONS_COUNT=0; LOSSLESS_WINS_COUNT=0; QUALITY_ANALYSIS_COUNT=0
    FAILED_FILES=(); QUALITY_STATS=(); LOG_BUFFER=()
    
    TEMP_DIR=$(mktemp -d); RESULTS_DIR="$TEMP_DIR/results"; mkdir -p "$RESULTS_DIR"
    init_logging
    log_message "INFO" "转换任务启动 - 目录: $TARGET_DIR, 模式: $MODE, 版本: $VERSION"
    
    main_conversion_loop
    if [[ $? -ne 0 ]]; then return; fi
    
    aggregate_results
    generate_report
    
    echo -e "\n${COLOR_SUCCESS}${BOLD}================== 全部任务完成 ✅ ==================${RESET}\n"
    cat "$REPORT_FILE"
    echo
    
    if [[ $ALL_FILES_COUNT -gt 0 && $ALL_FILES_COUNT -eq $SKIP_COUNT ]]; then
        :
    elif [[ "$MODE" == "quality" && $LOSSLESS_WINS_COUNT -gt 0 ]]; then
        echo -e "${DIM}${COLOR_SUBTLE}💡 提示: 质量模式下，智能分析为 ${COLOR_HIGHLIGHT}${LOSSLESS_WINS_COUNT}${DIM}${COLOR_SUBTLE} 个文件选择了最优的无损方案。${RESET}"
    elif [[ "$MODE" == "efficiency" && $SMART_DECISIONS_COUNT -gt 0 ]]; then
        echo -e "${DIM}${COLOR_SUBTLE}💡 提示: 效率模式下，智能引擎为 ${COLOR_HIGHLIGHT}${SMART_DECISIONS_COUNT}${DIM}${COLOR_SUBTLE} 个文件找到了最佳平衡点。${RESET}"
    fi
    
    local success_rate=0
    if [[ $ALL_FILES_COUNT -gt 0 ]]; then
        local effective_total=$((ALL_FILES_COUNT - SKIP_COUNT))
        if [[ $effective_total -gt 0 ]]; then
            success_rate=$(awk -v s="$SUCCESS_COUNT" -v t="$effective_total" 'BEGIN {printf "%.1f", s/t*100}')
        elif [[ $SUCCESS_COUNT -eq 0 ]]; then
            success_rate="100.0"
        fi
    fi
    echo -e "${DIM}${COLOR_SUBTLE}✨ 最终成功率: ${COLOR_SUCCESS}${success_rate}%%${RESET} ${DIM}${COLOR_SUBTLE}(基于稳定内核实现)${RESET}"
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
        echo -e "  ${DIM}${COLOR_LIGHT_GRAY}目标:${RESET} ${COLOR_BLUE}${TARGET_DIR}${RESET}"
        echo -e "  ${DIM}${COLOR_LIGHT_GRAY}模式:${RESET} ${BOLD}${COLOR_HIGHLIGHT}${MODE}${RESET}"
        echo -e "  ${DIM}${COLOR_LIGHT_GRAY}并发:${RESET} ${COLOR_VIOLET}${CONCURRENT_JOBS}${RESET}"
        
        local accel_status
        if [[ $ENABLE_HW_ACCEL -eq 1 ]]; then
            accel_status="${COLOR_SUCCESS}启用 ✅${RESET}"
        else
            accel_status="${COLOR_YELLOW}禁用 ❌${RESET}"
        fi
        echo -e "  ${DIM}${COLOR_LIGHT_GRAY}加速:${RESET} ${accel_status}"
        
        echo -e "  ${DIM}${COLOR_LIGHT_GRAY}内核:${RESET} ${COLOR_GREEN}稳定版转换引擎${RESET}"
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
    if [[ $# -eq 0 ]]; then interactive_session_loop
    else parse_arguments "$@"
        if ! validate_inputs; then show_help; exit 1; fi
        execute_conversion_task
    fi
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then main "$@"; fi