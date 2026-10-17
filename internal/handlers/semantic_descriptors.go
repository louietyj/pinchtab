package handlers

import (
	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/semdesc"
	"github.com/pinchtab/semantic"
)

func descriptorFromNode(node bridge.A11yNode) semantic.ElementDescriptor {
	return semdesc.FromNode(node)
}

func semanticDescriptorsFromNodes(nodes []bridge.A11yNode) []semantic.ElementDescriptor {
	return semdesc.Build(nodes)
}
