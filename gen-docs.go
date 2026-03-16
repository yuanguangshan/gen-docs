package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// ============================================================
//  Configuration & Data Types
// ============================================================

const versionStr = "v3.0.0"

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
//  Ignore Rules (split by type for correct matching)
// ============================================================

// 精确匹配目录名
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

// 精确匹配文件名
var ignoreFiles = map[string]bool{
	"package-lock.json": true, "yarn.lock": true, "go.sum": true,
	"composer.lock": true, "Gemfile.lock": true,
	"tags": true, "TAGS": true, ".DS_Store": true,
	"coverage.xml": true, "thumbs.db": true,
}

// 按扩展名排除（解决原版 *.log 通配符无法匹配的 bug）
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
	files, stats, err := scanDirectory(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 扫描失败: %v\n", err)
		os.Exit(1)
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

	flag.StringVar(&cfg.RootDir, "dir", ".", "Root directory to scan")
	flag.StringVar(&cfg.OutputFile, "o", "", "Output markdown file")
	flag.StringVar(&include, "i", "", "Include extensions (e.g. .go,.js)")
	flag.StringVar(&match, "m", "", "Include path keywords (e.g. _test.go)")
	flag.StringVar(&exclude, "x", "", "Exclude extensions (e.g. .exe,.o)")
	flag.StringVar(&excludeMatch, "xm", "", "Exclude path keywords (e.g. vendor/)")
	flag.Int64Var(&maxKB, "max-size", 500, "Max file size in KB")
	flag.BoolVar(&cfg.NoSubdirs, "no-subdirs", false, "Do not scan subdirectories")
	flag.BoolVar(&cfg.NoSubdirs, "ns", false, "Alias for --no-subdirs")
	flag.BoolVar(&cfg.Verbose, "v", false, "Verbose output")
	flag.BoolVar(&cfg.Version, "version", false, "Show version")
	flag.BoolVar(&cfg.ShowStats, "s", false, "Show project statistics only (no doc generation)")

	flag.Parse()

	if cfg.Version {
		fmt.Printf("gen-docs %s\n", versionStr)
		os.Exit(0)
	}

	if args := flag.Args(); len(args) > 0 {
		cfg.RootDir = args[0]
	}

	if cfg.OutputFile == "" {
		cfg.OutputFile = generateOutputName(cfg.RootDir)
	}

	cfg.IncludeExts = normalizeExts(include)
	cfg.IncludeMatches = splitAndTrim(match)
	cfg.ExcludeExts = normalizeExts(exclude)
	cfg.ExcludeMatches = splitAndTrim(excludeMatch)

	// 加载 .gen-docs-ignore 等配置文件
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
		baseName = strings.NewReplacer(
			string(filepath.Separator), "_",
			".", "_",
		).Replace(cleanRoot)
		for strings.Contains(baseName, "__") {
			baseName = strings.ReplaceAll(baseName, "__", "_")
		}
		baseName = strings.Trim(baseName, "_")
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
	seen := make(map[string]bool, len(base)+len(additional))
	var result []string
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

func formatSize(bytes int64) string {
	kb := float64(bytes) / 1024
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
		path := filepath.Join(rootDir, name)
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(strings.NewReader(string(content)))
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
		break
	}
	return
}

// ============================================================
//  File Utilities
// ============================================================

func shouldIgnoreDir(name string) bool {
	if strings.HasPrefix(name, ".") && name != "." {
		return true
	}
	return ignoreDirs[name]
}

func shouldSkipFile(name string) bool {
	if ignoreFiles[name] {
		return true
	}
	return ignoreExts[strings.ToLower(filepath.Ext(name))]
}

func isBinaryFile(path string) bool {
	if strings.Contains(filepath.Base(path), ".min.") {
		return true
	}

	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false
	}
	buf = buf[:n]

	for _, b := range buf {
		if b == 0 {
			return true
		}
	}
	return !utf8.Valid(buf)
}

func countLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}

func detectLanguage(path string) string {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case base == "dockerfile" || base == "containerfile":
		return "dockerfile"
	case base == "makefile" || base == "gnumakefile":
		return "makefile"
	case base == "cmakelists.txt":
		return "cmake"
	case base == "jenkinsfile":
		return "groovy"
	case base == "vagrantfile":
		return "ruby"
	}

	ext := strings.ToLower(filepath.Ext(path))
	if lang, ok := languageMap[ext]; ok {
		return lang
	}
	return "text"
}

// ============================================================
//  Unified Filtering
// ============================================================

func filePassesInclude(relPath string, cfg Config) bool {
	if len(cfg.IncludeExts) == 0 && len(cfg.IncludeMatches) == 0 {
		return true
	}

	extOK := len(cfg.IncludeExts) == 0
	if !extOK {
		ext := strings.ToLower(filepath.Ext(relPath))
		for _, e := range cfg.IncludeExts {
			if ext == e {
				extOK = true
				break
			}
		}
	}

	pathOK := len(cfg.IncludeMatches) == 0
	if !pathOK {
		for _, m := range cfg.IncludeMatches {
			if strings.Contains(relPath, m) {
				pathOK = true
				break
			}
		}
	}

	return extOK && pathOK
}

func fileIsExcluded(relPath string, cfg Config) bool {
	ext := strings.ToLower(filepath.Ext(relPath))
	for _, e := range cfg.ExcludeExts {
		if ext == e {
			return true
		}
	}
	for _, m := range cfg.ExcludeMatches {
		if strings.Contains(relPath, m) {
			return true
		}
	}
	return false
}

