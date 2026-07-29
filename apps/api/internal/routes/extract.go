package routes

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"e2e-sentinel/apps/api/internal/discovery"
)

// Extract walks root (already validated) and returns a best-effort route
// inventory: Next.js App Router file conventions (high confidence),
// OpenAPI/Swagger path declarations found among findings (high
// confidence), and regex-matched router calls in Express/Go/Python
// source (medium confidence — a regex cannot fully understand the
// language it's scanning).
func Extract(root string, findings []discovery.Finding) ([]Route, error) {
	c := newCollector()

	for _, f := range findings {
		if f.Category != discovery.CategoryAPISchema || (f.Name != "openapi" && f.Name != "swagger") {
			continue
		}
		for _, relPath := range evidencePaths(f) {
			extractOpenAPI(c, filepath.Join(root, relPath), relPath)
		}
	}

	err := discovery.Walk(root, func(rel string, isDir bool) error {
		if isDir {
			return nil
		}
		classifyRouteFile(c, root, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return c.routes(), nil
}

func evidencePaths(f discovery.Finding) []string {
	switch paths := f.Evidence["paths"].(type) {
	case []string:
		return paths
	case []any:
		out := make([]string, 0, len(paths))
		for _, p := range paths {
			if s, ok := p.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// --- Next.js App Router file conventions ---

var nextPageFiles = map[string]bool{"page.tsx": true, "page.ts": true, "page.jsx": true, "page.js": true}
var nextRouteFiles = map[string]bool{"route.ts": true, "route.js": true}
var nextHTTPMethodRe = regexp.MustCompile(`(?m)^export\s+(?:async\s+function|const)\s+(GET|POST|PUT|PATCH|DELETE|OPTIONS|HEAD)\b`)

func classifyRouteFile(c *collector, root, rel string) {
	name := filepath.Base(rel)

	if segments, ok := nextAppSegments(rel); ok {
		if nextPageFiles[name] {
			path := cleanPath("/" + strings.Join(segments, "/"))
			c.add(Route{Path: path, Kind: ClassifyPathKind(path, false), SourcePath: rel, Confidence: ConfidenceHigh,
				Evidence: map[string]any{"convention": "next-app-router-page"}})
			return
		}
		if nextRouteFiles[name] {
			path := cleanPath("/" + strings.Join(segments, "/"))
			methods := extractExportedMethods(filepath.Join(root, rel))
			if len(methods) == 0 {
				methods = []string{""}
			}
			for _, method := range methods {
				c.add(Route{Method: method, Path: path, Kind: ClassifyPathKind(path, method != ""), SourcePath: rel, Confidence: ConfidenceHigh,
					Evidence: map[string]any{"convention": "next-app-router-route-handler"}})
			}
			return
		}
	}

	ext := filepath.Ext(name)
	switch ext {
	case ".js", ".ts", ".jsx", ".tsx", ".mjs", ".cjs":
		extractJSRouterCalls(c, root, rel)
	case ".go":
		extractGoRouterCalls(c, root, rel)
	case ".py":
		extractPythonRouterCalls(c, root, rel)
	}

	// WebSocket URL literals (spec §7.6 "WebSocket endpoints") are
	// scanned for across every source file regardless of language —
	// language-specific client APis (JS `new WebSocket(...)`, Python
	// `websockets.connect(...)`, etc.) all end up embedding a literal
	// "ws://"/"wss://" URL somewhere, so matching the URL literal itself
	// is simpler and more portable than parsing each client API.
	switch ext {
	case ".js", ".ts", ".jsx", ".tsx", ".mjs", ".cjs", ".go", ".py":
		extractWebSocketURLs(c, root, rel)
	}
}

// nextAppSegments returns the URL path segments for a file under a
// Next.js App Router "app" directory (route groups in parens are
// dropped; dynamic segment brackets are kept as-is, e.g. "[id]"), and
// whether rel is under such a directory at all.
func nextAppSegments(rel string) ([]string, bool) {
	parts := strings.Split(rel, "/")
	appIdx := -1
	for i, p := range parts {
		if p == "app" {
			appIdx = i
			break
		}
	}
	if appIdx == -1 {
		return nil, false
	}

	var segments []string
	// Exclude the app/ prefix and the trailing file itself.
	for _, p := range parts[appIdx+1 : len(parts)-1] {
		if strings.HasPrefix(p, "(") && strings.HasSuffix(p, ")") {
			continue // route group, not part of the URL
		}
		segments = append(segments, p)
	}
	return segments, true
}

func extractExportedMethods(fullPath string) []string {
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil
	}
	matches := nextHTTPMethodRe.FindAllStringSubmatch(string(data), -1)
	methods := make([]string, 0, len(matches))
	for _, m := range matches {
		methods = append(methods, m[1])
	}
	return methods
}

func cleanPath(p string) string {
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	if p == "" {
		return "/"
	}
	return p
}

// --- Generic regex-based extraction (medium confidence) ---

var jsRouterRe = regexp.MustCompile(`(?i)\b(?:app|router)\.(get|post|put|patch|delete)\(\s*['"` + "`" + `]([^'"` + "`" + `]+)['"` + "`" + `]`)

func extractJSRouterCalls(c *collector, root, rel string) {
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return
	}
	for _, m := range jsRouterRe.FindAllStringSubmatch(string(data), -1) {
		c.add(Route{Method: strings.ToUpper(m[1]), Path: m[2], Kind: ClassifyPathKind(m[2], true), SourcePath: rel, Confidence: ConfidenceMedium,
			Evidence: map[string]any{"pattern": "express-style router call"}})
	}
}

var goRouterRe = regexp.MustCompile(`\b\w+\.(Get|Post|Put|Patch|Delete|GET|POST|PUT|PATCH|DELETE)\(\s*["` + "`" + `]([^"` + "`" + `]+)["` + "`" + `]`)

func extractGoRouterCalls(c *collector, root, rel string) {
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return
	}
	for _, m := range goRouterRe.FindAllStringSubmatch(string(data), -1) {
		c.add(Route{Method: strings.ToUpper(m[1]), Path: m[2], Kind: ClassifyPathKind(m[2], true), SourcePath: rel, Confidence: ConfidenceMedium,
			Evidence: map[string]any{"pattern": "go router call (chi/gin/fiber-style)"}})
	}
}

var pyRouterRe = regexp.MustCompile(`@\w+\.(get|post|put|patch|delete)\(\s*['"]([^'"]+)['"]`)

func extractPythonRouterCalls(c *collector, root, rel string) {
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return
	}
	for _, m := range pyRouterRe.FindAllStringSubmatch(string(data), -1) {
		c.add(Route{Method: strings.ToUpper(m[1]), Path: m[2], Kind: ClassifyPathKind(m[2], true), SourcePath: rel, Confidence: ConfidenceMedium,
			Evidence: map[string]any{"pattern": "flask/fastapi-style decorator"}})
	}
}

// --- WebSocket URL literals (medium confidence) ---

// wsURLRe matches a "ws://" or "wss://" URL literal, stopping at the
// first quote/backtick/whitespace — deliberately loose (no attempt to
// distinguish a real connection target from a URL embedded in a
// comment or string constant elsewhere) since spec §7.6 only asks for
// an inventory with evidence, not a guarantee of runtime correctness.
var wsURLRe = regexp.MustCompile(`wss?://[^\s'"` + "`" + `)]+`)

func extractWebSocketURLs(c *collector, root, rel string) {
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return
	}
	for _, url := range wsURLRe.FindAllString(string(data), -1) {
		c.add(Route{Path: url, Kind: KindWebSocket, SourcePath: rel, Confidence: ConfidenceMedium,
			Evidence: map[string]any{"pattern": "websocket URL literal"}})
	}
}

// --- OpenAPI/Swagger path declarations (high confidence) ---

type openAPIDoc struct {
	Paths map[string]map[string]any `yaml:"paths"`
}

var httpMethodNames = map[string]bool{
	"get": true, "post": true, "put": true, "patch": true, "delete": true, "options": true, "head": true,
}

func extractOpenAPI(c *collector, fullPath, relPath string) {
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return
	}
	var doc openAPIDoc
	// yaml.v3 parses JSON too (JSON is a YAML subset), so this handles
	// both openapi.yaml and openapi.json without a separate code path.
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return
	}
	for path, methods := range doc.Paths {
		for method := range methods {
			if !httpMethodNames[strings.ToLower(method)] {
				continue
			}
			c.add(Route{Method: strings.ToUpper(method), Path: path, Kind: ClassifyPathKind(path, true), SourcePath: relPath, Confidence: ConfidenceHigh,
				Evidence: map[string]any{"convention": "openapi-declared"}})
		}
	}
}

// --- collector: dedupe by (method, path) ---

type collector struct {
	order []string
	byKey map[string]*Route
}

func newCollector() *collector { return &collector{byKey: map[string]*Route{}} }

func (c *collector) add(r Route) {
	key := r.Method + " " + r.Path
	if existing, ok := c.byKey[key]; ok {
		paths, _ := existing.Evidence["paths"].([]string)
		existing.Evidence["paths"] = append(paths, r.SourcePath)
		// A high-confidence match (e.g. OpenAPI) upgrades a prior
		// medium-confidence regex guess for the same route.
		if r.Confidence == ConfidenceHigh {
			existing.Confidence = ConfidenceHigh
		}
		return
	}

	evidence := map[string]any{"paths": []string{r.SourcePath}}
	for k, v := range r.Evidence {
		evidence[k] = v
	}
	r.Evidence = evidence
	c.byKey[key] = &r
	c.order = append(c.order, key)
}

func (c *collector) routes() []Route {
	out := make([]Route, 0, len(c.order))
	for _, key := range c.order {
		out = append(out, *c.byKey[key])
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}
