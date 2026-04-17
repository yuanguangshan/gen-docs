# Gdox (gd)

**一键将项目代码库转换为 AI 可读的 Markdown 快照。**

Gdox 是一个极简、高性能的工具，用于扫描项目目录并将代码合并为单个 Markdown 文件，方便你快速喂给 LLM (GPT/Claude) 或进行代码审计。

## 安装

```bash
bash install-gdox.sh
```

## 使用

```bash
gd                  # 扫描当前项目
gd -i go,md         # 只包含 Go 和 Markdown 文件
gd -x exe,bin       # 排除特定后缀
gd -X vendor        # 排除指定目录关键字
gd -n               # 不扫描子目录
gd --dry-run        # 预览文件列表
```

## 特性

- **极速**: Go 编写，秒级处理万行代码。
- **智能**: 自动处理 `.gitignore` 和二进制文件。
- **清晰**: 自动生成带跳转链接的项目目录树。

---
*Simple, Fast, and AI-Friendly.*
