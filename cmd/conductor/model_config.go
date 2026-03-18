package main

import (
	"fmt"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/llm"
)

var pipelineModelRoles = []string{
	"decompose",
	"analyze",
	"crosscut",
	"synthesize",
	"describe",
	"verify_analyze",
	"verify_synthesize",
}

func buildClientsForRoles(cfg *config.ProjectConfig, requiredRoles []string) (map[string]llm.Client, error) {
	if err := config.ValidateModelRouting(cfg, requiredRoles); err != nil {
		return nil, err
	}

	requiredProviders := make(map[string]struct{}, len(requiredRoles))
	for _, role := range requiredRoles {
		requiredProviders[cfg.Models.Roles[role]] = struct{}{}
	}

	clients := make(map[string]llm.Client, len(requiredProviders))
	for providerName := range requiredProviders {
		client, err := llm.NewClientFromProvider(cfg.Models.Providers[providerName], cfg.Project.Path)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", providerName, err)
		}
		clients[providerName] = client
	}

	return clients, nil
}

func buildRoleResolver(cfg *config.ProjectConfig, requiredRoles []string) (*llm.RoleResolver, error) {
	clients, err := buildClientsForRoles(cfg, requiredRoles)
	if err != nil {
		return nil, err
	}
	return llm.NewRoleResolver(clients, cfg.Models.Roles), nil
}

func buildClientForRole(cfg *config.ProjectConfig, role string) (llm.Client, error) {
	clients, err := buildClientsForRoles(cfg, []string{role})
	if err != nil {
		return nil, err
	}
	return clients[cfg.Models.Roles[role]], nil
}
