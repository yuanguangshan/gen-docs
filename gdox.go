package main

import (
	"bufio"
	"bytes"
	"fmt"
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

const versionStr = "v3.2.1"

type Config struct {
	RootDir           string
	OutputFile        string
	IncludeExts       []string
	IncludeMatches    []string
	ExcludeExts       []string
	ExcludeMatches    []string
	MaxFileSize       int64
	NoSubdirs         bool
	Verbose           bool
	Version           bool
	ShowStats         bool
	DryRun            bool
	NoDefaultIgnore   bool
	NoGitignore       bool
	AdditionalIgnores []string
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
	
	DirMap             map[string]*DirStats
	ExtMap             map[string]*ExtStats
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
//  Ignore Rules & Language Map
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

var languageMap = map[string]string{
	".go": "go", ".js": "javascript", ".ts": "typescript", ".py": "python",
	".c": "c", ".cpp": "cpp", ".h": "cpp", ".hpp": "cpp", ".cc": "cpp",
	".java": "java", ".rb": "ruby", ".php": "php", ".rs": "rust",
	".swift": "swift", ".kt": "kotlin", ".m": "objectivec", ".mm": "objectivec",
	".sh": "bash", ".zsh": "bash", ".bash": "bash", ".fish": "fish",
	".yml": "yaml", ".yaml": "yaml", ".json": "json", ".xml": "xml",
	".html": "html", ".css": "css", ".scss": "scss", ".sass": "sass", ".less": "less",
	".md": "markdown", ".sql": "sql", ".graphql": "graphql", ".proto": "protobuf",
	".dockerfile": "dockerfile", ".makefile": "makefile", ".cmake": "cmake",
	".vue": "vue", ".svelte": "svelte", ".dart": "dart", ".lua": "lua",
	".pl": "perl", ".ex": "elixir", ".erl": "erlang", ".hs": "haskell",
	".ml": "ocaml", ".clj": "clojure", ".tf": "hcl",
}

// ============================================================
//  Main Entry
// ============================================================

func main() {
	config := parseFlags()

	if config.Version {
		fmt.Printf("gdox %s\n", versionStr)
		return
	}

	if !config.DryRun {
		fmt.Println("▶ Gdox Started")
	} else {
		fmt.Println("▶ Gdox Dry-Run Mode")
	}

	startTime := time.Now()

	// 1. Scan and filter
	files, stats, skipped := scanDirectory(config)

	// 2. Output to Terminal
	if config.DryRun {
		printDryRun(files, stats, skipped)
	}
	
	if config.ShowStats || config.DryRun {
		printStatsTerminal(files, stats)
	}

	if config.DryRun {
		return
	}

	// 3. Generate content
	err := generateOutput(config, files, stats)
	if err != nil {
		fmt.Printf("❌ Error generating output: %v\n", err)
		os.Exit(1)
	}

	duration := time.Since(startTime)
	fmt.Printf("\n✨ Done! Generated %s in %v\n", config.OutputFile, duration)
}

func parseFlags() Config {
	var c Config
	pflag.StringVarP(&c.RootDir, "dir", "d", ".", "Root directory to scan")
	pflag.StringVarP(&c.OutputFile, "out", "o", "project_snapshot.md", "Output markdown file")
	
	var incExts, incMatches, excExts, excMatches, addIgnores string
	pflag.StringVarP(&incExts, "include", "i", "", "Include extensions (comma separated)")
	pflag.StringVarP(&incMatches, "match", "m", "", "Include path keywords (comma separated)")
	pflag.StringVarP(&excExts, "exclude", "x", "", "Exclude extensions (comma separated)")
	pflag.StringVarP(&excMatches, "exclude-match", "X", "", "Exclude path keywords (comma separated)")
	pflag.StringVarP(&addIgnores, "ignore", "", "", "Additional ignore patterns (comma separated)")

	pflag.Int64Var(&c.MaxFileSize, "max-size", 500, "Max file size in KB")
	pflag.BoolVarP(&c.NoSubdirs, "no-subdirs", "n", false, "Do not scan subdirectories")
	pflag.BoolVarP(&c.Verbose, "verbose", "v", false, "Verbose output")
	pflag.BoolVar(&c.Version, "version", false, "Show version")
	pflag.BoolVarP(&c.ShowStats, "stats", "s", false, "Show detailed multi-dimensional statistics")
	pflag.BoolVar(&c.DryRun, "dry-run", false, "Dry run mode (no file write)")
	pflag.BoolVar(&c.NoDefaultIgnore, "no-default-ignore", false, "Disable default ignore rules")
	pflag.BoolVar(&c.NoGitignore, "no-gitignore", false, "Do not load .gitignore")

	pflag.Parse()

	if incExts != "" { c.IncludeExts = cleanList(incExts) }
	if incMatches != "" { c.IncludeMatches = cleanList(incMatches) }
	if excExts != "" { c.ExcludeExts = cleanList(excExts) }
	if excMatches != "" { c.ExcludeMatches = cleanList(excMatches) }
	if addIgnores != "" { c.AdditionalIgnores = cleanList(addIgnores) }

	c.MaxFileSize *= 1024
	absRoot, _ := filepath.Abs(c.RootDir)
	c.RootDir = absRoot

	return c
}

func cleanList(s string) []string {
	parts := strings.Split(s, ",")
	var res []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" { continue }
		if !strings.HasPrefix(trimmed, ".") && !strings.ContainsAny(trimmed, "/\\") {
			res = append(res, "."+trimmed)
		} else {
			res = append(res, trimmed)
		}
	}
	return res
}

// ============================================================
//  Scanning Logic
// ============================================================

func scanDirectory(config Config) ([]FileMetadata, Stats, []SkippedFile) {
	var files []FileMetadata
	stats := Stats{
		DirMap: make(map[string]*DirStats),
		ExtMap: make(map[string]*ExtStats),
	}
	var skipped []SkippedFile

	gitPatterns := []string{}
	if !config.NoGitignore {
		gitPatterns = loadGitignore(config.RootDir)
	}

	filepath.WalkDir(config.RootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil { return nil }
		relPath, _ := filepath.Rel(config.RootDir, path)
		if relPath == "." { return nil }

		if d.IsDir() {
			if shouldIgnoreDir(relPath, config, gitPatterns) { return filepath.SkipDir }
			if config.NoSubdirs && strings.Contains(relPath, string(filepath.Separator)) { return filepath.SkipDir }
			stats.DirCount++
			return nil
		}

		stats.PotentialMatches++
		if shouldIgnoreFile(relPath, config, gitPatterns) {
			stats.ExplicitlyExcluded++
			return nil
		}

		info, _ := d.Info()
		if info.Size() > config.MaxFileSize {
			skipped = append(skipped, SkippedFile{relPath, "Size limit"})
			stats.Skipped++
			return nil
		}

		if isBinaryFile(path) {
			skipped = append(skipped, SkippedFile{relPath, "Binary file"})
			stats.Skipped++
			return nil
		}

		lineCount := countLines(path)
		fMeta := FileMetadata{RelPath: relPath, FullPath: path, Size: info.Size(), LineCount: lineCount}
		files = append(files, fMeta)

		// Accumulate Stats
		stats.FileCount++
		stats.TotalSize += fMeta.Size
		stats.TotalLines += fMeta.LineCount

		dir := filepath.Dir(relPath)
		if _, ok := stats.DirMap[dir]; !ok { stats.DirMap[dir] = &DirStats{Path: dir} }
		stats.DirMap[dir].FileCount++
		stats.DirMap[dir].TotalSize += fMeta.Size
		stats.DirMap[dir].TotalLines += fMeta.LineCount

		ext := strings.ToLower(filepath.Ext(relPath))
		if ext == "" { ext = "[no ext]" }
		if _, ok := stats.ExtMap[ext]; !ok { stats.ExtMap[ext] = &ExtStats{Ext: ext} }
		stats.ExtMap[ext].FileCount++
		stats.ExtMap[ext].TotalSize += fMeta.Size
		stats.ExtMap[ext].TotalLines += fMeta.LineCount

		return nil
	})

	return files, stats, skipped
}

