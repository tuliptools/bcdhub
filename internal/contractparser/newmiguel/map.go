package newmiguel

import (
	"fmt"

	"github.com/baking-bad/bcdhub/internal/contractparser/consts"
	"github.com/baking-bad/bcdhub/internal/contractparser/formatter"
	"github.com/baking-bad/bcdhub/internal/contractparser/meta"
	"github.com/tidwall/gjson"
)

type mapDecoder struct{}

// Decode -
func (l *mapDecoder) Decode(data gjson.Result, path string, nm *meta.NodeMetadata, metadata meta.Metadata, isRoot bool) (*Node, error) {
	if data.Get("int").Exists() {
		return &Node{
			Prim:  consts.BIGMAP,
			Type:  consts.BIGMAP,
			Value: data.Get("int").Int(),
		}, nil
	}

	if data.IsArray() && len(data.Array()) == 0 && path == "0/0" {
		return &Node{
			Prim:  consts.BIGMAP,
			Type:  consts.BIGMAP,
			Value: 0,
		}, nil
	}

	node := Node{
		Prim:     nm.Prim,
		Type:     nm.Type,
		Children: make([]*Node, 0),
	}
	if data.Value() == nil {
		return &node, nil
	}
	gjsonPath := GetGJSONPath("k")
	keyJSON := data.Get(gjsonPath)

	for i, k := range keyJSON.Array() {
		key, err := michelineNodeToMiguel(k, path+"/k", metadata, false)
		if err != nil {
			return nil, err
		}
		if key != nil {
			gjsonPath := fmt.Sprintf("%d.args.1", i)
			valJSON := data.Get(gjsonPath)
			var argNode *Node
			if valJSON.Exists() {
				argNode, err = michelineNodeToMiguel(valJSON, path+"/v", metadata, false)
				if err != nil {
					return nil, err
				}
			}

			if key.Value == nil && len(key.Children) > 0 {
				key.Value, err = formatter.MichelineToMichelson(keyJSON, true, formatter.DefLineSize)
				if err != nil {
					return nil, err
				}
			}
			s, err := l.getKey(key)
			if err != nil {
				return nil, err
			}
			argNode.Name = s
			node.Children = append(node.Children, argNode)
		}
	}

	return &node, nil
}

func (l *mapDecoder) getKey(key *Node) (s string, err error) {
	switch kv := key.Value.(type) {
	case string:
		s = kv
	case int, int64:
		s = fmt.Sprintf("%d", kv)
	case map[string]interface{}:
		s = fmt.Sprintf("%v", kv["miguel_value"])
	case []interface{}:
		s = ""
		for i, item := range kv {
			val := item.(map[string]interface{})
			if i != 0 {
				s += "@"
			}
			s += fmt.Sprintf("%v", val["miguel_value"])
		}
	default:
		err = fmt.Errorf("Invalid map key type: %v %T", key, key)
	}
	return
}
