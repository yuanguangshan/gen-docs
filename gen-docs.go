package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/pflag"
)

// ============================================================
//  Configuration & Data Types
// ============================================================

const versionStr = "v3.2.0"

type Config struct {
	RootDir        string
	OutputFile     string
	IncludeExts    []string
	IncludeMatches []string
	ExcludeExts    []string
	ExcludeMatches []string
	MaxFileSize    int64
	NoSubdirs      bool
	Verbose        bool
	Version        bool
	ShowStats      bool
	DryRun         bool
}

type FileMetadata struct {
	RelPath   string
	FullPath  string
	Size      int64
	LineCount int
}

type Stats struct {
	PotentialMatches   int
	ExplicitlyExcluded int
	FileCount          int
	TotalSize          int64
	TotalLines         int
	Skipped            int
	DirCount           int
}

type SkippedFile struct {
	RelPath string
	Reason  string
}

type DirStats struct {
	Path       string
	FileCount  int
	TotalSize  int64
	TotalLines int
}

type ExtStats struct {
	Ext        string
	FileCount  int
	TotalSize  int64
	TotalLines int
}

// ============================================================
//  Ignore Rules
// ============================================================

var ignoreDirs = map[string]bool{
	".git": true, ".idea": true, ".vscode": true, ".svn": true, ".hg": true,
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	"target": true, "bin": true, "out": true, "release": true, "debug": true,
	"__pycache__": true, ".pytest_cache": true, ".tox": true,
	".env": true, ".venv": true, "venv": true, "env": true,
	"Pods": true, "Carthage": true, "CocoaPods": true,
	"obj": true, "ipch": true, "Debug": true, "Release": true,
	"x64": true, "x86": true, "arm64": true,
	".gradle": true, ".sonar": true, ".scannerwork": true,
	"logs": true, "tmp": true, "temp": true, "cache": true,
	".history": true, ".nyc_output": true, ".coverage": true,
}

var ignoreFiles = map[string]bool{
	"package-lock.json": true, "yarn.lock": true, "go.sum": true,
	"composer.lock": true, "Gemfile.lock": true,
	"tags": true, "TAGS": true, ".DS_Store": true,
	"coverage.xml": true, "thumbs.db": true,
}

var ignoreExts = map[string]bool{
	".log": true, ".tmp": true, ".temp": true, ".cache": true,
	".swp": true, ".swo": true, ".pid": true, ".seed": true, ".idx": true,
	".user": true, ".userosscache": true,
	".aps": true, ".ncb": true, ".opendb": true, ".opensdf": true,
	".sdf": true, ".cachefile": true,
	".tgz": true, ".zip": true, ".rar": true, ".7z": true,
	".tar": true, ".gz": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true,
	".o": true, ".a": true, ".lib": true,
	".class": true, ".pyc": true, ".pyo": true,
	".woff": true, ".woff2": true, ".ttf": true, ".eot": true,
	".ico": true, ".png": true, ".jpg": true, ".jpeg": true,
	".gif": true, ".bmp": true, ".svg": true, ".webp": true,
	".mp3": true, ".mp4": true, ".wav": true, ".avi": true,
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
}

var languageMap = map[string]string{
	".go": "go", ".js": "javascript", ".ts": "typescript",
	".tsx": "typescript", ".jsx": "javascript",
	".py": "python", ".java": "java",
	".c": "c", ".cpp": "cpp", ".cc": "cpp", ".cxx": "cpp",
	".h": "c", ".hpp": "cpp",
	".rs": "rust", ".rb": "ruby", ".php": "php",
	".cs": "csharp", ".swift": "swift", ".kt": "kotlin",
	".scala": "scala", ".r": "r", ".sql": "sql",
	".sh": "bash", ".bash": "bash", ".zsh": "bash", ".fish": "fish",
	".ps1": "powershell",
	".md":  "markdown", ".html": "html", ".htm": "html",
	".css": "css", ".scss": "scss", ".sass": "sass", ".less": "less",
	".xml": "xml", ".json": "json",
	".yaml": "yaml", ".yml": "yaml",
	".toml": "toml", ".ini": "ini", ".conf": "conf",
	".txt": "text",
	".vue": "vue", ".svelte": "svelte",
	".dart": "dart", ".lua": "lua", ".pl": "perl", ".pm": "perl",
	".zig": "zig", ".nim": "nim",
	".ex": "elixir", ".exs": "elixir",
	".erl": "erlang", ".hs": "haskell", ".ml": "ocaml",
	".tf": "hcl", ".hcl": "hcl",
	".proto":   "protobuf",
	".graphql": "graphql", ".gql": "graphql",
	".gradle": "gradle",
}

