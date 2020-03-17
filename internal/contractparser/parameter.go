package contractparser

import (
	"fmt"

	"github.com/baking-bad/bcdhub/internal/contractparser/consts"
	"github.com/baking-bad/bcdhub/internal/contractparser/meta"
	"github.com/baking-bad/bcdhub/internal/contractparser/node"
	"github.com/baking-bad/bcdhub/internal/helpers"
	"github.com/tidwall/gjson"
)

// Entrypoint -
type Entrypoint struct {
	Name      string       `json:"name"`
	Prim      string       `json:"prim"`
	Args      []Entrypoint `json:"args,omitempty"`
	Parameter interface{}  `json:"parameter"`
}

// Parameter -
type Parameter struct {
	*parser

	Metadata meta.Metadata

	Tags helpers.Set
}

func newParameter(v gjson.Result) (Parameter, error) {
	if !v.IsArray() {
		return Parameter{}, fmt.Errorf("Parameter is not array")
	}
	p := Parameter{
		parser: &parser{},
		Tags:   make(helpers.Set),
	}
	p.primHandler = p.handlePrimitive
	if err := p.parse(v); err != nil {
		return p, err
	}

	m, err := meta.ParseMetadata(v)
	if err != nil {
		return p, err
	}
	p.Metadata = m

	tags, err := endpointsTags(m)
	if err != nil {
		return p, err
	}
	p.Tags.Append(tags...)

	return p, err
}

func (p *Parameter) handlePrimitive(n node.Node) error {
	if n.Is(consts.CONTRACT) {
		p.Tags.Append(consts.ViewMethodTag)
	}
	return nil
}
