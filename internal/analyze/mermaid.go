package analyze

import (
	"fmt"
	"sort"
	"strings"
)

// Mermaid uses generated node IDs and entity-escaped labels, never source text
// as Mermaid syntax. Sorting and deduplication keep output stable, even in cycles.
func Mermaid(edges []Edge) string {
	return MermaidNodes(edges, nil)
}

// MermaidNodes includes standalone nodes as well as the endpoints of edges.
func MermaidNodes(edges []Edge, nodes []string) string {
	labels := map[string]bool{}
	for _, node := range nodes {
		labels[node] = true
	}
	for _, edge := range edges {
		labels[edge.From] = true
		labels[edge.To] = true
	}
	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	sort.Strings(names)
	ids := map[string]string{}
	var out strings.Builder
	out.WriteString("flowchart TD\n")
	for index, name := range names {
		id := fmt.Sprintf("n%d", index)
		ids[name] = id
		fmt.Fprintf(&out, "  %s[\"%s\"]\n", id, mermaidLabel(name))
	}
	sorted := append([]Edge(nil), edges...)
	sortEdges(sorted)
	for index, edge := range sorted {
		if index > 0 && edge == sorted[index-1] {
			continue
		}
		arrow := "-->"
		if edge.Unknown {
			arrow = "-.->"
		}
		fmt.Fprintf(&out, "  %s %s %s\n", ids[edge.From], arrow, ids[edge.To])
	}
	return out.String()
}

func mermaidLabel(label string) string {
	var out strings.Builder
	for _, r := range label {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == ' ' {
			out.WriteRune(r)
		} else {
			fmt.Fprintf(&out, "#%d;", r)
		}
	}
	return out.String()
}
