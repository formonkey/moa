package developer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Detect tests ---

func TestDetectGo(t *testing.T) {
	dir := t.TempDir()

	// Create a go.mod
	gomod := `module github.com/example/myapp

go 1.22

require (
	github.com/gin-gonic/gin v1.9.0
	github.com/stretchr/testify v1.8.0
)
`
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "internal"), 0o755)
	os.MkdirAll(filepath.Join(dir, "cmd", "server"), 0o755)

	info, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if info.Language != "go" {
		t.Errorf("expected language 'go', got %q", info.Language)
	}
	if info.Framework != "gin" {
		t.Errorf("expected framework 'gin', got %q", info.Framework)
	}
	if info.ModuleName != "github.com/example/myapp" {
		t.Errorf("expected module name 'github.com/example/myapp', got %q", info.ModuleName)
	}
	if len(info.EntryPoints) == 0 {
		t.Error("expected at least one entry point")
	}
	if info.Structure == "" {
		t.Error("expected non-empty structure")
	}
}

func TestDetectTypeScript(t *testing.T) {
	dir := t.TempDir()

	pkg := `{
  "name": "my-next-app",
  "dependencies": {
    "next": "14.0.0",
    "react": "18.2.0",
    "react-dom": "18.2.0"
  },
  "devDependencies": {
    "typescript": "5.0.0"
  }
}`
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644)
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "index.ts"), []byte(""), 0o644)

	info, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if info.Language != "typescript" {
		t.Errorf("expected language 'typescript', got %q", info.Language)
	}
	if info.Framework != "next" {
		t.Errorf("expected framework 'next', got %q", info.Framework)
	}
	if info.ModuleName != "my-next-app" {
		t.Errorf("expected module name 'my-next-app', got %q", info.ModuleName)
	}
}

func TestDetectPython(t *testing.T) {
	dir := t.TempDir()

	pyproject := `[project]
name = "myapi"
dependencies = [
    "fastapi>=0.100",
    "uvicorn",
]
`
	os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(pyproject), 0o644)
	os.WriteFile(filepath.Join(dir, "main.py"), []byte(""), 0o644)

	info, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if info.Language != "python" {
		t.Errorf("expected language 'python', got %q", info.Language)
	}
	if info.Framework != "fastapi" {
		t.Errorf("expected framework 'fastapi', got %q", info.Framework)
	}
}

func TestDetectRust(t *testing.T) {
	dir := t.TempDir()

	cargo := `[package]
name = "my-server"
version = "0.1.0"

[dependencies]
axum = "0.7"
tokio = { version = "1", features = ["full"] }
`
	os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(cargo), 0o644)
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "main.rs"), []byte(""), 0o644)

	info, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if info.Language != "rust" {
		t.Errorf("expected language 'rust', got %q", info.Language)
	}
	if info.Framework != "axum" {
		t.Errorf("expected framework 'axum', got %q", info.Framework)
	}
	if info.ModuleName != "my-server" {
		t.Errorf("expected module name 'my-server', got %q", info.ModuleName)
	}
}

func TestDetectCodegraph(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".codegraph"), 0o755)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o644)

	info, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}

	if !info.HasCodegraph {
		t.Error("expected HasCodegraph to be true")
	}
}

func TestDetectUnknown(t *testing.T) {
	dir := t.TempDir()
	// Empty dir — should not error
	info, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect failed on empty dir: %v", err)
	}
	if info.Language != "" {
		// Empty dir should produce empty language or inferred
		t.Logf("inferred language: %q", info.Language)
	}
}

