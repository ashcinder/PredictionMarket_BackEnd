#!/bin/bash
set -euo pipefail

if [ ! -f config.yaml ]; then
    echo "错误：未找到 config.yaml"
    echo "请先执行：cp config.example.yaml config.yaml"
    exit 1
fi

if grep -Eq 'replace-with-(wallet-private-key|ai-api-key)' config.yaml; then
    echo "错误：请在 config.yaml 中填写钱包私钥和 AI API Key"
    exit 1
fi

echo "配置文件已准备好，启动 PredictionMarket 后端"
go run main.go
