package gitops

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// ResourceLimitsPatcher implements PatchGenerator for the
// require-resource-limits violation class. This is deliberately
// rule-based, not model-generated: the correct fix for "missing
// resources block" has exactly one shape, adding conservative defaults,
// so there is nothing here that benefits from an LLM call. See
// docs/ARCHITECTURE.md for the reasoning behind this design choice.
type ResourceLimitsPatcher struct {
	// DefaultCPURequest etc. allow the caller to override the
	// conservative defaults below, useful for namespaces with known
	// different workload profiles. Zero values fall back to the
	// package defaults.
	DefaultCPURequest    string
	DefaultMemoryRequest string
	DefaultCPULimit      string
	DefaultMemoryLimit   string
}

func (p ResourceLimitsPatcher) defaults() (cpuReq, memReq, cpuLim, memLim string) {
	cpuReq, memReq, cpuLim, memLim = "50m", "32Mi", "100m", "64Mi"
	if p.DefaultCPURequest != "" {
		cpuReq = p.DefaultCPURequest
	}
	if p.DefaultMemoryRequest != "" {
		memReq = p.DefaultMemoryRequest
	}
	if p.DefaultCPULimit != "" {
		cpuLim = p.DefaultCPULimit
	}
	if p.DefaultMemoryLimit != "" {
		memLim = p.DefaultMemoryLimit
	}
	return
}

// Generate parses oldContent as a Kubernetes Pod manifest and returns
// new content with a resources block added to every container that is
// missing one. Containers that already declare partial resources
// (e.g. requests but no limits) are left untouched, this patcher only
// fills a completely absent resources block, it does not adjust
// existing values, adjusting existing values is a judgment call outside
// this violation class's deterministic scope.
func (p ResourceLimitsPatcher) Generate(oldContent string) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(oldContent), &doc); err != nil {
		return "", fmt.Errorf("gitops: failed to parse manifest as YAML: %w", err)
	}

	if len(doc.Content) == 0 {
		return "", fmt.Errorf("gitops: empty YAML document")
	}

	root := doc.Content[0]
	containers, err := findContainersNode(root)
	if err != nil {
		return "", err
	}

	cpuReq, memReq, cpuLim, memLim := p.defaults()
	patched := false

	for _, container := range containers.Content {
		if hasResourcesBlock(container) {
			continue
		}
		addResourcesBlock(container, cpuReq, memReq, cpuLim, memLim)
		patched = true
	}

	if !patched {
		return "", fmt.Errorf("gitops: no containers found missing a resources block, nothing to patch")
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return "", fmt.Errorf("gitops: failed to marshal patched manifest: %w", err)
	}

	return string(out), nil
}

// findContainersNode walks spec.containers (Pod) or
// spec.template.spec.containers (Deployment/other pod-template
// workloads) and returns the sequence node for the containers list.
func findContainersNode(root *yaml.Node) (*yaml.Node, error) {
	spec := mapGet(root, "spec")
	if spec == nil {
		return nil, fmt.Errorf("gitops: manifest has no top-level spec field")
	}

	if containers := mapGet(spec, "containers"); containers != nil {
		return containers, nil
	}

	if template := mapGet(spec, "template"); template != nil {
		if templateSpec := mapGet(template, "spec"); templateSpec != nil {
			if containers := mapGet(templateSpec, "containers"); containers != nil {
				return containers, nil
			}
		}
	}

	return nil, fmt.Errorf("gitops: could not locate spec.containers or spec.template.spec.containers")
}

// mapGet finds the value node for a given key in a YAML mapping node.
// Returns nil if not found or if node isn't a mapping.
func mapGet(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func hasResourcesBlock(container *yaml.Node) bool {
	return mapGet(container, "resources") != nil
}

func addResourcesBlock(container *yaml.Node, cpuReq, memReq, cpuLim, memLim string) {
	resourcesKey := &yaml.Node{Kind: yaml.ScalarNode, Value: "resources"}
	resourcesVal := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "requests"},
			{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "cpu"}, {Kind: yaml.ScalarNode, Value: cpuReq},
					{Kind: yaml.ScalarNode, Value: "memory"}, {Kind: yaml.ScalarNode, Value: memReq},
				},
			},
			{Kind: yaml.ScalarNode, Value: "limits"},
			{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "cpu"}, {Kind: yaml.ScalarNode, Value: cpuLim},
					{Kind: yaml.ScalarNode, Value: "memory"}, {Kind: yaml.ScalarNode, Value: memLim},
				},
			},
		},
	}
	container.Content = append(container.Content, resourcesKey, resourcesVal)
}
