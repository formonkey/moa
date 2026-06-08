// Package developer provides a "Developer Agent" skill that combines
// file operations, shell execution, and code graph awareness into a
// ready-to-use agent preset.
//
// It auto-detects the project language and framework, loads the code graph
// for codebase awareness, and generates an intelligent system prompt that
// teaches the agent to explore the graph before making changes.
package developer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectInfo contains detected project metadata.
type ProjectInfo struct {
	// Language is the primary language (e.g., "go", "typescript", "python", "rust",
	// "java", "kotlin", "csharp", "php", "ruby", "swift", "dart").
	Language string `json:"language"`
	// Framework is the detected framework (e.g., "gin", "next", "angular", "spring-boot",
	// "laravel", "aspnet", "rails", "django", "fastapi", "flutter").
	Framework string `json:"framework,omitempty"`
	// EntryPoints are the main files/packages detected.
	EntryPoints []string `json:"entry_points,omitempty"`
	// Structure is a human-readable directory tree (1 level deep).
	Structure string `json:"structure,omitempty"`
	// ModuleName is the module name (e.g., Go module path, npm package name).
	ModuleName string `json:"module_name,omitempty"`
	// HasCodegraph indicates whether a .codegraph/ directory exists.
	HasCodegraph bool `json:"has_codegraph"`
}

// Detect inspects a project directory and returns metadata about the project.
// It examines manifest files (go.mod, package.json, etc.) to determine
// language, framework, and entry points.
//
// Supported languages: Go, TypeScript/JavaScript, Python, Rust, Java, Kotlin,
// C#, PHP, Ruby, Swift, Dart.
func Detect(projectDir string) (*ProjectInfo, error) {
	absDir, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, fmt.Errorf("developer: cannot resolve path: %w", err)
	}

	info := &ProjectInfo{}

	// Check for codegraph
	if stat, err := os.Stat(filepath.Join(absDir, ".codegraph")); err == nil && stat.IsDir() {
		info.HasCodegraph = true
	}

	// Generate structure tree
	info.Structure = buildStructureTree(absDir)

	// Try each detector in priority order
	detectors := []func(string, *ProjectInfo) bool{
		detectGo,
		detectNodeTS,
		detectPython,
		detectRust,
		detectJava,
		detectKotlin,
		detectCSharp,
		detectPHP,
		detectRuby,
		detectSwift,
		detectDart,
	}

	for _, detect := range detectors {
		if detect(absDir, info) {
			break
		}
	}

	// Fallback: if nothing detected, try to infer from file extensions
	if info.Language == "" {
		info.Language = inferFromExtensions(absDir)
	}

	return info, nil
}

// --- Go detection ---

func detectGo(dir string, info *ProjectInfo) bool {
	gomodPath := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(gomodPath)
	if err != nil {
		return false
	}

	info.Language = "go"
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			info.ModuleName = strings.TrimPrefix(line, "module ")
			break
		}
	}

	// Detect frameworks from go.mod requires
	content := strings.ToLower(string(data))
	frameworks := map[string]string{
		"github.com/gin-gonic/gin":  "gin",
		"github.com/labstack/echo":  "echo",
		"github.com/gofiber/fiber":  "fiber",
		"github.com/go-chi/chi":     "chi",
		"github.com/gorilla/mux":    "gorilla",
		"github.com/beego/beego":    "beego",
		"gorm.io/gorm":              "gorm",
		"entgo.io/ent":              "ent",
	}
	for pkg, fw := range frameworks {
		if strings.Contains(content, pkg) {
			info.Framework = fw
			break
		}
	}

	// Find entry points
	info.EntryPoints = findEntryPoints(dir, "main.go")
	if len(info.EntryPoints) == 0 {
		// Check cmd/ pattern
		cmdDir := filepath.Join(dir, "cmd")
		if entries, err := os.ReadDir(cmdDir); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					mainPath := filepath.Join("cmd", e.Name(), "main.go")
					if _, err := os.Stat(filepath.Join(dir, mainPath)); err == nil {
						info.EntryPoints = append(info.EntryPoints, mainPath)
					}
				}
			}
		}
	}

	return true
}

// --- Node/TypeScript detection ---