// ============================================================
//  Directory Scanning (for doc generation)
// ============================================================

func scanDirectory(cfg Config) ([]FileMetadata, Stats, error) {
	var files []FileMetadata
	var stats Stats
	absOutput, _ := filepath.Abs(cfg.OutputFile)

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

		// 目录处理
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

		// 跳过输出文件自身
		if absPath, _ := filepath.Abs(path); absPath == absOutput {
			return nil
		}

		// 内置文件名/扩展名排除
		if shouldSkipFile(d.Name()) {
			stats.Skipped++
			return nil
		}

		info, err := d.Info()
		if err != nil {
			stats.Skipped++
			return nil
		}

		// 大小 + 二进制过滤
		if info.Size() > cfg.MaxFileSize {
			logf(cfg.Verbose, "⊘ 文件过大: %s (%.2f KB)", relPath, float64(info.Size())/1024)
			stats.Skipped++
			return nil
		}
		if isBinaryFile(path) {
			logf(cfg.Verbose, "⊘ 二进制文件: %s", relPath)
			stats.Skipped++
			return nil
		}

		// 包含过滤
		if !filePassesInclude(relPath, cfg) {
			stats.Skipped++
			return nil
		}

		stats.PotentialMatches++

		// 排除过滤
		if fileIsExcluded(relPath, cfg) {
			logf(cfg.Verbose, "⊘ 被排除规则拦截: %s", relPath)
			stats.ExplicitlyExcluded++
			return nil
		}

		// 通过所有过滤
		lineCount, _ := countLines(path)
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

	return files, stats, err
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

	// Header
	fmt.Fprintln(w, "# Project Documentation")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "- **Generated at:** %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "- **Root Dir:** `%s`\n", cfg.RootDir)
	fmt.Fprintf(w, "- **File Count:** %d\n", stats.FileCount)
	fmt.Fprintf(w, "- **Total Lines:** %d\n", stats.TotalLines)
	fmt.Fprintf(w, "- **Total Size:** %s\n", formatSize(stats.TotalSize))
	fmt.Fprintln(w)

	// TOC
	fmt.Fprintln(w, "<a name=\"toc\"></a>")
	fmt.Fprintln(w, "## 📂 扫描目录")
	for _, file := range files {
		anchor := generateAnchor(file.RelPath)
		fmt.Fprintf(w, "- [%s](#%s) (%d lines, %s)\n",
			file.RelPath, anchor, file.LineCount, formatSize(file.Size))
	}
	fmt.Fprintln(w, "\n---")

	// File contents
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

	// Footer stats
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
//  Project Statistics (-s)
// ============================================================

func showProjectStats(cfg Config) error {
	fmt.Println("📊 正在统计项目信息...")
	fmt.Printf("  Root: %s\n\n", cfg.RootDir)

	files, stats, err := collectAllFiles(cfg)
	if err != nil {
		return err
	}

	dirMap, extMap := aggregateStats(files)

	sep := strings.Repeat("=", 71)

	// 基本统计
	fmt.Println(sep)
	fmt.Println("📁 基本统计")
	fmt.Println(sep)
	fmt.Printf("  文件夹数量: %d\n", stats.DirCount)
	fmt.Printf("  文件数量  : %d\n", stats.FileCount)
	fmt.Printf("  总行数    : %d\n", stats.TotalLines)
	fmt.Printf("  总大小    : %s (%.2f MB)\n",
		formatSize(stats.TotalSize), float64(stats.TotalSize)/1024/1024)

	// Top 5 最大文件夹
	printTopDirs(dirMap, stats, sep)

	// Top 5 最大文件
	printTopFiles(files, stats, sep)

	// 按文件类型统计
	printExtBreakdown(extMap, stats, sep)

	fmt.Println("\n" + sep)
	fmt.Println("✅ 统计完成!")
	fmt.Println(sep)

	return nil
}

// collectAllFiles 专用于 -s 模式，不受 include/exclude 规则限制
func collectAllFiles(cfg Config) ([]FileMetadata, Stats, error) {
	var files []FileMetadata
	var stats Stats
	absOutput, _ := filepath.Abs(cfg.OutputFile)

	err := filepath.WalkDir(cfg.RootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		relPath, _ := filepath.Rel(cfg.RootDir, path)
		if relPath == "." {
			return nil
		}

		if d.IsDir() {
			if shouldIgnoreDir(d.Name()) {
				return filepath.SkipDir
			}
			stats.DirCount++
			return nil
		}

		if absPath, _ := filepath.Abs(path); absPath == absOutput {
			return nil
		}

		if shouldSkipFile(d.Name()) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		if info.Size() > cfg.MaxFileSize || isBinaryFile(path) {
			return nil
		}

		lineCount, _ := countLines(path)
		files = append(files, FileMetadata{
			RelPath:   relPath,
			FullPath:  path,
			Size:      info.Size(),
			LineCount: lineCount,
		})
		stats.FileCount++
		stats.TotalLines += lineCount
		stats.TotalSize += info.Size()

		return nil
	})

	return files, stats, err
}

func aggregateStats(files []FileMetadata) (map[string]*DirStats, map[string]*ExtStats) {
	dirMap := make(map[string]*DirStats)
	extMap := make(map[string]*ExtStats)

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
	var list []DirStats
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
	var list []ExtStats
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
