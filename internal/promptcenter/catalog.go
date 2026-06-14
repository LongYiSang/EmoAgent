package promptcenter

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Catalog struct {
	components map[string]PromptComponent
	order      []string
}

type componentDefinition struct {
	ID           string   `yaml:"id"`
	Group        string   `yaml:"group"`
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	Kind         string   `yaml:"kind"`
	Editable     bool     `yaml:"editable"`
	RiskLevel    string   `yaml:"risk_level"`
	ScopeSupport []string `yaml:"scope_support"`
	MaxChars     int      `yaml:"max_chars"`
	Order        int      `yaml:"order"`
	DefaultFile  string   `yaml:"default_file"`
}

func DefaultCatalog() (*Catalog, error) {
	data, err := defaultsFS.ReadFile("defaults/components.yaml")
	if err != nil {
		return nil, fmt.Errorf("read components manifest: %w", err)
	}
	var defs []componentDefinition
	if err := yaml.Unmarshal(data, &defs); err != nil {
		return nil, fmt.Errorf("decode components manifest: %w", err)
	}
	components := make([]PromptComponent, 0, len(defs))
	for _, def := range defs {
		component, err := componentFromDefinition(def)
		if err != nil {
			return nil, err
		}
		components = append(components, component)
	}
	return NewCatalog(components)
}

func NewCatalog(components []PromptComponent) (*Catalog, error) {
	catalog := &Catalog{
		components: make(map[string]PromptComponent, len(components)),
		order:      make([]string, 0, len(components)),
	}
	for _, component := range components {
		if strings.TrimSpace(component.ID) == "" {
			return nil, fmt.Errorf("component id is required")
		}
		if _, exists := catalog.components[component.ID]; exists {
			return nil, fmt.Errorf("duplicate component id %q", component.ID)
		}
		if strings.TrimSpace(component.DefaultText) == "" {
			return nil, fmt.Errorf("component %s default text is required", component.ID)
		}
		component.DefaultHash = HashText(component.DefaultText)
		catalog.components[component.ID] = component
		catalog.order = append(catalog.order, component.ID)
	}
	sort.Slice(catalog.order, func(i, j int) bool {
		left := catalog.components[catalog.order[i]]
		right := catalog.components[catalog.order[j]]
		if left.Order == right.Order {
			return left.ID < right.ID
		}
		return left.Order < right.Order
	})
	return catalog, nil
}

func componentFromDefinition(def componentDefinition) (PromptComponent, error) {
	if strings.TrimSpace(def.DefaultFile) == "" {
		return PromptComponent{}, fmt.Errorf("component %s default_file is required", def.ID)
	}
	data, err := defaultsFS.ReadFile("defaults/" + def.DefaultFile)
	if err != nil {
		return PromptComponent{}, fmt.Errorf("read default %s: %w", def.DefaultFile, err)
	}
	text := strings.TrimSpace(string(data))
	return PromptComponent{
		ID:           def.ID,
		Group:        def.Group,
		Name:         def.Name,
		Description:  def.Description,
		Kind:         def.Kind,
		DefaultText:  text,
		Editable:     def.Editable,
		RiskLevel:    def.RiskLevel,
		ScopeSupport: append([]string(nil), def.ScopeSupport...),
		MaxChars:     def.MaxChars,
		Order:        def.Order,
	}, nil
}

func (c *Catalog) Get(id string) (PromptComponent, bool) {
	if c == nil {
		return PromptComponent{}, false
	}
	component, ok := c.components[id]
	return component, ok
}

func (c *Catalog) MustGet(id string) PromptComponent {
	component, ok := c.Get(id)
	if !ok {
		panic("prompt component not found: " + id)
	}
	return component
}

func (c *Catalog) List() []PromptComponent {
	if c == nil {
		return nil
	}
	items := make([]PromptComponent, 0, len(c.order))
	for _, id := range c.order {
		items = append(items, c.components[id])
	}
	return items
}
