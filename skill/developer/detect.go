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
	// Language is the primary language (e.g., "go", "typescript", "python", "rust").
	Language string `json:"language"`
	// Framework is the detected framework (e.g., "gin", "next", "angular", "fastapi").
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
		}
		for pkg, fw := range frameworks {
			if strings.Contains(strings.ToLower(content), pkg) {
				info.Framework = fw
				break
			}
		}

		// Entry points
		info.EntryPoints = findEntryPoints(dir, "main.py", "app.py", "manage.py")
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
		}
		for pkg, fw := range frameworks {
			if strings.Contains(content, pkg) {
				info.Framework = fw
				break
			}
		}

		info.EntryPoints = findEntryPoints(dir, "main.py", "app.py", "manage.py")
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
			if d.IsDir() && (name == "node_modules" || name == ".git" || name == "vendor" || name == "target") {
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
