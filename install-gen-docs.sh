#!/usr/bin/env bash
# 项目文档生成工具安装脚本（全局可用 + gd 快捷命令）

set -e

echo "🚀 开始安装 gen-docs..."

# -------- 基础检查 --------
if ! command -v go &> /dev/null; then
    echo "❌ 未检测到 Go 编译器"
    echo "请先安装 Go: https://go.dev/dl/"
    exit 1
fi

echo "✓ Go 版本: $(go version)"

# -------- 编译 --------
echo "📦 编译 gen-docs..."
go build -o gen-docs gen-docs.go

# -------- 选择安装目录 --------
if [ -w "/usr/local/bin" ]; then
    INSTALL_DIR="/usr/local/bin"
    USE_SUDO=""
elif command -v sudo &> /dev/null; then
    INSTALL_DIR="/usr/local/bin"
    USE_SUDO="sudo"
else
    INSTALL_DIR="$HOME/.local/bin"
    USE_SUDO=""
    mkdir -p "$INSTALL_DIR"
fi

echo "📍 安装目录: $INSTALL_DIR"

# -------- 安装主程序 --------
echo "📥 安装 gen-docs"
$USE_SUDO mv gen-docs "$INSTALL_DIR/gen-docs"
$USE_SUDO chmod +x "$INSTALL_DIR/gen-docs"

# -------- 创建 gd 快捷命令（软链接） --------
echo "🔗 创建 gd 快捷命令"
$USE_SUDO ln -sf "$INSTALL_DIR/gen-docs" "$INSTALL_DIR/gd"

# -------- PATH 检查（仅在用户目录时） --------
if [[ "$INSTALL_DIR" == "$HOME/.local/bin" ]]; then
    if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
        echo ""
        echo "⚠️  $INSTALL_DIR 不在 PATH 中"
        echo ""
        echo "请将以下内容加入你的 shell 配置文件："
        echo ""
        echo "    export PATH=\"\$HOME/.local/bin:\$PATH\""
        echo ""
        echo "然后执行:"
        echo "    source ~/.zshrc  或  source ~/.bashrc"
    else
        echo "✓ PATH 已正确配置"
    fi
fi

# -------- 完成 --------
echo ""
echo "✅ 安装完成！"
echo ""
echo "现在你可以在任意目录使用："
echo "  gen-docs     # 完整命令"
echo "  gd           # 快捷命令"
echo ""
echo "示例："
echo "  gd"
echo "  gd -i md,go                  # 只包含特定后缀"
echo "  gd -x exe,bin                # 排除特定后缀"
echo "  gd -m _test.go               # 模糊匹配：提取所有测试文件"
echo "  gd -m _test.go -xm vendor/   # 复合匹配：包含测试但排除第三方库"
echo "  gd -ns                       # 不扫描子目录"
echo ""
