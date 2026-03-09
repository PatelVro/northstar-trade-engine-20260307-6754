package news

import (
	"fmt"
	"strings"
)

// NewProvider constructs a news provider by name.
func NewProvider(name string) (Provider, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" || normalized == "rss" || normalized == "yahoo_rss" {
		return NewRSSProvider(), nil
	}
	return nil, fmt.Errorf("unsupported news provider: %s", name)
}
