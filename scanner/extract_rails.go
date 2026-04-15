package scanner

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AgusRdz/probe/config"
	"github.com/AgusRdz/probe/observer"
)

// railsExtractor implements Extractor for Ruby on Rails applications.
type railsExtractor struct{}

func (e *railsExtractor) Name() string { return "rails" }

// Detect returns true if Gemfile in dir mentions rails.
func (e *railsExtractor) Detect(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "Gemfile"))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "rails")
}

// resources :users
var reRailsResources = regexp.MustCompile(`^\s*resources\s+:(\w+)(?:\s*,\s*only:\s*\[([^\]]+)\])?`)

// resource :profile (singular)
var reRailsResource = regexp.MustCompile(`^\s*resource\s+:(\w+)`)

// namespace :api do
var reRailsNamespace = regexp.MustCompile(`^\s*namespace\s+:(\w+)\s+do`)

// scope '/v1' do
var reRailsScope = regexp.MustCompile(`^\s*scope\s+['"]([^'"]+)['"]\s+do`)

// get '/search', to: 'ctrl#act'
var reRailsVerb = regexp.MustCompile(`^\s*(get|post|put|patch|delete)\s+['"]([^'"]+)['"]`)

// end
var reRailsEnd = regexp.MustCompile(`^\s*end\s*$`)

// member { get :activate } — single-line
var reRailsMember = regexp.MustCompile(`member\s+do|member\s*\{`)
var reRailsCollection = regexp.MustCompile(`collection\s+do|collection\s*\{`)
var reRailsMemberVerb = regexp.MustCompile(`(get|post|put|patch|delete)\s+:(\w+)`)

// :id → {id}
var reRailsPathParam = regexp.MustCompile(`:(\w+)`)

// params.require(:user).permit(:name, :email)
var reRailsPermit = regexp.MustCompile(`permit\(([^)]+)\)`)

// Extract walks the project config/routes.rb and related controller files.
func (e *railsExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	routesPath := filepath.Join(dir, "config", "routes.rb")
	if _, err := os.Stat(routesPath); err != nil {
		// Try nested.
		_ = walkWithSkip(dir, map[string]bool{".git": true}, "routes.rb", func(p string) error {
			routesPath = p
			return fmt.Errorf("found") // stop walking
		})
	}

	// Collect controller permit schemas.
	schemas := make(map[string]*observer.Schema)
	controllersDir := filepath.Join(dir, "app", "controllers")
	_ = walkWithSkip(controllersDir, map[string]bool{}, ".rb", func(path string) error {
		found, ferr := extractRailsControllerSchema(path)
		if ferr != nil {
			return nil
		}
		for k, v := range found {
			schemas[k] = v
		}
		return nil
	})

	endpoints, err := extractRailsRoutesFile(routesPath, schemas)
	if err != nil {
		fmt.Fprintf(errorWriter, "scanner/rails: error reading %s: %v\n", routesPath, err)
		return nil, nil
	}
	return endpoints, nil
}