func detectNodeTS(dir string, info *ProjectInfo) bool {
	pkgPath := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return false
	}

	var pkg struct {
		Name         string            `json:"name"`
		Dependencies map[string]string `json:"dependencies"`
		DevDeps      map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}

	info.ModuleName = pkg.Name

	// Check for TypeScript
	allDeps := mergeMaps(pkg.Dependencies, pkg.DevDeps)
	if _, ok := allDeps["typescript"]; ok {
		info.Language = "typescript"
	} else {
		info.Language = "javascript"
	}

	// Detect frameworks
	frameworkPriority := []struct {
		pkg       string
		framework string
	}{
		{"next", "next"},
		{"@angular/core", "angular"},
		{"nuxt", "nuxt"},
		{"vue", "vue"},
		{"svelte", "svelte"},
		{"@sveltejs/kit", "sveltekit"},
		{"react", "react"},
		{"express", "express"},
		{"fastify", "fastify"},
		{"nest", "nestjs"},
		{"@nestjs/core", "nestjs"},
		{"hono", "hono"},
		{"koa", "koa"},
		{"@remix-run/react", "remix"},
		{"astro", "astro"},
	}
	for _, fp := range frameworkPriority {
		if _, ok := allDeps[fp.pkg]; ok {
			info.Framework = fp.framework
			break
		}
	}

	// Find entry points
	entryFiles := []string{
		"src/index.ts", "src/index.tsx", "src/main.ts", "src/main.tsx",
		"src/app.ts", "src/app.tsx", "src/index.js", "src/main.js",
		"index.ts", "index.js", "app.ts", "app.js",
		"pages/index.tsx", "app/page.tsx", "app/layout.tsx",
	}
	for _, ep := range entryFiles {
		if _, err := os.Stat(filepath.Join(dir, ep)); err == nil {
			info.EntryPoints = append(info.EntryPoints, ep)
		}
	}

	return true
}

// --- Python detection ---

func detectPython(dir string, info *ProjectInfo) bool {
	// Try pyproject.toml first
	pyprojectPath := filepath.Join(dir, "pyproject.toml")
	if data, err := os.ReadFile(pyprojectPath); err == nil {
		info.Language = "python"
		content := string(data)

		// Detect frameworks
		frameworks := map[string]string{
			"fastapi":  "fastapi",
			"django":   "django",
			"flask":    "flask",
			"starlette": "starlette",
			"tornado":  "tornado",
			"aiohttp":  "aiohttp",
			"litestar": "litestar",
			"sanic":    "sanic",
		}
		for pkg, fw := range frameworks {
			if strings.Contains(strings.ToLower(content), pkg) {
				info.Framework = fw
				break
			}
		}

		// Entry points
		info.EntryPoints = findEntryPoints(dir, "main.py", "app.py", "manage.py", "wsgi.py", "asgi.py")
		return true
	}

	// Try requirements.txt
	reqPath := filepath.Join(dir, "requirements.txt")
	if data, err := os.ReadFile(reqPath); err == nil {
		info.Language = "python"
		content := strings.ToLower(string(data))

		frameworks := map[string]string{
			"fastapi":  "fastapi",
			"django":   "django",
			"flask":    "flask",
			"sanic":    "sanic",
			"litestar": "litestar",
		}
		for pkg, fw := range frameworks {
			if strings.Contains(content, pkg) {
				info.Framework = fw
				break
			}
		}

		info.EntryPoints = findEntryPoints(dir, "main.py", "app.py", "manage.py", "wsgi.py", "asgi.py")
		return true
	}

	return false
}

// --- Rust detection ---

func detectRust(dir string, info *ProjectInfo) bool {
	cargoPath := filepath.Join(dir, "Cargo.toml")
	data, err := os.ReadFile(cargoPath)
	if err != nil {
		return false
	}

	info.Language = "rust"
	content := strings.ToLower(string(data))

	// Parse package name (simple)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name") && strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				info.ModuleName = strings.Trim(strings.TrimSpace(parts[1]), "\"")
				break
			}
		}
	}

	// Detect frameworks
	frameworks := map[string]string{
		"actix-web": "actix",
		"axum":      "axum",
		"rocket":    "rocket",
		"warp":      "warp",
		"tauri":     "tauri",
	}
	for pkg, fw := range frameworks {
		if strings.Contains(content, pkg) {
			info.Framework = fw
			break
		}
	}

	info.EntryPoints = findEntryPoints(dir, "src/main.rs", "src/lib.rs")
	return true
}

// --- Java detection ---