// 预分配，供 bytes.Count 使用
var newlineSep = []byte{'\n'}

// ============================================================
//  Main
// ============================================================

func main() {
	cfg := parseFlags()

	if cfg.ShowStats {
		if err := showProjectStats(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "❌ 统计失败: %v\n", err)
			os.Exit(1)
		}
		return
	}

	printStartupInfo(cfg)

	fmt.Println("⏳ 正在扫描文件结构...")
	files, stats, skippedFiles, err := scanDirectory(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 扫描失败: %v\n", err)
		os.Exit(1)
	}

	if cfg.DryRun {
		printDryRunReport(cfg, files, stats, skippedFiles)
		return
	}

	fmt.Printf("💾 正在写入文档 [文件数: %d]...\n", len(files))
	if err := writeMarkdownStream(cfg, files, stats); err != nil {
		fmt.Fprintf(os.Stderr, "❌ 写入失败: %v\n", err)
		os.Exit(1)
	}

	printSummary(stats, cfg.OutputFile)
}

// ============================================================
//  Flag Parsing
// ============================================================

func parseFlags() Config {
	var cfg Config
	var include, match, exclude, excludeMatch string
	var maxKB int64

	pflag.StringVar(&cfg.RootDir, "dir", ".", "Root directory to scan")
	pflag.StringVarP(&cfg.OutputFile, "output", "o", "", "Output markdown file")
	pflag.StringVarP(&include, "include", "i", "", "Include extensions (e.g. .go,.js)")
	pflag.StringVarP(&match, "match", "m", "", "Include path keywords (e.g. _test.go)")
	pflag.StringVarP(&exclude, "exclude", "x", "", "Exclude extensions (e.g. .exe,.o)")
	pflag.StringVar(&excludeMatch, "exclude-match", "", "Exclude path keywords (e.g. vendor/)")
	pflag.Int64Var(&maxKB, "max-size", 500, "Max file size in KB")
	pflag.BoolVar(&cfg.NoSubdirs, "no-subdirs", false, "Do not scan subdirectories")
	pflag.BoolVar(&cfg.NoSubdirs, "ns", false, "Alias for --no-subdirs")
	pflag.BoolVarP(&cfg.Verbose, "verbose", "v", false, "Verbose output")
	pflag.BoolVar(&cfg.Version, "version", false, "Show version")
	pflag.BoolVarP(&cfg.ShowStats, "stats", "s", false, "Show project statistics only")
	pflag.BoolVarP(&cfg.DryRun, "dry-run", "n", false, "Preview which files would be included (no output written)")

	pflag.Parse()

	if cfg.Version {
		fmt.Printf("gen-docs %s\n", versionStr)
		os.Exit(0)
	}

	if args := pflag.Args(); len(args) > 0 {
		cfg.RootDir = args[0]
	}

	if cfg.OutputFile == "" {
		cfg.OutputFile = generateOutputName(cfg.RootDir)
	}

	cfg.IncludeExts = normalizeExts(include)
	cfg.IncludeMatches = splitAndTrim(match)
	cfg.ExcludeExts = normalizeExts(exclude)
	cfg.ExcludeMatches = splitAndTrim(excludeMatch)

	fileExcludes, pathExcludes := loadIgnoreFile(cfg.RootDir)
	cfg.ExcludeExts = mergeUnique(cfg.ExcludeExts, fileExcludes)
	cfg.ExcludeMatches = mergeUnique(cfg.ExcludeMatches, pathExcludes)

	cfg.MaxFileSize = maxKB * 1024
	return cfg
}

func generateOutputName(rootDir string) string {
	baseName := "project"
	cleanRoot := filepath.Clean(rootDir)

	if cleanRoot == "." || cleanRoot == string(filepath.Separator) {
		if abs, err := filepath.Abs(cleanRoot); err == nil {
			baseName = filepath.Base(abs)
		}
	} else {
		r := strings.NewReplacer(string(filepath.Separator), "_", ".", "_")
		baseName = r.Replace(cleanRoot)
		// 压缩连续下划线：用一次循环替代原来的 for+Contains
		var b strings.Builder
		b.Grow(len(baseName))
		prev := byte(0)
		for i := 0; i < len(baseName); i++ {
			c := baseName[i]
			if c == '_' && prev == '_' {
				continue
			}
			b.WriteByte(c)
			prev = c
		}
		baseName = strings.Trim(b.String(), "_")
	}

	return fmt.Sprintf("%s-%s-docs.md", baseName, time.Now().Format("20060102"))
}

