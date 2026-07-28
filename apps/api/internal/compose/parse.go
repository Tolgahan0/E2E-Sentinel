// Package compose parses Docker Compose files into a normalized,
// read-only structure. It never executes `docker compose` or any other
// subprocess — parsing the YAML directly avoids the command-injection
// surface a shell-out would introduce (spec §23.3) and works even when
// the `docker compose` CLI isn't installed (e.g. inside the distroless
// sentinel-api image).
//
// Only structure is extracted. Environment variable *names* are kept;
// values are discarded before they ever leave this package, since a
// value may be a literal secret written directly into the compose file
// (spec §7.4 "Do not display secret values").
package compose

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Service is one normalized service declaration from a compose file.
type Service struct {
	Name        string
	Image       string
	HasBuild    bool
	Ports       []string
	EnvVarNames []string
	DependsOn   []string
	Profiles    []string
	Command     string
	Entrypoint  string
	Networks    []string
	Volumes     []string
}

type rawFile struct {
	Services map[string]rawService `yaml:"services"`
}

type rawService struct {
	Image       string          `yaml:"image"`
	Build       yaml.Node       `yaml:"build"`
	Ports       stringOrIntList `yaml:"ports"`
	Environment envList         `yaml:"environment"`
	DependsOn   stringOrMapKeys `yaml:"depends_on"`
	Profiles    stringOrIntList `yaml:"profiles"`
	Command     stringOrList    `yaml:"command"`
	Entrypoint  stringOrList    `yaml:"entrypoint"`
	Networks    stringOrIntList `yaml:"networks"`
	Volumes     stringOrIntList `yaml:"volumes"`
}

// ParseFile reads and normalizes a compose file at path. It never
// returns an error for content it doesn't understand — unknown or
// malformed sections are simply omitted, since discovery must degrade
// gracefully rather than fail the whole scan over one file (spec §2.2).
func ParseFile(path string) ([]Service, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("compose: reading %q: %w", path, err)
	}

	var file rawFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("compose: parsing %q: %w", path, err)
	}

	services := make([]Service, 0, len(file.Services))
	for name, raw := range file.Services {
		services = append(services, Service{
			Name:        name,
			Image:       raw.Image,
			HasBuild:    raw.Build.Kind != 0,
			Ports:       raw.Ports.values,
			EnvVarNames: raw.Environment.names,
			DependsOn:   raw.DependsOn.values,
			Profiles:    raw.Profiles.values,
			Command:     raw.Command.joined(),
			Entrypoint:  raw.Entrypoint.joined(),
			Networks:    raw.Networks.values,
			Volumes:     raw.Volumes.values,
		})
	}
	return services, nil
}
