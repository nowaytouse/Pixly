#!/bin/bash

# 合规确认：我将优先考虑复用而非创建
# 基于现有CLI架构的真实转换验证测试

echo "🎯 合规验证测试 - 使用现有CLI架构"
echo "=================================="

# 测试路径配置
test_source="/Users/nameko_1/Downloads/test_副本4/debug/test_package/safe_copy_AI测试_03_自动模式+_不同格式测试合集_测试运行_副本_1756431698"
test_workdir="/tmp/pixly_compliance_test"

# 清理并准备测试环境
rm -rf "$test_workdir"
cp -r "$test_source" "$test_workdir"

echo "📋 初始状态统计:"
echo "原始格式文件: $(find "$test_workdir" -type f \( -name "*.jpg" -o -name "*.jpeg" -o -name "*.png" -o -name "*.gif" -o -name "*.webp" \) | wc -l)"
echo "AVIF格式文件: $(find "$test_workdir" -type f -name "*.avif" | wc -l)"

echo "📝 待转换文件详情:"
find "$test_workdir" -type f \( -name "*.jpg" -o -name "*.jpeg" -o -name "*.png" -o -name "*.gif" -o -name "*.webp" \) -exec basename {} \; | sort

# 使用现有CLI架构执行转换
echo "🔄 执行实际转换 (使用convert子命令)..."
cd /Users/nameko_1/Downloads/test_副本4

# 执行转换并捕获详细日志
./pixly convert "$test_workdir" --mode auto+ --verbose --concurrent 4 2>&1 | tee /tmp/pixly_conversion.log

# 检查转换结果
conversion_exit_code=$?

echo "📊 转换后状态验证:"
remaining_original=$(find "$test_workdir" -type f \( -name "*.jpg" -o -name "*.jpeg" -o -name "*.png" -o -name "*.gif" -o -name "*.webp" \) | wc -l)
new_avif=$(find "$test_workdir" -type f -name "*.avif" | wc -l)

echo "剩余原始格式文件: $remaining_original"
echo "新AVIF格式文件: $new_avif"

# 验证转换完整性
echo "🔍 转换完整性检查:"
if [ "$conversion_exit_code" -eq 0 ]; then
    echo "✅ CLI程序正常退出"
else
    echo "❌ CLI程序异常退出 (exit code: $conversion_exit_code)"
fi

# 检查AVIF文件有效性
if [ "$new_avif" -gt 0 ]; then
    echo "🔍 验证AVIF文件完整性..."
    find "$test_workdir" -type f -name "*.avif" -exec ffprobe -v error -select_streams v:0 -show_entries stream=codec_name -of csv=p=0 {} \; 2>/dev/null | head -5
fi

# 最终验证结果
if [ "$remaining_original" -eq 0 ] && [ "$new_avif" -gt 0 ]; then
    echo "✅ 合规验证通过: 所有原始文件已成功转换并清理"
    
    # 清理测试数据
    rm -rf "$test_workdir"
    exit 0
else
    echo "❌ 合规验证失败: 转换不完整"
    echo "📋 未处理的文件:"
    find "$test_workdir" -type f \( -name "*.jpg" -o -name "*.jpeg" -o -name "*.png" -o -name "*.gif" -o -name "*.webp" \) | head -10
    
    # 保留测试数据用于调试
    echo "🐛 测试数据保留在: $test_workdir"
    exit 1
fi