// extractRailsRoutesFile parses config/routes.rb.
func extractRailsRoutesFile(path string, schemas map[string]*observer.Schema) ([]ScannedEndpoint, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	absPath, _ := filepath.Abs(path)

	// Simple prefix stack.
	prefixStack := []string{""}
	var endpoints []ScannedEndpoint
	inMember := false
	inCollection := false
	lastResource := ""

	push := func(seg string) {
		top := prefixStack[len(prefixStack)-1]
		prefixStack = append(prefixStack, top+"/"+strings.TrimLeft(seg, "/"))
	}
	pop := func() {
		if len(prefixStack) > 1 {
			prefixStack = prefixStack[:len(prefixStack)-1]
		}
	}
	top := func() string { return prefixStack[len(prefixStack)-1] }

	add := func(method, path string) {
		ep := ScannedEndpoint{
			Method:      strings.ToUpper(method),
			PathPattern: normalizeRailsPath(path),
			Protocol:    "rest",
			Framework:   "rails",
			SourceFile:  absPath,
		}
		endpoints = append(endpoints, ep)
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if reRailsEnd.MatchString(trimmed) {
			if inMember || inCollection {
				inMember = false
				inCollection = false
			} else {
				pop()
			}
			continue
		}

		if reRailsMember.MatchString(trimmed) {
			inMember = true
		}
		if reRailsCollection.MatchString(trimmed) {
			inCollection = true
		}

		// Member/collection verbs inside blocks.
		if (inMember || inCollection) && lastResource != "" {
			if m := reRailsMemberVerb.FindStringSubmatch(trimmed); m != nil {
				method := m[1]
				action := m[2]
				if inMember {
					add(method, top()+"/"+lastResource+"/{id}/"+action)
				} else {
					add(method, top()+"/"+lastResource+"/"+action)
				}
				continue
			}
		}

		if m := reRailsNamespace.FindStringSubmatch(line); m != nil {
			push(m[1])
			continue
		}
		if m := reRailsScope.FindStringSubmatch(line); m != nil {
			push(m[1])
			continue
		}

		// resources :users
		if m := reRailsResources.FindStringSubmatch(line); m != nil {
			name := m[1]
			lastResource = name
			base := top() + "/" + name
			onlyStr := m[2]
			allowed := railsResourceMethods(onlyStr)
			for _, am := range allowed {
				if am.hasID {
					add(am.method, base+"/{id}")
				} else {
					add(am.method, base)
				}
			}
			continue
		}

		// resource :profile (singular)
		if m := reRailsResource.FindStringSubmatch(line); m != nil {
			name := m[1]
			base := top() + "/" + name
			for _, am := range railsSingularResourceMethods() {
				if am.hasID {
					add(am.method, base)
				} else {
					add(am.method, base)
				}
			}
			continue
		}

		// Single verb route: get '/search', to: ...
		if m := reRailsVerb.FindStringSubmatch(line); m != nil {
			method := m[1]
			rawPath := m[2]
			add(method, top()+"/"+strings.TrimLeft(rawPath, "/"))
			continue
		}
	}

	return endpoints, nil
}

type railsAction struct {
	method string
	hasID  bool
}

// railsResourceMethods returns the CRUD actions for resources, filtered by only: [...].
func railsResourceMethods(onlyStr string) []railsAction {
	all := []railsAction{
		{"GET", false},    // index
		{"POST", false},   // create
		{"GET", true},     // show
		{"PUT", true},     // update
		{"PATCH", true},   // update
		{"DELETE", true},  // destroy
	}
	if onlyStr == "" {
		return all
	}
	actionNames := map[string]bool{}
	for _, s := range strings.Split(onlyStr, ",") {
		name := strings.Trim(strings.TrimSpace(s), `:"'`)
		actionNames[name] = true
	}
	actionMap := map[string]railsAction{
		"index":   {"GET", false},
		"create":  {"POST", false},
		"show":    {"GET", true},
		"update":  {"PUT", true},
		"destroy": {"DELETE", true},
	}
	var result []railsAction
	for _, name := range []string{"index", "create", "show", "update", "destroy"} {
		if actionNames[name] {
			result = append(result, actionMap[name])
		}
	}
	return result
}

// railsSingularResourceMethods returns actions for singular resource (no :id).
func railsSingularResourceMethods() []railsAction {
	return []railsAction{
		{"GET", false},
		{"POST", false},
		{"PUT", false},
		{"PATCH", false},
		{"DELETE", false},
	}
}

// normalizeRailsPath converts :param to {param} and cleans double slashes.
func normalizeRailsPath(path string) string {
	path = reRailsPathParam.ReplaceAllString(path, `{$1}`)
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

// extractRailsControllerSchema reads params.require/permit from a controller file.
func extractRailsControllerSchema(path string) (map[string]*observer.Schema, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string]*observer.Schema)
	// Derive resource name from file name (users_controller.rb → users).
	base := filepath.Base(path)
	resourceName := strings.TrimSuffix(base, "_controller.rb")

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if m := reRailsPermit.FindStringSubmatch(line); m != nil {
			schema := &observer.Schema{
				Type:       "object",
				Properties: make(map[string]*observer.Schema),
			}
			for _, field := range strings.Split(m[1], ",") {
				name := strings.Trim(strings.TrimSpace(field), `:"'`)
				if name != "" {
					schema.Properties[name] = &observer.Schema{Type: "string"}
					schema.Required = append(schema.Required, name)
				}
			}
			result[resourceName] = schema
		}
	}
	return result, sc.Err()
}