// ============================================================
//  Shared Utilities
// ============================================================

func splitAndTrim(input string) []string {
	if input == "" {
		return nil
	}
	var result []string
	for _, p := range strings.Split(input, ",") {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	return result
}

func normalizeExts(input string) []string {
	if input == "" {
		return nil
	}
	var exts []string
	for _, p := range strings.Split(input, ",") {
		p = strings.TrimSpace(strings.ToLower(p))
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, ".") {
			p = "." + p
		}
		exts = append(exts, p)
	}
	return exts
}

func mergeUnique(base, additional []string) []string {
	if len(additional) == 0 {
		return base
	}
	if len(base) == 0 {
		return additional
	}
	seen := make(map[string]bool, len(base)+len(additional))
	result := make([]string, 0, len(base)+len(additional))
	for _, slc := range [2][]string{base, additional} {
		for _, s := range slc {
			if !seen[s] {
				seen[s] = true
				result = append(result, s)
			}
		}
	}
	return result
}

// toExtSet 将扩展名切片转为 map，用于 O(1) 查找
func toExtSet(exts []string) map[string]bool {
	if len(exts) == 0 {
		return nil
	}
	m := make(map[string]bool, len(exts))
	for _, e := range exts {
		m[e] = true
	}
	return m
}

func logf(verbose bool, format string, a ...any) {
	if verbose {
		fmt.Printf(format+"\n", a...)
	}
}

func safePercent(part, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return part / total * 100
}

func formatSize(b int64) string {
	kb := float64(b) / 1024
	if kb >= 1024 {
		return fmt.Sprintf("%.2f MB", kb/1024)
	}
	return fmt.Sprintf("%.2f KB", kb)
}

// ============================================================
//  Ignore File Loading (.gen-docs-ignore)
// ============================================================

func loadIgnoreFile(rootDir string) (extExcludes, pathExcludes []string) {
	candidates := []string{".gen-docs-ignore", ".gdocsignore", ".docs-ignore"}
	for _, name := range candidates {
		p := filepath.Join(rootDir, name)
		f, err := os.Open(p)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.HasPrefix(line, ".") && !strings.Contains(line, "/") {
				extExcludes = append(extExcludes, strings.ToLower(line))
			} else {
				pathExcludes = append(pathExcludes, line)
			}
		}
		f.Close()
		break
	}
	return
}

// ============================================================
//  File Utilities
// ============================================================

func shouldIgnoreDir(name string) bool {
	if ignoreDirs[name] {
		return true
	}
	return len(name) > 1 && name[0] == '.'
}

func shouldSkipFile(name string) bool {
	return ignoreFiles[name] || ignoreExts[strings.ToLower(filepath.Ext(name))]
}

// analyzeFile 单次读取文件：同时完成二进制检测和行数统计
// 原来 isBinaryFile + countLines 需要打开文件两次，现在只需一次
// 行数统计使用 bytes.Count 替代 bufio.Scanner，速度更快
func analyzeFile(path string, buf []byte) (isBinary bool, lineCount int, err error) {
	if strings.Contains(filepath.Base(path), ".min.") {
		return true, 0, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return true, 0, err
	}
	defer f.Close()

	var (
		totalBytes int
		lastByte   byte
		isFirst    = true
	)

	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			totalBytes += n
			lastByte = chunk[n-1]

			// 仅对首块进行二进制检测（与原逻辑一致，采样前 512 字节）
			if isFirst {
				sampleLen := n
				if sampleLen > 512 {
					sampleLen = 512
				}
				sample := chunk[:sampleLen]
				if bytes.IndexByte(sample, 0) >= 0 {
					return true, 0, nil
				}
				if !utf8.Valid(sample) {
					return true, 0, nil
				}
				isFirst = false
			}

			lineCount += bytes.Count(chunk, newlineSep)
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return false, 0, readErr
		}
	}

	// 兼容 bufio.Scanner 的行为：文件非空且末尾无换行时，最后一行仍计数
	if totalBytes > 0 && lastByte != '\n' {
		lineCount++
	}

	return false, lineCount, nil
}

