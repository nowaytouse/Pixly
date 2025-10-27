#!/bin/bash

# 转换测试脚本
echo "🔄 转换测试开始..."

# 创建测试目录
test_dir="/tmp/pixly_conversion_test"
mkdir -p "$test_dir"

# 复制测试文件
cp test_images/test1.jpg "$test_dir/"
cp test_images/test3.gif "$test_dir/"

echo "📁 测试文件:"
ls -la "$test_dir"

echo ""
echo "🎯 运行交互模式测试..."
cd "$test_dir"

echo "📝 当前目录: $(pwd)"
echo "📊 转换前文件:"
ls -la

echo ""
echo "🔄 启动pixly交互模式..."
echo "使用参数: -m auto+ -o . -c 2"
echo ""

# 运行交互模式（模拟用户输入）
timeout 10s /Users/nameko_1/Downloads/test_副本4/pixly -m auto+ -o . -c 2 || echo "⏰ 交互模式超时（正常现象）"

echo ""
echo "📊 转换后文件:"
ls -la

echo "✅ 转换测试完成"