package config

import (
	"errors"
	"fmt"
	"strings"
)

// PlatformTool configures the operator command line tool.
type PlatformTool struct {
	Database   Database
	BaseDomain string
	AdminURL   string
	ConsoleURL string
}

// LoadPlatformTool reads the configuration of the operator tool, which connects
// with the role allowed to manage tenants.
func LoadPlatformTool(lookup Lookup) (PlatformTool, error) {
	r := reader{lookup: lookup}
	cfg := PlatformTool{
		Database:   r.database("PLATFORM_DB_USER", "PLATFORM_DB_PASSWORD"),
		BaseDomain: strings.ToLower(r.required("PLATFORM_BASE_DOMAIN")),
		AdminURL:   r.absoluteURL("ADMIN_PUBLIC_URL"),
		ConsoleURL: r.absoluteURL("CONSOLE_PUBLIC_URL"),
	}
	if err := errors.Join(r.errs...); err != nil {
		return PlatformTool{}, fmt.Errorf("invalid platform tool configuration: %w", err)
	}
	return cfg, nil
}