func detectLanguage(path string) string {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "dockerfile", "containerfile":
		return "dockerfile"
	case "makefile", "gnumakefile":
		return "makefile"
	case "cmakelists.txt":
		return "cmake"
	case "jenkinsfile":
		return "groovy"
	case "vagrantfile":
		return "ruby"
	}

	if lang, ok := languageMap[strings.ToLower(filepath.Ext(path))]; ok {
		return lang
	}
	return "text"
}

// ============================================================
//  Unified Filtering (使用 map 实现 O(1) 扩展名匹配)
// ============================================================

func filePassesInclude(relPath string, includeExtSet map[string]bool, includeMatches []string) bool {
	if includeExtSet == nil && len(includeMatches) == 0 {
		return true
	}

	extOK := includeExtSet == nil
	if !extOK {
		extOK = includeExtSet[strings.ToLower(filepath.Ext(relPath))]
	}

	pathOK := len(includeMatches) == 0
	if !pathOK {
		for _, m := range includeMatches {
			if strings.Contains(relPath, m) {
				pathOK = true
				break
			}
		}
	}

	return extOK && pathOK
}

func fileIsExcluded(relPath string, excludeExtSet map[string]bool, excludeMatches []string) bool {
	if excludeExtSet != nil && excludeExtSet[strings.ToLower(filepath.Ext(relPath))] {
		return true
	}
	for _, m := range excludeMatches {
		if strings.Contains(relPath, m) {
			return true
		}
	}
	return false
}

// ============================================================
//  Directory Scanning
// ============================================================

func scanDirectory(cfg Config) ([]FileMetadata, Stats, []SkippedFile, error) {
	var files []FileMetadata
	var stats Stats
	var skipped []SkippedFile

	absOutput, _ := filepath.Abs(cfg.OutputFile)
	trackSkip := cfg.DryRun || cfg.Verbose
	readBuf := make([]byte, 32*1024) // 扫描期间所有文件复用同一个缓冲区

	// 预编译扩展名集合，O(1) 查找
	includeExtSet := toExtSet(cfg.IncludeExts)
	excludeExtSet := toExtSet(cfg.ExcludeExts)

	err := filepath.WalkDir(cfg.RootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			logf(cfg.Verbose, "⚠ 无法访问: %s", path)
			stats.Skipped++
			return nil
		}

		relPath, _ := filepath.Rel(cfg.RootDir, path)
		if relPath == "." {
			return nil
		}

		// ── 目录处理 ──
		if d.IsDir() {
			if cfg.NoSubdirs && relPath != "." {
				return filepath.SkipDir
			}
			if shouldIgnoreDir(d.Name()) {
				logf(cfg.Verbose, "⊘ 跳过目录: %s", relPath)
				return filepath.SkipDir
			}
			stats.DirCount++
			return nil
		}

		// ── 跳过输出文件自身 ──
		if absPath, _ := filepath.Abs(path); absPath == absOutput {
			return nil
		}

		// ── 内置文件名/扩展名排除（无 I/O）──
		if shouldSkipFile(d.Name()) {
			stats.Skipped++
			if trackSkip {
				skipped = append(skipped, SkippedFile{relPath, "内置忽略规则 (文件名/扩展名)"})
			}
			return nil
		}

		// ── 文件大小检查（DirEntry 缓存，无额外 syscall）──
		info, err := d.Info()
		if err != nil {
			stats.Skipped++
			return nil
		}

		if info.Size() > cfg.MaxFileSize {
			logf(cfg.Verbose, "⊘ 文件过大: %s (%.2f KB)", relPath, float64(info.Size())/1024)
			stats.Skipped++
			if trackSkip {
				skipped = append(skipped, SkippedFile{relPath, fmt.Sprintf("超出大小限制 (%s > %d KB)", formatSize(info.Size()), cfg.MaxFileSize/1024)})
			}
			return nil
		}

		// ── include/exclude 过滤（无 I/O，前置于文件读取）──
		if !filePassesInclude(relPath, includeExtSet, cfg.IncludeMatches) {
			stats.Skipped++
			if trackSkip {
				skipped = append(skipped, SkippedFile{relPath, "不符合 include 规则 (-i / -m)"})
			}
			return nil
		}

		stats.PotentialMatches++

		if fileIsExcluded(relPath, excludeExtSet, cfg.ExcludeMatches) {
			logf(cfg.Verbose, "⊘ 被排除规则拦截: %s", relPath)
			stats.ExplicitlyExcluded++
			if trackSkip {
				skipped = append(skipped, SkippedFile{relPath, "命中 exclude 规则 (-x / -xm)"})
			}
			return nil
		}

		// ── 单次读取：二进制检测 + 行数统计（核心优化点）──
		isBin, lineCount, _ := analyzeFile(path, readBuf)
		if isBin {
			logf(cfg.Verbose, "⊘ 二进制文件: %s", relPath)
			stats.Skipped++
			if trackSkip {
				skipped = append(skipped, SkippedFile{relPath, "二进制/minified 文件"})
			}
			return nil
		}

		// ── 通过所有过滤 ──
		files = append(files, FileMetadata{
			RelPath:   relPath,
			FullPath:  path,
			Size:      info.Size(),
			LineCount: lineCount,
		})
		stats.FileCount++
		stats.TotalLines += lineCount
		stats.TotalSize += info.Size()

		logf(cfg.Verbose, "✓ 添加: %s (%d lines)", relPath, lineCount)
		return nil
	})

	sort.Slice(files, func(i, j int) bool {
		return files[i].RelPath < files[j].RelPath
	})

	return files, stats, skipped, err
}