func detectJava(dir string, info *ProjectInfo) bool {
	// Maven (pom.xml)
	pomPath := filepath.Join(dir, "pom.xml")
	if data, err := os.ReadFile(pomPath); err == nil {
		info.Language = "java"
		content := strings.ToLower(string(data))

		// Extract artifactId as module name
		if idx := strings.Index(content, "<artifactid>"); idx >= 0 {
			rest := content[idx+len("<artifactid>"):]
			if end := strings.Index(rest, "</artifactid>"); end >= 0 {
				info.ModuleName = rest[:end]
			}
		}

		frameworks := map[string]string{
			"spring-boot":          "spring-boot",
			"spring-webflux":       "spring-webflux",
			"quarkus":              "quarkus",
			"micronaut":            "micronaut",
			"jakarta.faces":        "jsf",
			"javax.servlet":        "servlet",
			"dropwizard":           "dropwizard",
			"vert.x":               "vertx",
			"play-java":            "play",
			"struts":               "struts",
		}
		for pkg, fw := range frameworks {
			if strings.Contains(content, pkg) {
				info.Framework = fw
				break
			}
		}

		info.EntryPoints = findJavaEntryPoints(dir)
		return true
	}

	// Gradle (build.gradle or build.gradle.kts)
	for _, gradleFile := range []string{"build.gradle", "build.gradle.kts"} {
		gradlePath := filepath.Join(dir, gradleFile)
		if data, err := os.ReadFile(gradlePath); err == nil {
			info.Language = "java"
			content := strings.ToLower(string(data))

			// Check if it's Kotlin DSL (likely a Kotlin project)
			if strings.HasSuffix(gradleFile, ".kts") {
				if strings.Contains(content, "kotlin(") || strings.Contains(content, "org.jetbrains.kotlin") {
					info.Language = "kotlin"
				}
			}

			frameworks := map[string]string{
				"spring-boot":           "spring-boot",
				"org.springframework":    "spring-boot",
				"quarkus":               "quarkus",
				"micronaut":             "micronaut",
				"io.vertx":              "vertx",
				"android":               "android",
				"com.android":           "android",
				"ktor":                  "ktor",
				"compose":               "compose",
			}
			for pkg, fw := range frameworks {
				if strings.Contains(content, pkg) {
					info.Framework = fw
					break
				}
			}

			// Extract module name from settings.gradle
			settingsPath := filepath.Join(dir, "settings.gradle")
			if sdata, err := os.ReadFile(settingsPath); err == nil {
				for _, line := range strings.Split(string(sdata), "\n") {
					line = strings.TrimSpace(line)
					if strings.HasPrefix(line, "rootProject.name") {
						parts := strings.SplitN(line, "=", 2)
						if len(parts) == 2 {
							info.ModuleName = strings.Trim(strings.TrimSpace(parts[1]), "'\"")
						}
					}
				}
			}

			info.EntryPoints = findJavaEntryPoints(dir)
			return true
		}
	}

	return false
}

func findJavaEntryPoints(dir string) []string {
	var entries []string
	// Standard Maven/Gradle entry points
	candidates := []string{
		"src/main/java", "src/main/kotlin",
	}
	for _, c := range candidates {
		mainDir := filepath.Join(dir, c)
		_ = filepath.WalkDir(mainDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, "Application.java") ||
				strings.HasSuffix(path, "Application.kt") ||
				strings.HasSuffix(path, "Main.java") ||
				strings.HasSuffix(path, "Main.kt") {
				if rel, err := filepath.Rel(dir, path); err == nil {
					entries = append(entries, rel)
				}
			}
			return nil
		})
	}
	return entries
}

// --- Kotlin detection (standalone, not Gradle-based) ---

func detectKotlin(dir string, info *ProjectInfo) bool {
	// Kotlin multiplatform or standalone (build.gradle.kts with kotlin plugin)
	// Already handled in detectJava for Gradle projects.
	// This handles pure Kotlin projects with no Gradle (rare but possible).
	for _, gradleFile := range []string{"build.gradle.kts"} {
		gradlePath := filepath.Join(dir, gradleFile)
		if data, err := os.ReadFile(gradlePath); err == nil {
			content := strings.ToLower(string(data))
			if strings.Contains(content, "kotlin") {
				info.Language = "kotlin"

				frameworks := map[string]string{
					"ktor":          "ktor",
					"spring-boot":   "spring-boot",
					"compose":       "compose",
					"android":       "android",
					"kotlinx.coroutines": "coroutines",
				}
				for pkg, fw := range frameworks {
					if strings.Contains(content, pkg) {
						info.Framework = fw
						break
					}
				}

				info.EntryPoints = findJavaEntryPoints(dir)
				return true
			}
		}
	}
	return false
}

// --- C# detection ---

