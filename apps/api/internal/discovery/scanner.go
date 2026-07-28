package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Scan walks root (which must already be validated via
// projects.ValidateRepositoryPath) and returns deterministic findings.
// It never follows a symlink whose real target falls outside root.
func Scan(root string) ([]Finding, error) {
	c := newCollector()

	err := Walk(root, func(rel string, isDir bool) error {
		name := filepath.Base(rel)
		if isDir {
			classifyDir(c, root, rel, name)
		} else {
			classifyFile(c, root, rel, name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return c.findings(), nil
}

func classifyDir(c *collector, root, rel, name string) {
	switch name {
	case ".maestro":
		c.add(CategoryTestTool, "maestro", rel, ConfidenceHigh, nil)
	case ".detox":
		c.add(CategoryTestTool, "detox", rel, ConfidenceHigh, nil)
	case "migrations":
		if dirHasSuffix(filepath.Join(root, rel), ".sql") {
			c.add(CategoryDatabase, "sql_migrations", rel, ConfidenceHigh, nil)
		}
	}
}

func dirHasSuffix(dir, suffix string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), suffix) {
			return true
		}
	}
	return false
}

func classifyFile(c *collector, root, rel, name string) {
	full := filepath.Join(root, rel)

	switch name {
	case "package.json":
		c.add(CategoryLanguage, "node", rel, ConfidenceHigh, nil)
		classifyPackageJSON(c, full, rel)
	case "go.mod":
		c.add(CategoryLanguage, "go", rel, ConfidenceHigh, nil)
		classifyGoMod(c, full, rel)
	case "requirements.txt":
		c.add(CategoryLanguage, "python", rel, ConfidenceHigh, nil)
		classifyTextForFrameworks(c, full, rel, pythonFrameworkMarkers)
	case "pyproject.toml":
		c.add(CategoryLanguage, "python", rel, ConfidenceHigh, nil)
		classifyTextForFrameworks(c, full, rel, pythonFrameworkMarkers)
	case "Cargo.toml":
		c.add(CategoryLanguage, "rust", rel, ConfidenceHigh, nil)
	case "pom.xml":
		c.add(CategoryLanguage, "java", rel, ConfidenceHigh, nil)
		classifyTextForFrameworks(c, full, rel, javaFrameworkMarkers)
	case "build.gradle", "build.gradle.kts":
		c.add(CategoryLanguage, "java", rel, ConfidenceHigh, nil)
		classifyTextForFrameworks(c, full, rel, javaFrameworkMarkers)
	case "composer.json":
		c.add(CategoryLanguage, "php", rel, ConfidenceHigh, nil)
	case "Jenkinsfile":
		c.add(CategoryCI, "jenkins", rel, ConfidenceHigh, nil)
	case ".gitlab-ci.yml":
		c.add(CategoryCI, "gitlab_ci", rel, ConfidenceHigh, nil)
	case "docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml":
		c.add(CategoryDocker, "docker_compose", rel, ConfidenceHigh, nil)
	case "openapi.yaml", "openapi.yml", "openapi.json":
		c.add(CategoryAPISchema, "openapi", rel, ConfidenceHigh, nil)
	case "swagger.yaml", "swagger.yml", "swagger.json":
		c.add(CategoryAPISchema, "swagger", rel, ConfidenceHigh, nil)
	case "schema.prisma":
		c.add(CategoryAPISchema, "prisma", rel, ConfidenceHigh, nil)
	case "next.config.js", "next.config.ts", "next.config.mjs":
		c.add(CategoryFramework, "next", rel, ConfidenceHigh, nil)
	case "nuxt.config.js", "nuxt.config.ts":
		c.add(CategoryFramework, "nuxt", rel, ConfidenceHigh, nil)
	case "angular.json":
		c.add(CategoryFramework, "angular", rel, ConfidenceHigh, nil)
	case "playwright.config.ts", "playwright.config.js", "playwright.config.mjs", "playwright.config.cjs":
		c.add(CategoryTestTool, "playwright", rel, ConfidenceHigh, nil)
	case "cypress.config.ts", "cypress.config.js", "cypress.config.mjs", "cypress.json":
		c.add(CategoryTestTool, "cypress", rel, ConfidenceHigh, nil)
	}

	if strings.HasPrefix(name, "Dockerfile") {
		c.add(CategoryDocker, "dockerfile", rel, ConfidenceHigh, nil)
	}
	if strings.HasSuffix(name, ".csproj") {
		c.add(CategoryLanguage, "csharp", rel, ConfidenceHigh, nil)
	}
	if strings.HasSuffix(name, ".postman_collection.json") {
		c.add(CategoryTestTool, "postman", rel, ConfidenceHigh, nil)
	}
	if strings.HasSuffix(name, ".graphql") || strings.HasSuffix(name, ".graphqls") {
		c.add(CategoryAPISchema, "graphql", rel, ConfidenceHigh, nil)
	}
	if strings.Contains(rel, ".github/workflows/") && (strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) {
		c.add(CategoryCI, "github_actions", rel, ConfidenceHigh, nil)
	}
}

type packageJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

// nodeFrameworkDeps maps an npm dependency name to the framework/test-tool
// finding it implies.
var nodeFrameworkDeps = map[string][2]string{
	"next":               {CategoryFramework, "next"},
	"react":              {CategoryFramework, "react"},
	"vue":                {CategoryFramework, "vue"},
	"nuxt":               {CategoryFramework, "nuxt"},
	"@angular/core":      {CategoryFramework, "angular"},
	"svelte":             {CategoryFramework, "svelte"},
	"express":            {CategoryFramework, "express"},
	"@nestjs/core":       {CategoryFramework, "nestjs"},
	"@playwright/test":   {CategoryTestTool, "playwright"},
	"cypress":            {CategoryTestTool, "cypress"},
	"selenium-webdriver": {CategoryTestTool, "selenium"},
	"detox":              {CategoryTestTool, "detox"},
	"expo":               {CategoryFramework, "expo"},
	"react-native":       {CategoryFramework, "react-native"},
}

func classifyPackageJSON(c *collector, full, rel string) {
	data, err := os.ReadFile(full)
	if err != nil {
		return
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return
	}

	for depName := range pkg.Dependencies {
		if hit, ok := nodeFrameworkDeps[depName]; ok {
			c.add(hit[0], hit[1], rel, ConfidenceHigh, map[string]any{"dependency": depName})
		}
	}
	for depName := range pkg.DevDependencies {
		if hit, ok := nodeFrameworkDeps[depName]; ok {
			c.add(hit[0], hit[1], rel, ConfidenceHigh, map[string]any{"dependency": depName})
		}
	}
}

// goFrameworkImports maps a go.mod require path to the framework it implies.
var goFrameworkImports = map[string]string{
	"github.com/gin-gonic/gin": "gin",
	"github.com/gofiber/fiber": "fiber",
	"github.com/go-chi/chi":    "chi",
}

func classifyGoMod(c *collector, full, rel string) {
	data, err := os.ReadFile(full)
	if err != nil {
		return
	}
	content := string(data)
	for importPath, name := range goFrameworkImports {
		if strings.Contains(content, importPath) {
			c.add(CategoryFramework, name, rel, ConfidenceHigh, map[string]any{"import": importPath})
		}
	}
}

var pythonFrameworkMarkers = map[string]string{
	"fastapi": "fastapi",
	"django":  "django",
	"flask":   "flask",
}

var javaFrameworkMarkers = map[string]string{
	"spring-boot":          "spring-boot",
	"springframework.boot": "spring-boot",
}

func classifyTextForFrameworks(c *collector, full, rel string, markers map[string]string) {
	data, err := os.ReadFile(full)
	if err != nil {
		return
	}
	content := strings.ToLower(string(data))
	for marker, name := range markers {
		if strings.Contains(content, marker) {
			c.add(CategoryFramework, name, rel, ConfidenceMedium, map[string]any{"matched": marker})
		}
	}
}

// collector deduplicates findings by (category, name), keeping the first
// path seen and accumulating every matching path as evidence.
type collector struct {
	order []string
	byKey map[string]*Finding
}

func newCollector() *collector {
	return &collector{byKey: map[string]*Finding{}}
}

func (c *collector) add(category, name, path, confidence string, extra map[string]any) {
	key := category + "|" + name
	if existing, ok := c.byKey[key]; ok {
		paths, _ := existing.Evidence["paths"].([]string)
		existing.Evidence["paths"] = append(paths, path)
		return
	}

	evidence := map[string]any{"paths": []string{path}}
	for k, v := range extra {
		evidence[k] = v
	}

	c.byKey[key] = &Finding{
		Category:   category,
		Name:       name,
		Path:       path,
		Confidence: confidence,
		Evidence:   evidence,
	}
	c.order = append(c.order, key)
}

func (c *collector) findings() []Finding {
	out := make([]Finding, 0, len(c.order))
	for _, key := range c.order {
		out = append(out, *c.byKey[key])
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Name < out[j].Name
	})
	return out
}