// ============================================================
//  Dry-Run Report
// ============================================================

func printDryRunReport(cfg Config, files []FileMetadata, stats Stats, skippedFiles []SkippedFile) {
	sep := strings.Repeat("=", 74)
	thin := strings.Repeat("─", 74)

	fmt.Println()
	fmt.Println(sep)
	fmt.Println("  🔍  DRY-RUN 模式 — 不会写入任何文件")
	fmt.Println(sep)

	fmt.Println()
	fmt.Println("📋 当前过滤规则:")
	fmt.Println(thin)
	fmt.Printf("  Root Dir   : %s\n", cfg.RootDir)
	fmt.Printf("  Output File: %s\n", cfg.OutputFile)
	fmt.Printf("  Max Size   : %d KB\n", cfg.MaxFileSize/1024)
	if len(cfg.IncludeExts) > 0 {
		fmt.Printf("  Include Ext: %s\n", strings.Join(cfg.IncludeExts, ", "))
	} else {
		fmt.Printf("  Include Ext: (全部)\n")
	}
	if len(cfg.IncludeMatches) > 0 {
		fmt.Printf("  Include Key: %s\n", strings.Join(cfg.IncludeMatches, ", "))
	}
	if len(cfg.ExcludeExts) > 0 {
		fmt.Printf("  Exclude Ext: %s\n", strings.Join(cfg.ExcludeExts, ", "))
	}
	if len(cfg.ExcludeMatches) > 0 {
		fmt.Printf("  Exclude Key: %s\n", strings.Join(cfg.ExcludeMatches, ", "))
	}
	fmt.Printf("  No Subdirs : %v\n", cfg.NoSubdirs)

	fmt.Println()
	fmt.Println("✅ 将被收录的文件:")
	fmt.Println(thin)

	if len(files) == 0 {
		fmt.Println("  (无文件被收录，请检查过滤规则)")
	} else {
		dirGroup := make(map[string][]FileMetadata)
		var dirs []string
		for _, f := range files {
			dir := filepath.Dir(f.RelPath)
			if _, exists := dirGroup[dir]; !exists {
				dirs = append(dirs, dir)
			}
			dirGroup[dir] = append(dirGroup[dir], f)
		}
		sort.Strings(dirs)

		for _, dir := range dirs {
			dirFiles := dirGroup[dir]
			var dirSize int64
			var dirLines int
			for _, f := range dirFiles {
				dirSize += f.Size
				dirLines += f.LineCount
			}
			fmt.Printf("\n  📂 %s/ (%d files, %s, %d lines)\n", dir, len(dirFiles), formatSize(dirSize), dirLines)
			for _, f := range dirFiles {
				lang := detectLanguage(f.RelPath)
				fmt.Printf("     ├─ %-40s %6d lines  %10s  [%s]\n",
					filepath.Base(f.RelPath), f.LineCount, formatSize(f.Size), lang)
			}
		}
	}

	fmt.Println()
	fmt.Println("❌ 被跳过的文件:")
	fmt.Println(thin)

	if len(skippedFiles) == 0 {
		fmt.Println("  (无)")
	} else {
		reasonGroup := make(map[string][]string)
		var reasons []string
		for _, s := range skippedFiles {
			if _, exists := reasonGroup[s.Reason]; !exists {
				reasons = append(reasons, s.Reason)
			}
			reasonGroup[s.Reason] = append(reasonGroup[s.Reason], s.RelPath)
		}
		sort.Strings(reasons)

		for _, reason := range reasons {
			paths := reasonGroup[reason]
			fmt.Printf("\n  🚫 %s (%d 个文件)\n", reason, len(paths))
			limit := len(paths)
			truncated := false
			if limit > 15 {
				limit = 15
				truncated = true
			}
			for _, p := range paths[:limit] {
				fmt.Printf("     ├─ %s\n", p)
			}
			if truncated {
				fmt.Printf("     └─ ... 还有 %d 个文件\n", len(paths)-15)
			}
		}
	}

	fmt.Println()
	fmt.Println(sep)
	fmt.Println("📊 汇总:")
	fmt.Println(sep)
	fmt.Printf("  扫描目录数        : %d\n", stats.DirCount)
	fmt.Printf("  符合 include 规则 : %d\n", stats.PotentialMatches)
	fmt.Printf("  被 exclude 踢除   : %d\n", stats.ExplicitlyExcluded)
	fmt.Printf("  其他原因跳过      : %d\n", stats.Skipped)
	fmt.Printf("  最终收录文件数    : %d\n", stats.FileCount)
	fmt.Printf("  预计总行数        : %d\n", stats.TotalLines)
	fmt.Printf("  预计总大小        : %s\n", formatSize(stats.TotalSize))

	if len(files) > 0 {
		extMap := make(map[string]*ExtStats)
		for _, f := range files {
			ext := strings.ToLower(filepath.Ext(f.RelPath))
			if ext == "" {
				ext = "(no ext)"
			}
			if es, ok := extMap[ext]; ok {
				es.FileCount++
				es.TotalSize += f.Size
				es.TotalLines += f.LineCount
			} else {
				extMap[ext] = &ExtStats{Ext: ext, FileCount: 1, TotalSize: f.Size, TotalLines: f.LineCount}
			}
		}

		extList := make([]ExtStats, 0, len(extMap))
		for _, es := range extMap {
			extList = append(extList, *es)
		}
		sort.Slice(extList, func(i, j int) bool { return extList[i].TotalLines > extList[j].TotalLines })

		fmt.Println()
		fmt.Println("📊 收录文件类型分布:")
		fmt.Println(thin)
		fmt.Printf("  %-12s %8s %12s %10s\n", "类型", "文件数", "总大小", "总行数")
		fmt.Println("  " + strings.Repeat("-", 46))
		for _, es := range extList {
			fmt.Printf("  %-12s %8d %12s %10d\n", es.Ext, es.FileCount, formatSize(es.TotalSize), es.TotalLines)
		}
	}

	fmt.Println()
	fmt.Println(sep)
	fmt.Println("💡 确认无误后去掉 --dry-run / -n 即可生成文档")
	fmt.Println(sep)
}