func shouldIgnoreDir(relPath string, config Config, gitPatterns []string) bool {
	name := filepath.Base(relPath)
	if !config.NoDefaultIgnore && ignoreDirs[name] { return true }
	for _, p := range config.AdditionalIgnores { if matchPattern(relPath, p) { return true } }
	for _, p := range gitPatterns { if matchPattern(relPath, p) { return true } }
	return false
}

func shouldIgnoreFile(relPath string, config Config, gitPatterns []string) bool {
	name := filepath.Base(relPath)
	ext := filepath.Ext(relPath)
	if !config.NoDefaultIgnore && ignoreFiles[name] { return true }
	if len(config.IncludeExts) > 0 {
		match := false
		for _, e := range config.IncludeExts { if strings.EqualFold(ext, e) { match = true; break } }
		if !match { return true }
	}
	if len(config.IncludeMatches) > 0 {
		match := false
		for _, m := range config.IncludeMatches { if strings.Contains(relPath, m) { match = true; break } }
		if !match { return true }
	}
	for _, e := range config.ExcludeExts { if strings.EqualFold(ext, e) { return true } }
	for _, m := range config.ExcludeMatches { if strings.Contains(relPath, m) { return true } }
	for _, p := range gitPatterns { if matchPattern(relPath, p) { return true } }
	return false
}

