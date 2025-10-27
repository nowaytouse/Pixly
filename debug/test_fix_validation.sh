#!/bin/bash

# Pixly媒体转换引擎 - 修复验证脚本
# 用于验证修复后的程序在三个测试路径上的有效性

echo "🎯 Pixly媒体转换引擎 - 修复验证脚本"
echo "=================================="

# 测试路径
TEST_PATHS=(
    "/Users/nameko_1/Downloads/test_副本4/debug/test_package/safe_copy_AI测试_03_自动模式+_不同格式测试合集_测试运行_副本_1756431698"
    "/Users/nameko_1/Downloads/test_副本4/debug/test_package/safe_copy_AI测试_04_表情包模式_🆕_Avif动图和表情包测试使用_MuseDash_三人日常_2.0_📁_测フォ_Folder_Name_副本_1756431879"
    "/Users/nameko_1/Downloads/test_副本4/debug/test_package/safe_copy_AI测试_05_自动模式+_🆕测试大量转换和嵌套文件夹_自动模式_应当仅使用副本_📁_测フォ_Folder_Name_1756432059"
)

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 验证函数
validate_test() {
    local test_path="$1"
    local test_name="$2"
    
    echo -e "\n${YELLOW}🔍 测试路径: $test_name${NC}"
    echo "路径: $test_path"
    
    # 检查路径是否存在
    if [[ ! -d "$test_path" ]]; then
        echo -e "${RED}❌ 测试路径不存在${NC}"
        return 1
    fi
    
    # 统计原始文件
    local original_count=$(find "$test_path" -type f \( -name "*.jpg" -o -name "*.jpeg" -o -name "*.png" -o -name "*.gif" -o -name "*.webp" -o -name "*.avif" -o -name "*.jxl" \) | wc -l)
    echo "原始媒体文件数量: $original_count"
    
    # 运行转换
    echo "🔄 执行转换..."
    cd /Users/nameko_1/Downloads/test_副本4
    
    # 使用修复后的程序进行测试 - 修复CLI参数格式
    ./pixly -m auto+ -o "$test_path" --in-place --format=avif --verbose "$test_path"
    
    local exit_code=$?
    
    # 验证结果
    local converted_count=$(find "$test_path" -type f -name "*.avif" | wc -l)
    local remaining_original=$(find "$test_path" -type f \( -name "*.jpg" -o -name "*.jpeg" -o -name "*.png" -o -name "*.gif" -o -name "*.webp" \) | wc -l)
    
    echo "转换后AVIF文件数量: $converted_count"
    echo "剩余原始文件数量: $remaining_original"
    
    # 验证AVIF文件完整性
    local valid_avif=0
    while IFS= read -r -d '' file; do
        if ffprobe -v error -select_streams v:0 -show_entries stream=width,height -of csv=p=0 "$file" >/dev/null 2>&1; then
            ((valid_avif++))
        else
            echo -e "${RED}❌ 无效的AVIF文件: $file${NC}"
        fi
    done < <(find "$test_path" -type f -name "*.avif" -print0)
    
    echo "有效AVIF文件数量: $valid_avif"
    
    # 结果判断
    if [[ $exit_code -eq 0 && $converted_count -gt 0 && $remaining_original -eq 0 && $valid_avif -eq $converted_count ]]; then
        echo -e "${GREEN}✅ 测试通过${NC}"
        return 0
    else
        echo -e "${RED}❌ 测试失败${NC}"
        return 1
    fi
}

# 主测试流程
echo "🚀 开始验证修复效果..."

passed=0
total=0

for i in "${!TEST_PATHS[@]}"; do
    test_path="${TEST_PATHS[$i]}"
    test_name="测试$(($i+1))"
    
    validate_test "$test_path" "$test_name"
    if [[ $? -eq 0 ]]; then
        ((passed++))
    fi
    ((total++))
done

echo -e "\n${YELLOW}📊 验证总结${NC}"
echo "================"
echo "总测试数: $total"
echo "通过数: $passed"
echo "失败数: $(($total - $passed))"

if [[ $passed -eq $total ]]; then
    echo -e "${GREEN}🎉 所有测试通过！修复有效${NC}"
    exit 0
else
    echo -e "${RED}⚠️  部分测试失败，需要进一步修复${NC}"
    exit 1
fi