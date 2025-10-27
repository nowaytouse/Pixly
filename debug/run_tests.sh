#!/bin/bash

# Pixly测试运行脚本

echo "========================================="
echo "Pixly测试套件"
echo "========================================="

# 运行所有测试
echo "运行所有测试..."
cd ..
go test ./core/converter/... -v

if [ $? -ne 0 ]; then
    echo "❌ 测试执行失败!"
    exit 1
fi

echo "✅ 所有测试执行完成!"