func loadGitignore(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil { return nil }
	var res []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		l := strings.TrimSpace(scanner.Text())
		if l != "" && !strings.HasPrefix(l, "#") { res = append(res, l) }
	}
	return res
}

func matchPattern(path, pattern string) bool {
	m, _ := filepath.Match(pattern, filepath.Base(path))
	return m || strings.HasPrefix(path, pattern)
}

func isBinaryFile(path string) bool {
	f, _ := os.Open(path)
	defer f.Close()
	buf := make([]byte, 1024)
	n, _ := f.Read(buf)
	if n == 0 { return false }
	if !utf8.Valid(buf[:n]) {
		for _, b := range buf[:n] { if b == 0 { return true } }
	}
	return false
}

func countLines(path string) int {
	f, _ := os.Open(path)
	defer f.Close()
	s := bufio.NewScanner(f)
	n := 0
	for s.Scan() { n++ }
	return n
}

// ============================================================
//  Statistics & Output
// ============================================================

func printStatsTerminal(files []FileMetadata, stats Stats) {
	fmt.Printf("\n📊 Project Statistics Summary\n")
	fmt.Printf("  %-20s %d\n", "Files Processed:", stats.FileCount)
	fmt.Printf("  %-20s %d\n", "Total Lines:", stats.TotalLines)
	fmt.Printf("  %-20s %.2f KB\n", "Total Size:", float64(stats.TotalSize)/1024)
	fmt.Printf("  %-20s %d\n", "Directories:", stats.DirCount)

	// 1. Top Files by Line Count
	sort.Slice(files, func(i, j int) bool { return files[i].LineCount > files[j].LineCount })
	fmt.Printf("\n%-45s %12s %12s\n", "🔝 Top Files (by Lines):", "Lines", "Size")
	fmt.Printf("%-45s %12s %12s\n", "---------------------------------------------", "-----", "----")
	for i := 0; i < len(files) && i < 5; i++ {
		fmt.Printf("%-45s %12d %11.2f KB\n", files[i].RelPath, files[i].LineCount, float64(files[i].Size)/1024)
	}

	// 2. Directory Dimension
	dirs := make([]*DirStats, 0, len(stats.DirMap))
	for _, d := range stats.DirMap { dirs = append(dirs, d) }
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].TotalLines > dirs[j].TotalLines })
	fmt.Printf("\n%-30s %8s %12s %10s %12s %10s\n", "📁 Folder Dimension:", "Files", "Lines", "Lines %", "Size", "Size %")
	fmt.Printf("%-30s %8s %12s %10s %12s %10s\n", "------------------------------", "-------", "-----------", "-------", "-----------", "-------")
	for i := 0; i < len(dirs) && i < 10; i++ {
		linePct := 0.0
		if stats.TotalLines > 0 { linePct = float64(dirs[i].TotalLines) / float64(stats.TotalLines) * 100 }
		sizePct := 0.0
		if stats.TotalSize > 0 { sizePct = float64(dirs[i].TotalSize) / float64(stats.TotalSize) * 100 }
		fmt.Printf("%-30s %8d %12d %9.1f%% %11.2f KB %9.1f%%\n", 
			dirs[i].Path, dirs[i].FileCount, dirs[i].TotalLines, linePct, float64(dirs[i].TotalSize)/1024, sizePct)
	}

	// 3. Language Dimension
	exts := make([]*ExtStats, 0, len(stats.ExtMap))
	for _, e := range stats.ExtMap { exts = append(exts, e) }
	sort.Slice(exts, func(i, j int) bool { return exts[i].TotalLines > exts[j].TotalLines })
	fmt.Printf("\n%-15s %8s %12s %10s %12s %10s\n", "📝 Language:", "Files", "Lines", "Lines %", "Size", "Size %")
	fmt.Printf("%-15s %8s %12s %10s %12s %10s\n", "---------------", "-------", "-----------", "-------", "-----------", "-------")
	for _, e := range exts {
		linePct := 0.0
		if stats.TotalLines > 0 { linePct = float64(e.TotalLines) / float64(stats.TotalLines) * 100 }
		sizePct := 0.0
		if stats.TotalSize > 0 { sizePct = float64(e.TotalSize) / float64(stats.TotalSize) * 100 }
		fmt.Printf("%-15s %8d %12d %9.1f%% %11.2f KB %9.1f%%\n", 
			e.Ext, e.FileCount, e.TotalLines, linePct, float64(e.TotalSize)/1024, sizePct)
	}
}

