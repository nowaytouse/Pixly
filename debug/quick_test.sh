#!/bin/bash

# 快速功能测试脚本
echo "🚀 快速功能测试开始..."

# 创建简单测试目录
test_dir="/tmp/pixly_quick_test"
mkdir -p "$test_dir"

# 复制测试文件
cp test_images/test1.jpg "$test_dir/"
cp test_images/test3.gif "$test_dir/"

echo "📁 测试文件准备完成"
echo "测试目录: $test_dir"
echo "包含文件:"
ls -la "$test_dir"

echo ""
echo "🎯 开始测试pixly分析功能..."
./pixly analyze "$test_dir" 2>&1

echo ""
echo "✅ 快速测试完成"