// ============================================================
//  Startup & Summary
// ============================================================

func printStartupInfo(cfg Config) {
	fmt.Println("▶ Gen-Docs Started")
	fmt.Printf("  Root: %s\n", cfg.RootDir)
	fmt.Printf("  Out : %s\n", cfg.OutputFile)
	fmt.Printf("  Max : %d KB\n", cfg.MaxFileSize/1024)
	if len(cfg.IncludeExts) > 0 {
		fmt.Printf("  Only Ext: %v\n", cfg.IncludeExts)
	}
	if len(cfg.IncludeMatches) > 0 {
		fmt.Printf("  Match   : %v\n", cfg.IncludeMatches)
	}
	if len(cfg.ExcludeExts) > 0 {
		fmt.Printf("  Skip Ext: %v\n", cfg.ExcludeExts)
	}
	if len(cfg.ExcludeMatches) > 0 {
		fmt.Printf("  Skip Key: %v\n", cfg.ExcludeMatches)
	}
	fmt.Println()
}

func printSummary(stats Stats, output string) {
	fmt.Println("\n✔ 完成!")
	fmt.Printf("  符合包含规则 (Potential) : %d\n", stats.PotentialMatches)
	fmt.Printf("  由于排除规则被踢除 (Excluded): %d\n", stats.ExplicitlyExcluded)
	fmt.Printf("  最终写入文件数 (Final)    : %d\n", stats.FileCount)
	fmt.Printf("  总行数 (Total Lines)      : %d\n", stats.TotalLines)
	fmt.Printf("  总物理大小 (Total Size)   : %s\n", formatSize(stats.TotalSize))
	fmt.Printf("  无需处理的无关文件          : %d\n", stats.Skipped)
	fmt.Printf("  输出路径                  : %s\n", output)
}