func detectCSharp(dir string, info *ProjectInfo) bool {
	// Look for *.csproj or *.sln files
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".csproj") {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				continue
			}

			info.Language = "csharp"
			info.ModuleName = strings.TrimSuffix(name, ".csproj")
			content := strings.ToLower(string(data))

			frameworks := map[string]string{
				"microsoft.aspnetcore":     "aspnet",
				"microsoft.net.sdk.web":    "aspnet",
				"microsoft.net.sdk.blazorwebassembly": "blazor",
				"microsoft.maui":            "maui",
				"avalonia":                  "avalonia",
				"xamarin":                   "xamarin",
				"unity":                     "unity",
			}
			for pkg, fw := range frameworks {
				if strings.Contains(content, pkg) {
					info.Framework = fw
					break
				}
			}

			// Entry points
			info.EntryPoints = findEntryPoints(dir, "Program.cs", "Startup.cs",
				"src/Program.cs", "src/Startup.cs")
			return true
		}
		if strings.HasSuffix(name, ".sln") {
			info.Language = "csharp"
			info.ModuleName = strings.TrimSuffix(name, ".sln")
			info.EntryPoints = findEntryPoints(dir, "Program.cs", "Startup.cs")
			return true
		}
	}

	return false
}

// --- PHP detection ---

func detectPHP(dir string, info *ProjectInfo) bool {
	composerPath := filepath.Join(dir, "composer.json")
	data, err := os.ReadFile(composerPath)
	if err != nil {
		return false
	}

	info.Language = "php"

	var composer struct {
		Name    string            `json:"name"`
		Require map[string]string `json:"require"`
	}
	if err := json.Unmarshal(data, &composer); err == nil {
		info.ModuleName = composer.Name

		frameworkPriority := []struct {
			pkg       string
			framework string
		}{
			{"laravel/framework", "laravel"},
			{"symfony/framework-bundle", "symfony"},
			{"symfony/http-kernel", "symfony"},
			{"cakephp/cakephp", "cakephp"},
			{"codeigniter4/framework", "codeigniter"},
			{"yiisoft/yii2", "yii"},
			{"slim/slim", "slim"},
			{"wp-cli/wp-cli", "wordpress"},
			{"drupal/core", "drupal"},
			{"statamic/cms", "statamic"},
		}
		for _, fp := range frameworkPriority {
			if _, ok := composer.Require[fp.pkg]; ok {
				info.Framework = fp.framework
				break
			}
		}
	}

	// Check for artisan (Laravel marker)
	if info.Framework == "" {
		if _, err := os.Stat(filepath.Join(dir, "artisan")); err == nil {
			info.Framework = "laravel"
		}
	}

	// Entry points
	info.EntryPoints = findEntryPoints(dir,
		"public/index.php", "index.php", "artisan",
		"bin/console", "web/app.php", "web/index.php")
	return true
}

// --- Ruby detection ---

func detectRuby(dir string, info *ProjectInfo) bool {
	gemfilePath := filepath.Join(dir, "Gemfile")
	data, err := os.ReadFile(gemfilePath)
	if err != nil {
		return false
	}

	info.Language = "ruby"
	content := strings.ToLower(string(data))

	frameworks := map[string]string{
		"rails":     "rails",
		"sinatra":   "sinatra",
		"hanami":    "hanami",
		"roda":      "roda",
		"grape":     "grape",
		"padrino":   "padrino",
		"jekyll":    "jekyll",
	}
	for pkg, fw := range frameworks {
		if strings.Contains(content, pkg) {
			info.Framework = fw
			break
		}
	}

	// Parse gemspec or Gemfile for project name
	gemspecFiles, _ := filepath.Glob(filepath.Join(dir, "*.gemspec"))
	if len(gemspecFiles) > 0 {
		info.ModuleName = strings.TrimSuffix(filepath.Base(gemspecFiles[0]), ".gemspec")
	}

	info.EntryPoints = findEntryPoints(dir,
		"config.ru", "app.rb", "config/application.rb",
		"bin/rails", "Rakefile")
	return true
}

// --- Swift detection ---

