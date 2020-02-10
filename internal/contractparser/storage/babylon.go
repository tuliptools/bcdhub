package storage

import (
	"fmt"
	"strings"

	"github.com/aopoltorzhicky/bcdhub/internal/contractparser/consts"
	"github.com/aopoltorzhicky/bcdhub/internal/contractparser/meta"
	"github.com/aopoltorzhicky/bcdhub/internal/contractparser/miguel"
	"github.com/aopoltorzhicky/bcdhub/internal/elastic"
	"github.com/aopoltorzhicky/bcdhub/internal/models"
	"github.com/aopoltorzhicky/bcdhub/internal/noderpc"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Babylon -
type Babylon struct {
	es  *elastic.Elastic
	rpc *noderpc.NodeRPC
}

// NewBabylon -
func NewBabylon(es *elastic.Elastic, rpc *noderpc.NodeRPC) Babylon {
	return Babylon{
		es:  es,
		rpc: rpc,
	}
}

// ParseTransaction -
func (b Babylon) ParseTransaction(content gjson.Result, protocol string, level int64, operationID string) (RichStorage, error) {
	address := content.Get("destination").String()
	data, err := b.rpc.GetScriptJSON(address, level)
	if err != nil {
		return RichStorage{Empty: true}, err
	}

	m, err := meta.GetMetadata(b.es, address, consts.Babylon, "storage", protocol)
	if err != nil {
		return RichStorage{Empty: true}, err
	}

	result, err := getResult(content)
	if err != nil {
		return RichStorage{Empty: true}, err
	}

	ptrMap, err := b.binPathToPtrMap(m, data.Get("storage"))
	if err != nil {
		return RichStorage{Empty: true}, err
	}

	bm, err := b.getBigMapDiff(result, ptrMap, operationID, address, level, m)
	if err != nil {
		return RichStorage{Empty: true}, err
	}
	return RichStorage{
		BigMapDiffs:     bm,
		DeffatedStorage: result.Get("storage").String(),
	}, nil
}

// ParseOrigination -
func (b Babylon) ParseOrigination(content gjson.Result, protocol string, level int64, operationID string) (RichStorage, error) {
	result, err := getResult(content)
	if err != nil {
		return RichStorage{Empty: true}, err
	}

	address := result.Get("originated_contracts.0").String()
	// s := content.Get("script.storage")

	m, err := meta.GetMetadata(b.es, address, consts.Babylon, "storage", protocol)
	if err != nil {
		return RichStorage{Empty: true}, err
	}

	data, err := b.rpc.GetScriptJSON(address, level)
	if err != nil {
		return RichStorage{Empty: true}, err
	}

	st := data.Get("storage")
	ptrToBin, err := b.binPathToPtrMap(m, st)
	if err != nil {
		return RichStorage{Empty: true}, err
	}

	bm, err := b.getBigMapDiff(result, ptrToBin, operationID, address, level, m)
	if err != nil {
		return RichStorage{Empty: true}, err
	}

	return RichStorage{
		BigMapDiffs:     bm,
		DeffatedStorage: st.String(),
	}, nil
}

// Enrich -
func (b Babylon) Enrich(storage string, bmd gjson.Result) (gjson.Result, error) {
	if bmd.IsArray() && len(bmd.Array()) == 0 {
		return gjson.Parse(storage), nil
	}

	data := gjson.Parse(storage)
	m := map[string][]interface{}{}
	for _, bm := range bmd.Array() {
		elt := map[string]interface{}{
			"prim": "Elt",
		}
		args := make([]interface{}, 1)
		val := gjson.Parse(bm.Get("value").String())
		args[0] = bm.Get("key").Value()

		if bm.Get("value").String() != "" {
			args = append(args, val.Value())
		}

		elt["args"] = args

		binPath := strings.TrimPrefix(bm.Get("bin_path").String(), "0/")
		p := miguel.GetGJSONPath(binPath)

		res, err := b.findPtrJSONPath(bmd.Get("ptr").Int(), p, data)
		if err != nil {
			return data, err
		}
		if _, ok := m[p]; !ok {
			m[res] = make([]interface{}, 0)
		}
		m[res] = append(m[res], elt)
	}

	for p, arr := range m {
		value, err := sjson.Set(storage, p, arr)
		if err != nil {
			return gjson.Result{}, err
		}
		data = gjson.Parse(value)
	}
	return data, nil
}

func (b Babylon) getBigMapDiff(result gjson.Result, ptrMap map[int64]string, operationID, address string, level int64, m meta.Metadata) ([]models.BigMapDiff, error) {
	bmd := make([]models.BigMapDiff, 0)

	for _, item := range result.Get("big_map_diff").Array() {
		if item.Get("action").String() == "update" {
			ptr := item.Get("big_map").Int()
			binPath, ok := ptrMap[ptr]
			if !ok {
				return nil, fmt.Errorf("Invalid big map pointer value: %d", ptr)
			}
			bmd = append(bmd, models.BigMapDiff{
				Ptr:         ptr,
				BinPath:     binPath,
				Key:         item.Get("key").Value(),
				KeyHash:     item.Get("key_hash").String(),
				Value:       item.Get("value").String(),
				OperationID: operationID,
				Level:       level,
				Address:     address,
			})
		}
	}
	return bmd, nil
}

func (b Babylon) binPathToPtrMap(m meta.Metadata, storage gjson.Result) (map[int64]string, error) {
	key := make(map[int64]string)
	keyInt := storage.Get("int")

	if keyInt.Exists() {
		key[keyInt.Int()] = "0"
		return key, nil
	}

	for k, v := range m {
		if v.Prim != consts.BIGMAP {
			continue
		}

		if err := b.setMapPtr(storage, k, key); err != nil {
			return nil, err
		}
	}
	return key, nil
}

func (b Babylon) setMapPtr(storage gjson.Result, path string, m map[int64]string) error {
	var buf strings.Builder

	trimmed := strings.TrimPrefix(path, "0/")
	for _, s := range strings.Split(trimmed, "/") {
		switch s {
		case "l", "s":
			buf.WriteString("#.")
		case "k":
			buf.WriteString("#.args.0.")
		case "v":
			buf.WriteString("#.args.1.")
		case "o":
			buf.WriteString("args.0.")
		default:
			buf.WriteString("args.")
			buf.WriteString(s)
			buf.WriteString(".")
		}
	}
	buf.WriteString("int")

	ptr := storage.Get(buf.String())
	if !ptr.Exists() {
		return fmt.Errorf("Path %s is not pointer: %s", path, buf.String())
	}

	for _, p := range ptr.Array() {
		if _, ok := m[p.Int()]; ok {
			return fmt.Errorf("Pointer already exists: %d", p.Int())
		}
		m[p.Int()] = path
	}

	return nil
}

func (b Babylon) findPtrJSONPath(ptr int64, path string, data gjson.Result) (string, error) {
	val := data
	parts := strings.Split(path, ".")

	var newPath strings.Builder
	for i := range parts {
		buf := val.Get(parts[i])

		if i == len(parts)-1 {
			if buf.Get("int").Exists() && buf.Get("int").Int() == ptr {
				return newPath.String(), nil
			}
		}

		if parts[i] == "#" {
			for j := 0; j < int(buf.Int()); j++ {
				var bufPath strings.Builder
				fmt.Fprintf(&bufPath, "%d", j)
				if i < len(parts)-1 {
					fmt.Fprintf(&bufPath, ".%s", strings.Join(parts[i+1:], "."))
				}
				p, err := b.findPtrJSONPath(ptr, bufPath.String(), val)
				if err != nil {
					return "", err
				}
				if p != "" {
					fmt.Fprintf(&newPath, ".%s", p)
					return newPath.String(), nil
				}
			}
		} else {
			if newPath.Len() != 0 {
				newPath.WriteString(".")
			}
			newPath.WriteString(parts[i])
			val = buf
		}
	}
	return newPath.String(), nil
}