// ============================================================
//  Markdown Output
// ============================================================

func writeMarkdownStream(cfg Config, files []FileMetadata, stats Stats) error {
	f, err := os.Create(cfg.OutputFile)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriterSize(f, 64*1024)

	fmt.Fprintln(w, "# Project Documentation")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "- **Generated at:** %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "- **Root Dir:** `%s`\n", cfg.RootDir)
	fmt.Fprintf(w, "- **File Count:** %d\n", stats.FileCount)
	fmt.Fprintf(w, "- **Total Lines:** %d\n", stats.TotalLines)
	fmt.Fprintf(w, "- **Total Size:** %s\n", formatSize(stats.TotalSize))
	fmt.Fprintln(w)

	fmt.Fprintln(w, "<a name=\"toc\"></a>")
	fmt.Fprintln(w, "## 📂 扫描目录")
	for _, file := range files {
		anchor := generateAnchor(file.RelPath)
		fmt.Fprintf(w, "- [%s](#%s) (%d lines, %s)\n",
			file.RelPath, anchor, file.LineCount, formatSize(file.Size))
	}
	fmt.Fprintln(w, "\n---")

	total := len(files)
	for i, file := range files {
		if !cfg.Verbose && (i%10 == 0 || i == total-1) {
			fmt.Printf("\r🚀 写入进度: %d/%d (%.1f%%)", i+1, total, float64(i+1)/float64(total)*100)
		}
		if err := writeFileSection(w, file); err != nil {
			logf(true, "\n⚠ 读取失败 %s: %v", file.RelPath, err)
		}
	}
	fmt.Println()

	fmt.Fprintln(w, "\n---")
	fmt.Fprintln(w, "### 📊 最终统计汇总")
	fmt.Fprintf(w, "- **文件总数:** %d\n", stats.FileCount)
	fmt.Fprintf(w, "- **代码总行数:** %d\n", stats.TotalLines)
	fmt.Fprintf(w, "- **物理总大小:** %s\n", formatSize(stats.TotalSize))

	return w.Flush()
}

func generateAnchor(relPath string) string {
	anchor := strings.ToLower(relPath)
	anchor = strings.ReplaceAll(anchor, " ", "-")
	anchor = strings.ReplaceAll(anchor, "/", "-")
	return "file-" + anchor
}

func writeFileSection(w *bufio.Writer, file FileMetadata) error {
	src, err := os.Open(file.FullPath)
	if err != nil {
		return err
	}
	defer src.Close()

	lang := detectLanguage(file.RelPath)
	anchor := generateAnchor(file.RelPath)

	fmt.Fprintln(w)
	fmt.Fprintf(w, "<a name=\"%s\"></a>\n", anchor)
	fmt.Fprintf(w, "## 📄 %s\n\n", file.RelPath)
	fmt.Fprintf(w, "````%s\n", lang)

	if _, err := io.Copy(w, src); err != nil {
		return err
	}

	fmt.Fprintln(w, "\n````")
	fmt.Fprintln(w, "\n[⬆ 回到目录](#toc)")
	return nil
}

// ============================================================
//  Project Statistics (-s)：复用 scanDirectory，消除重复代码
// ============================================================

func collectAllFiles(cfg Config) ([]FileMetadata, Stats, error) {
	// 复用 scanDirectory，清除所有过滤规则以收集全部文件
	statsCfg := cfg
	statsCfg.IncludeExts = nil
	statsCfg.IncludeMatches = nil
	statsCfg.ExcludeExts = nil
	statsCfg.ExcludeMatches = nil
	statsCfg.DryRun = false
	statsCfg.Verbose = false

	files, stats, _, err := scanDirectory(statsCfg)
	return files, stats, err
}

