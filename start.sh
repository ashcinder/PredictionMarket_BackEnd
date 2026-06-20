#!/bin/bash

# PredictionMarket 后端启动脚本

# 检查 config.xml 文件是否存在
if [ ! -f config.xml ]; then
    echo "❌ 错误：未找到 config.xml 文件！"
    echo "📝 请先复制 config.example.xml 为 config.xml 并填入配置："
    echo "   cp config.example.xml config.xml"
    echo "   然后编辑 config.xml 文件填入你的私钥"
    exit 1
fi

# 检查私钥是否已配置
if grep -q 'your_private_key_here' config.xml; then
    echo "❌ 错误：private_key 未配置或使用了默认值！"
    echo "📝 请编辑 config.xml 文件，填入你的真实私钥"
    exit 1
fi

echo "✅ 配置文件已准备好"
echo "🚀 启动 PredictionMarket 后端..."
echo ""

# 运行后端（Go 程序会自动读取 config.xml 文件）
go run main.go
