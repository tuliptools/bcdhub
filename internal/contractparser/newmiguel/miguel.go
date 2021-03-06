package newmiguel

import (
	"fmt"
	"strings"

	"github.com/baking-bad/bcdhub/internal/contractparser/consts"
	"github.com/baking-bad/bcdhub/internal/contractparser/meta"
	"github.com/tidwall/gjson"
)

var decoders = map[string]decoder{
	consts.TypeNamedTuple: &namedTupleDecoder{},
	consts.TypeTuple:      &tupleDecoder{},
	consts.LIST:           &listDecoder{},
	consts.SET:            &listDecoder{},
	consts.MAP:            &mapDecoder{},
	consts.BIGMAP:         &mapDecoder{},
	consts.TypeNamedUnion: &namedUnionDecoder{},
	consts.TypeUnion:      &unionDecoder{},
	consts.OR:             &orDecoder{},
	consts.LAMBDA:         &lambdaDecoder{},
	consts.OPTION:         &optionDecoder{},
	"default":             newLiteralDecoder(),
}

// MichelineToMiguel -
func MichelineToMiguel(data gjson.Result, metadata meta.Metadata) (*Node, error) {
	return michelineNodeToMiguel(data, "0", metadata, true)
}

// BigMapToMiguel -
func BigMapToMiguel(data gjson.Result, binPath string, metadata meta.Metadata) (*Node, error) {
	return michelineNodeToMiguel(data, binPath, metadata, false)
}

// ParameterToMiguel -
func ParameterToMiguel(data gjson.Result, metadata meta.Metadata) (*Node, error) {
	if !data.IsArray() && !data.IsObject() {
		return nil, nil
	}
	node, startPath, err := getStartPath(data, metadata)
	if err != nil {
		return nil, err
	}
	node, startPath = getGJSONParameterPath(node, startPath)
	res, err := michelineNodeToMiguel(node, startPath, metadata, true)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func getStartPath(data gjson.Result, metadata meta.Metadata) (gjson.Result, string, error) {
	var entrypoint, value gjson.Result
	if data.IsArray() {
		entrypoint = data.Get("0.entrypoint")
		value = data.Get("0.value")
	} else if data.IsObject() {
		entrypoint = data.Get("entrypoint")
		value = data.Get("value")
	}

	if entrypoint.Exists() && value.Exists() {
		root := metadata["0"]
		if root.Prim != consts.OR && root.Type != consts.TypeNamedUnion && root.Type != consts.TypeNamedTuple {
			return value, "0", nil
		}
		for path, md := range metadata {
			if md.FieldName == entrypoint.String() {
				return value, path, nil
			}
		}
		return value, "0", nil
	}
	return data, "0", nil
}

func michelineNodeToMiguel(data gjson.Result, path string, metadata meta.Metadata, isRoot bool) (node *Node, err error) {
	nm, ok := metadata[path]
	if !ok {
		return nil, fmt.Errorf("Unknown metadata path: %s", path)
	}

	if dec, ok := decoders[nm.Type]; ok {
		node, err = dec.Decode(data, path, nm, metadata, isRoot)

	} else {
		node, err = decoders["default"].Decode(data, path, nm, metadata, isRoot)
	}
	if err != nil {
		return
	}
	if strings.HasSuffix(path, "/o") {
		node.IsOption = true
	}
	return
}

// GetGJSONPath -
func GetGJSONPath(path string) string {
	parts := strings.Split(path, "/")
	res := buildPathFromArray(parts)
	return strings.TrimSuffix(res, ".")
}

func buildPathFromArray(parts []string) (res string) {
	if len(parts) == 0 {
		return
	}

	for _, part := range parts {
		switch part {
		case "l", "s":
			res += "args.#."
		case "k":
			res += "#.args.0."
		case "v":
			res += "#.args.1."
		case "o":
			res += "args.0."
		default:
			res += fmt.Sprintf("args.%s.", part)
		}
	}
	return
}

func getGJSONPathUnion(path string, node gjson.Result) (res string, err error) {
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		idx := len(parts)
		for i, part := range parts {
			switch part {
			case "0":
				if node.IsObject() {
					if node.Get(res+"prim").String() != "Left" {
						return "", fmt.Errorf("Invalid path")
					}
					res += "args.0."
				} else {
					res += "#(prim==\"Left\").args.0."
				}
			case "1":
				if node.IsObject() {
					if node.Get(res+"prim").String() != "Right" {
						return "", fmt.Errorf("Invalid path")
					}
					res += "args.0."
				} else {
					res += "#(prim==\"Right\").args.0."
				}
			case "o":
				if node.Get(res+"prim").String() != consts.None {
					res += "args.0."
				}
			default:
				idx = i + 1
				goto Break
			}
		}
	Break:
		res += buildPathFromArray(parts[idx:])
	}
	res = strings.TrimSuffix(res, ".")
	return
}

func getGJSONParameterPath(node gjson.Result, startPath string) (gjson.Result, string) {
	path := startPath
	prim := node.Get("prim").String()
	if prim == "Right" {
		path += "/1"
		right := node.Get("args.0")
		return getGJSONParameterPath(right, path)
	}
	if prim == "Left" {
		path += "/0"
		left := node.Get("args.0")
		return getGJSONParameterPath(left, path)
	}
	return node, path
}