func detectSwift(dir string, info *ProjectInfo) bool {
	// Swift Package Manager
	spmPath := filepath.Join(dir, "Package.swift")
	if data, err := os.ReadFile(spmPath); err == nil {
		info.Language = "swift"
		content := string(data)

		// Extract package name
		if idx := strings.Index(content, "name:"); idx >= 0 {
			rest := content[idx:]
			if qStart := strings.Index(rest, "\""); qStart >= 0 {
				rest = rest[qStart+1:]
				if qEnd := strings.Index(rest, "\""); qEnd >= 0 {
					info.ModuleName = rest[:qEnd]
				}
			}
		}

		lower := strings.ToLower(content)
		frameworks := map[string]string{
			"vapor":        "vapor",
			"hummingbird":  "hummingbird",
			"kitura":       "kitura",
			"perfect":      "perfect",
		}
		for pkg, fw := range frameworks {
			if strings.Contains(lower, pkg) {
				info.Framework = fw
				break
			}
		}

		info.EntryPoints = findEntryPoints(dir, "Sources/main.swift",
			"Sources/App/main.swift", "Sources/Run/main.swift")
		return true
	}

	// Xcode project
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".xcodeproj") || strings.HasSuffix(e.Name(), ".xcworkspace") {
			info.Language = "swift"
			info.ModuleName = strings.TrimSuffix(strings.TrimSuffix(e.Name(), ".xcodeproj"), ".xcworkspace")
			// Check for SwiftUI
			if _, err := os.Stat(filepath.Join(dir, "ContentView.swift")); err == nil {
				info.Framework = "swiftui"
			}
			info.EntryPoints = findEntryPoints(dir, "AppDelegate.swift",
				"ContentView.swift", "App.swift")
			return true
		}
	}

	return false
}

// --- Dart/Flutter detection ---

func detectDart(dir string, info *ProjectInfo) bool {
	pubspecPath := filepath.Join(dir, "pubspec.yaml")
	data, err := os.ReadFile(pubspecPath)
	if err != nil {
		return false
	}

	info.Language = "dart"
	content := strings.ToLower(string(data))

	// Parse name
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			info.ModuleName = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
			break
		}
	}

	if strings.Contains(content, "flutter") {
		info.Framework = "flutter"
	} else {
		// Server-side Dart frameworks
		frameworks := map[string]string{
			"shelf":       "shelf",
			"aqueduct":    "aqueduct",
			"angel":       "angel",
			"dart_frog":   "dart_frog",
			"serverpod":   "serverpod",
		}
		for pkg, fw := range frameworks {
			if strings.Contains(content, pkg) {
				info.Framework = fw
				break
			}
		}
	}

	info.EntryPoints = findEntryPoints(dir, "lib/main.dart", "bin/main.dart",
		"bin/server.dart", "web/main.dart")
	return true
}

// --- Helpers ---

func findEntryPoints(dir string, candidates ...string) []string {
	var found []string
	for _, c := range candidates {
		full := filepath.Join(dir, c)
		if _, err := os.Stat(full); err == nil {
			found = append(found, c)
		}
	}
	return found
}

func buildStructureTree(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "(unable to read directory)"
	}

	// Directories to skip
	skip := map[string]bool{
		"node_modules": true, ".git": true, ".angular": true,
		"dist": true, ".next": true, "__pycache__": true,
		".venv": true, "target": true, "vendor": true,
		".codegraph": true, ".idea": true, ".vscode": true,
		"bin": false, "obj": false, // C# build dirs
		".gradle": true, ".dart_tool": true, ".pub-cache": true,
		"build": true, "Pods": true,
	}

	var dirs []string
	var files []string

	for _, e := range entries {
		name := e.Name()
		if skip[name] {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, name+"/")
		} else {
			files = append(files, name)
		}
	}

	sort.Strings(dirs)
	sort.Strings(files)

	var sb strings.Builder
	for _, d := range dirs {
		sb.WriteString("  " + d + "\n")
	}
	for _, f := range files {
		sb.WriteString("  " + f + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func mergeMaps(a, b map[string]string) map[string]string {
	merged := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		merged[k] = v
	}
	for k, v := range b {
		merged[k] = v
	}
	return merged
}

func inferFromExtensions(dir string) string {
	counts := make(map[string]int)
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			// Skip common non-source dirs
			name := d.Name()
			if d.IsDir() && (name == "node_modules" || name == ".git" || name == "vendor" ||
				name == "target" || name == "build" || name == "Pods" ||
				name == ".gradle" || name == ".dart_tool") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".go":
			counts["go"]++
		case ".ts", ".tsx":
			counts["typescript"]++
		case ".js", ".jsx":
			counts["javascript"]++
		case ".py":
			counts["python"]++
		case ".rs":
			counts["rust"]++
		case ".java":
			counts["java"]++
		case ".kt", ".kts":
			counts["kotlin"]++
		case ".cs":
			counts["csharp"]++
		case ".php":
			counts["php"]++
		case ".rb":
			counts["ruby"]++
		case ".swift":
			counts["swift"]++
		case ".dart":
			counts["dart"]++
		}
		return nil
	})

	best := ""
	bestCount := 0
	for lang, count := range counts {
		if count > bestCount {
			best = lang
			bestCount = count
		}
	}
	return best
}