func TestDetectGoCmdPattern(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.22\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "cmd", "server"), 0o755)
	os.WriteFile(filepath.Join(dir, "cmd", "server", "main.go"), []byte("package main\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "cmd", "worker"), 0o755)
	os.WriteFile(filepath.Join(dir, "cmd", "worker", "main.go"), []byte("package main\n"), 0o644)

	info, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(info.EntryPoints) < 2 {
		t.Errorf("expected at least 2 entry points (cmd/server, cmd/worker), got %d: %v",
			len(info.EntryPoints), info.EntryPoints)
	}
}

func TestDetectJavaMaven(t *testing.T) {
	dir := t.TempDir()

	pom := `<?xml version="1.0"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapi</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-web</artifactId>
    </dependency>
  </dependencies>
</project>`
	os.WriteFile(filepath.Join(dir, "pom.xml"), []byte(pom), 0o644)
	os.MkdirAll(filepath.Join(dir, "src", "main", "java", "com", "example"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "main", "java", "com", "example", "Application.java"), []byte(""), 0o644)

	info, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	if info.Language != "java" {
		t.Errorf("expected language 'java', got %q", info.Language)
	}
	if info.Framework != "spring-boot" {
		t.Errorf("expected framework 'spring-boot', got %q", info.Framework)
	}
	if info.ModuleName != "myapi" {
		t.Errorf("expected module name 'myapi', got %q", info.ModuleName)
	}
	if len(info.EntryPoints) == 0 {
		t.Error("expected at least one entry point")
	}
}

func TestDetectJavaGradle(t *testing.T) {
	dir := t.TempDir()

	gradle := `plugins {
    id 'org.springframework.boot' version '3.2.0'
    id 'java'
}
dependencies {
    implementation 'org.springframework.boot:spring-boot-starter-web'
}
`
	os.WriteFile(filepath.Join(dir, "build.gradle"), []byte(gradle), 0o644)
	os.WriteFile(filepath.Join(dir, "settings.gradle"), []byte("rootProject.name = 'my-gradle-app'\n"), 0o644)

	info, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	if info.Language != "java" {
		t.Errorf("expected language 'java', got %q", info.Language)
	}
	if info.Framework != "spring-boot" {
		t.Errorf("expected framework 'spring-boot', got %q", info.Framework)
	}
	if info.ModuleName != "my-gradle-app" {
		t.Errorf("expected module name 'my-gradle-app', got %q", info.ModuleName)
	}
}

func TestDetectPHP(t *testing.T) {
	dir := t.TempDir()

	composer := `{
  "name": "acme/my-laravel-app",
  "require": {
    "php": "^8.2",
    "laravel/framework": "^11.0"
  }
}`
	os.WriteFile(filepath.Join(dir, "composer.json"), []byte(composer), 0o644)
	os.WriteFile(filepath.Join(dir, "artisan"), []byte("#!/usr/bin/env php\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "public"), 0o755)
	os.WriteFile(filepath.Join(dir, "public", "index.php"), []byte(""), 0o644)

	info, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	if info.Language != "php" {
		t.Errorf("expected language 'php', got %q", info.Language)
	}
	if info.Framework != "laravel" {
		t.Errorf("expected framework 'laravel', got %q", info.Framework)
	}
	if info.ModuleName != "acme/my-laravel-app" {
		t.Errorf("expected module name 'acme/my-laravel-app', got %q", info.ModuleName)
	}
}

func TestDetectCSharp(t *testing.T) {
	dir := t.TempDir()

	csproj := `<Project Sdk="Microsoft.NET.Sdk.Web">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="Microsoft.AspNetCore.OpenApi" Version="8.0.0" />
  </ItemGroup>
</Project>`
	os.WriteFile(filepath.Join(dir, "MyApi.csproj"), []byte(csproj), 0o644)
	os.WriteFile(filepath.Join(dir, "Program.cs"), []byte("var builder = WebApplication.CreateBuilder(args);\n"), 0o644)

	info, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	if info.Language != "csharp" {
		t.Errorf("expected language 'csharp', got %q", info.Language)
	}
	if info.Framework != "aspnet" {
		t.Errorf("expected framework 'aspnet', got %q", info.Framework)
	}
	if info.ModuleName != "MyApi" {
		t.Errorf("expected module name 'MyApi', got %q", info.ModuleName)
	}
}

func TestDetectRuby(t *testing.T) {
	dir := t.TempDir()

	gemfile := `source 'https://rubygems.org'
gem 'rails', '~> 7.1'
gem 'pg'
gem 'puma'
`
	os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(gemfile), 0o644)
	os.MkdirAll(filepath.Join(dir, "config"), 0o755)
	os.WriteFile(filepath.Join(dir, "config", "application.rb"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "config.ru"), []byte(""), 0o644)

	info, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	if info.Language != "ruby" {
		t.Errorf("expected language 'ruby', got %q", info.Language)
	}
	if info.Framework != "rails" {
		t.Errorf("expected framework 'rails', got %q", info.Framework)
	}
}

func TestDetectSwift(t *testing.T) {
	dir := t.TempDir()

	spm := `// swift-tools-version:5.9
import PackageDescription
let package = Package(
    name: "MyVaporApp",
    dependencies: [
        .package(url: "https://github.com/vapor/vapor.git", from: "4.89.0"),
    ]
)
`
	os.WriteFile(filepath.Join(dir, "Package.swift"), []byte(spm), 0o644)
	os.MkdirAll(filepath.Join(dir, "Sources"), 0o755)
	os.WriteFile(filepath.Join(dir, "Sources", "main.swift"), []byte(""), 0o644)

	info, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	if info.Language != "swift" {
		t.Errorf("expected language 'swift', got %q", info.Language)
	}
	if info.Framework != "vapor" {
		t.Errorf("expected framework 'vapor', got %q", info.Framework)
	}
	if info.ModuleName != "MyVaporApp" {
		t.Errorf("expected module name 'MyVaporApp', got %q", info.ModuleName)
	}
}

func TestDetectDartFlutter(t *testing.T) {
	dir := t.TempDir()

	pubspec := `name: my_flutter_app
description: A new Flutter project.

dependencies:
  flutter:
    sdk: flutter
  cupertino_icons: ^1.0.2
`
	os.WriteFile(filepath.Join(dir, "pubspec.yaml"), []byte(pubspec), 0o644)
	os.MkdirAll(filepath.Join(dir, "lib"), 0o755)
	os.WriteFile(filepath.Join(dir, "lib", "main.dart"), []byte(""), 0o644)

	info, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	if info.Language != "dart" {
		t.Errorf("expected language 'dart', got %q", info.Language)
	}
	if info.Framework != "flutter" {
		t.Errorf("expected framework 'flutter', got %q", info.Framework)
	}
	if info.ModuleName != "my_flutter_app" {
		t.Errorf("expected module name 'my_flutter_app', got %q", info.ModuleName)
	}
}

func TestDetectKotlinKtor(t *testing.T) {
	dir := t.TempDir()

	gradle := `plugins {
    kotlin("jvm") version "1.9.0"
    id("io.ktor.plugin") version "2.3.0"
}
dependencies {
    implementation("io.ktor:ktor-server-core")
}
`
	os.WriteFile(filepath.Join(dir, "build.gradle.kts"), []byte(gradle), 0o644)

	info, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	if info.Language != "kotlin" {
		t.Errorf("expected language 'kotlin', got %q", info.Language)
	}
	if info.Framework != "ktor" {
		t.Errorf("expected framework 'ktor', got %q", info.Framework)
	}
}

// --- Directive generation tests ---

func TestBuildDirectiveWithCodegraph(t *testing.T) {
	info := &ProjectInfo{
		Language:     "go",
		Framework:    "gin",
		ModuleName:   "github.com/example/app",
		HasCodegraph: true,
		EntryPoints:  []string{"cmd/server/main.go"},
		Structure:    "  cmd/\n  internal/\n  go.mod",
	}

	directive := buildDirective(info, "Top-level symbols: main, router, handler", "Always use table-driven tests", false)

	checks := []string{
		"Expert go developer",
		"gin project",
		"github.com/example/app",
		"codegraph_search",
		"codegraph_impact",
		"Top-level symbols",
		"table-driven tests",
		"read_file",
		"edit_file",
	}
	for _, check := range checks {
		if !strings.Contains(directive, check) {
			t.Errorf("directive missing %q", check)
		}
	}
}

func TestBuildDirectiveWithoutCodegraph(t *testing.T) {
	info := &ProjectInfo{
		Language:     "typescript",
		Framework:    "next",
		HasCodegraph: false,
	}

	directive := buildDirective(info, "", "", false)

	if strings.Contains(directive, "codegraph") {
		t.Error("directive should not mention codegraph when HasCodegraph is false")
	}
	if !strings.Contains(directive, "search_files") {
		t.Error("directive should mention search_files as fallback")
	}
}

// --- Structure tree tests ---

func TestBuildStructureTree(t *testing.T) {
	dir := t.TempDir()

	// Create some dirs and files
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.MkdirAll(filepath.Join(dir, "internal"), 0o755)
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755) // should be filtered
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(""), 0o644)

	tree := buildStructureTree(dir)

	if strings.Contains(tree, "node_modules") {
		t.Error("structure tree should filter node_modules")
	}
	if !strings.Contains(tree, "src/") {
		t.Error("structure tree should include src/")
	}
	if !strings.Contains(tree, "go.mod") {
		t.Error("structure tree should include go.mod")
	}
}