func generateOutput(config Config, files []FileMetadata, stats Stats) error {
	out, err := os.Create(config.OutputFile)
	if err != nil { return err }
	defer out.Close()
	w := bufio.NewWriter(out)
	defer w.Flush()

	fmt.Fprintf(w, "# Project Snapshot: %s\n\n", filepath.Base(config.RootDir))
	fmt.Fprintf(w, "> Generated by [Gdox](https://github.com/yuanguangshan/gdox) on %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

	fmt.Fprintln(w, "## Table of Contents")
	sort.Slice(files, func(i, j int) bool { return files[i].RelPath < files[j].RelPath })
	for _, f := range files {
		fmt.Fprintf(w, "- [%s](#%s)\n", f.RelPath, generateAnchor(f.RelPath))
	}
	fmt.Fprintln(w, "\n---")

	for _, f := range files {
		fmt.Fprintf(w, "\n## %s\n\n", f.RelPath)
		c, _ := os.ReadFile(f.FullPath)
		lang := detectLanguage(f.RelPath)
		fence := determineFence(string(c))
		fmt.Fprintf(w, "%s%s\n%s%s\n%s\n", fence, lang, string(c), func()string{if len(c)>0 && c[len(c)-1]!='\n'{return "\n"} ; return ""}(), fence)
	}

	if config.ShowStats {
		fmt.Fprintln(w, "\n---")
		fmt.Fprintln(w, "## Detailed Project Audit")
		
		fmt.Fprintln(w, "### 📈 Overview")
		fmt.Fprintf(w, "- Files: %d\n- Lines: %d\n- Size: %.2f KB\n\n", stats.FileCount, stats.TotalLines, float64(stats.TotalSize)/1024)

		fmt.Fprintln(w, "### 📁 Directory Distribution")
		fmt.Fprintln(w, "| Directory | Files | Lines | Lines % | Size | Size % |")
		fmt.Fprintln(w, "| :--- | :---: | :---: | :---: | :---: | :---: |")
		dirs := make([]*DirStats, 0, len(stats.DirMap))
		for _, d := range stats.DirMap { dirs = append(dirs, d) }
		sort.Slice(dirs, func(i, j int) bool { return dirs[i].Path < dirs[j].Path })
		for _, d := range dirs {
			linePct := 0.0
			if stats.TotalLines > 0 { linePct = float64(d.TotalLines) / float64(stats.TotalLines) * 100 }
			sizePct := 0.0
			if stats.TotalSize > 0 { sizePct = float64(d.TotalSize) / float64(stats.TotalSize) * 100 }
			fmt.Fprintf(w, "| %s | %d | %d | %.1f%% | %.2f KB | %.1f%% |\n", d.Path, d.FileCount, d.TotalLines, linePct, float64(d.TotalSize)/1024, sizePct)
		}

		fmt.Fprintln(w, "\n### 📝 Language Breakdown")
		fmt.Fprintln(w, "| Extension | Files | Lines | Lines % | Size | Size % |")
		fmt.Fprintln(w, "| :--- | :---: | :---: | :---: | :---: | :---: |")
		exts := make([]*ExtStats, 0, len(stats.ExtMap))
		for _, e := range stats.ExtMap { exts = append(exts, e) }
		sort.Slice(exts, func(i, j int) bool { return exts[i].TotalLines > exts[j].TotalLines })
		for _, e := range exts {
			linePct := 0.0
			if stats.TotalLines > 0 { linePct = float64(e.TotalLines) / float64(stats.TotalLines) * 100 }
			sizePct := 0.0
			if stats.TotalSize > 0 { sizePct = float64(e.TotalSize) / float64(stats.TotalSize) * 100 }
			fmt.Fprintf(w, "| %s | %d | %d | %.1f%% | %.2f KB | %.1f%% |\n", e.Ext, e.FileCount, e.TotalLines, linePct, float64(e.TotalSize)/1024, sizePct)
		}
	}
	return nil
}

func generateAnchor(p string) string {
	return strings.NewReplacer("/", "-", "\\", "-", ".", "-").Replace(strings.ToLower(p))
}

func detectLanguage(p string) string {
	ext := strings.ToLower(filepath.Ext(p))
	if l, ok := languageMap[ext]; ok { return l }
	base := strings.ToLower(filepath.Base(p))
	if base == "dockerfile" || base == "makefile" { return base }
	return ""
}

func determineFence(c string) string {
	max := 0
	cur := 0
	for _, r := range c {
		if r == '`' { cur++; if cur > max { max = cur } } else { cur = 0 }
	}
	if max < 3 { return "```" }
	return strings.Repeat("`", max+1)
}

func printDryRun(files []FileMetadata, stats Stats, skipped []SkippedFile) {
	fmt.Printf("\n🔍 Files to be included (%d):\n", len(files))
	for _, f := range files { fmt.Printf("  - %-40s (%d lines)\n", f.RelPath, f.LineCount) }
	if len(skipped) > 0 {
		fmt.Printf("\n⏭  Skipped:\n")
		for _, s := range skipped { fmt.Printf("  - %-40s [%s]\n", s.RelPath, s.Reason) }
	}
}