func showProjectStats(cfg Config) error {
	fmt.Println("📊 正在统计项目信息...")
	fmt.Printf("  Root: %s\n\n", cfg.RootDir)

	files, stats, err := collectAllFiles(cfg)
	if err != nil {
		return err
	}

	dirMap, extMap := aggregateStats(files)

	sep := strings.Repeat("=", 71)

	fmt.Println(sep)
	fmt.Println("📁 基本统计")
	fmt.Println(sep)
	fmt.Printf("  文件夹数量: %d\n", stats.DirCount)
	fmt.Printf("  文件数量  : %d\n", stats.FileCount)
	fmt.Printf("  总行数    : %d\n", stats.TotalLines)
	fmt.Printf("  总大小    : %s (%.2f MB)\n",
		formatSize(stats.TotalSize), float64(stats.TotalSize)/1024/1024)

	printTopDirs(dirMap, stats, sep)
	printTopFiles(files, stats, sep)
	printExtBreakdown(extMap, stats, sep)

	fmt.Println("\n" + sep)
	fmt.Println("✅ 统计完成!")
	fmt.Println(sep)

	return nil
}

func aggregateStats(files []FileMetadata) (map[string]*DirStats, map[string]*ExtStats) {
	dirMap := make(map[string]*DirStats, len(files)/4)
	extMap := make(map[string]*ExtStats, 16)

	for _, f := range files {
		dir := filepath.Dir(f.RelPath)
		if ds, ok := dirMap[dir]; ok {
			ds.FileCount++
			ds.TotalSize += f.Size
			ds.TotalLines += f.LineCount
		} else {
			dirMap[dir] = &DirStats{
				Path: dir, FileCount: 1,
				TotalSize: f.Size, TotalLines: f.LineCount,
			}
		}

		ext := strings.ToLower(filepath.Ext(f.RelPath))
		if ext == "" {
			ext = "(no ext)"
		}
		if es, ok := extMap[ext]; ok {
			es.FileCount++
			es.TotalSize += f.Size
			es.TotalLines += f.LineCount
		} else {
			extMap[ext] = &ExtStats{
				Ext: ext, FileCount: 1,
				TotalSize: f.Size, TotalLines: f.LineCount,
			}
		}
	}

	return dirMap, extMap
}

func printTopDirs(dirMap map[string]*DirStats, stats Stats, sep string) {
	list := make([]DirStats, 0, len(dirMap))
	for _, ds := range dirMap {
		if ds.FileCount > 0 {
			list = append(list, *ds)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].TotalSize > list[j].TotalSize })

	fmt.Println("\n" + sep)
	fmt.Println("📂 Top 5 最大文件夹")
	fmt.Println(sep)

	for i := 0; i < 5 && i < len(list); i++ {
		ds := list[i]
		fmt.Printf("  %d. %s\n", i+1, ds.Path)
		fmt.Printf("     大小: %s (%.1f%%), 行数: %d (%.1f%%), 文件数: %d\n",
			formatSize(ds.TotalSize),
			safePercent(float64(ds.TotalSize), float64(stats.TotalSize)),
			ds.TotalLines,
			safePercent(float64(ds.TotalLines), float64(stats.TotalLines)),
			ds.FileCount)
	}
}

func printTopFiles(files []FileMetadata, stats Stats, sep string) {
	sorted := make([]FileMetadata, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Size > sorted[j].Size })

	fmt.Println("\n" + sep)
	fmt.Println("📄 Top 5 最大文件")
	fmt.Println(sep)

	for i := 0; i < 5 && i < len(sorted); i++ {
		f := sorted[i]
		fmt.Printf("  %d. %s\n", i+1, f.RelPath)
		fmt.Printf("     大小: %s (%.1f%%), 行数: %d (%.1f%%)\n",
			formatSize(f.Size),
			safePercent(float64(f.Size), float64(stats.TotalSize)),
			f.LineCount,
			safePercent(float64(f.LineCount), float64(stats.TotalLines)))
	}
}

func printExtBreakdown(extMap map[string]*ExtStats, stats Stats, sep string) {
	list := make([]ExtStats, 0, len(extMap))
	for _, es := range extMap {
		list = append(list, *es)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].TotalSize > list[j].TotalSize })

	fmt.Println("\n" + sep)
	fmt.Println("📊 按文件类型统计")
	fmt.Println(sep)

	fmt.Printf("  %-15s %8s %12s %10s %8s\n", "类型", "文件数", "总大小", "总行数", "占比")
	fmt.Println("  " + strings.Repeat("-", 58))
	for _, es := range list {
		fmt.Printf("  %-15s %8d %12s %10d %7.1f%%\n",
			es.Ext, es.FileCount,
			formatSize(es.TotalSize),
			es.TotalLines,
			safePercent(float64(es.TotalSize), float64(stats.TotalSize)))
	}
}