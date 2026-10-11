package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"crewstudio/internal/config"
	"crewstudio/internal/model"
)

// Catalog reads the tool list the Python runner implements and works out which tools have the
// environment variables they need. It re-reads the provider .env on every call so adding a key
// takes effect without a restart. Only key presence is checked; values are never read out.
//
// Every entry must say what the tool does outside the run (its effect) and what it needs: agent chat gives an agent
// only tools that read (docs/agent-chat-handoffs-design.md, section 7). An entry without a known effect is an error,
// and the server checks the catalog when it starts, so an unclassified tool stops it rather than reaching chat.
type Catalog struct {
	catalogPath     string
	providerEnvFile string
	getenv          func(string) string
}

func NewCatalog(catalogPath, providerEnvFile string, getenv func(string) string) *Catalog {
	return &Catalog{catalogPath: catalogPath, providerEnvFile: providerEnvFile, getenv: getenv}
}

// CheckCatalog reads the catalog at path and checks that it classifies every tool (model.CheckToolEffect). The server
// runs it at startup; unlike Tools it does not read the provider env file, so only the classification can stop a start.
func CheckCatalog(path string) error {
	_, err := readCatalog(path)
	return err
}

// readCatalog parses the catalog and checks every entry's effect and needs.
func readCatalog(path string) ([]model.Tool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tool catalog: %w", err)
	}
	var tools []model.Tool
	if err := json.Unmarshal(data, &tools); err != nil {
		return nil, fmt.Errorf("parse tool catalog: %w", err)
	}
	for _, tool := range tools {
		if err := model.CheckToolEffect(tool); err != nil {
			return nil, fmt.Errorf("tool catalog %s: %w", path, err)
		}
	}
	return tools, nil
}

func (c *Catalog) Tools() ([]model.Tool, error) {
	tools, err := readCatalog(c.catalogPath)
	if err != nil {
		return nil, err
	}

	provider, err := config.ReadDotEnv(c.providerEnvFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read provider env file: %w", err)
	}
	out := make([]model.Tool, 0, len(tools))
	for _, tool := range tools {
		available := true
		for _, key := range tool.RequiresEnv {
			if provider[key] == "" && c.getenv(key) == "" {
				available = false
			}
		}
		tool.Available = available
		if tool.RequiresEnv == nil {
			tool.RequiresEnv = []string{}
		}
		out = append(out, tool)
	}
	return out, nil
}

// ByKey returns the catalog keyed by tool key.
func (c *Catalog) ByKey() (map[string]model.Tool, error) {
	tools, err := c.Tools()
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]model.Tool, len(tools))
	for _, t := range tools {
		byKey[t.Key] = t
	}
	return byKey, nil
}

// EnvSet reports which of the variables are set, in the provider env file or the server's environment. Values are
// never read out, only whether they are empty.
func (c *Catalog) EnvSet(keys ...string) (map[string]bool, error) {
	provider, err := config.ReadDotEnv(c.providerEnvFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read provider env file: %w", err)
	}
	out := make(map[string]bool, len(keys))
	for _, key := range keys {
		out[key] = provider[key] != "" || c.getenv(key) != ""
	}
	return out, nil
}
