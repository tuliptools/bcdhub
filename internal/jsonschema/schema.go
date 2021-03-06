package jsonschema

import (
	"fmt"
	"strings"

	"github.com/baking-bad/bcdhub/internal/contractparser/consts"
	"github.com/baking-bad/bcdhub/internal/contractparser/meta"
)

var fabric = map[string]maker{
	"default":   &defaultMaker{},
	consts.PAIR: &pairMaker{},
	consts.MAP:  &mapMaker{},
	consts.LIST: &listMaker{},
	consts.SET:  &listMaker{},
	consts.OR:   &orMaker{},
}

// Create - creates json schema for entrypoint
func Create(binPath string, metadata meta.Metadata) (Schema, DefaultModel, error) {
	nm, ok := metadata[binPath]
	if !ok {
		return nil, nil, fmt.Errorf("[Create] Unknown metadata binPath: %s", binPath)
	}

	if nm.Prim == consts.UNIT {
		return nil, DefaultModel{}, nil
	}

	f, ok := fabric[nm.Prim]
	if !ok {
		f = fabric["default"]
	}

	schema, model, err := f.Do(binPath, metadata)
	if err != nil {
		return nil, nil, err
	}

	if strings.HasSuffix(binPath, "/o") {
		return optionWrapper(schema, binPath, metadata)
	}
	return schema, model, nil
}
