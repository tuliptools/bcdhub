package macros

import (
	"fmt"

	"github.com/valyala/fastjson"
)

type setCarFamily struct{}

func (f setCarFamily) Find(arr ...*fastjson.Value) (macros, error) {
	var offset int
	switch len(arr) {
	case 3:
	case 6:
		offset = 3
		prim0 := getPrim(arr[0])
		prim1 := getPrim(arr[1])
		prim2 := getPrim(arr[2])
		if prim0 != pDUP || prim1 != pCAR || prim2 != pDROP {
			return nil, nil
		}
	default:
		return nil, nil
	}

	fPrim := getPrim(arr[offset])
	sPrim := getPrim(arr[offset+1])
	tPrim := getPrim(arr[offset+2])

	if fPrim != pCDR || sPrim != pSWAP || tPrim != pPAIR {
		return nil, nil
	}
	return setCarMacros{
		skip: offset + 3,
	}, nil
}

type setCarMacros struct {
	skip int
}

func (f setCarMacros) Replace(tree *fastjson.Value, idx int) error {
	if tree.Type() != fastjson.TypeArray {
		return fmt.Errorf("Invalid tree type in setCarMacros.Replace: %s", tree.Type())
	}

	arena := fastjson.Arena{}
	newValue := arena.NewObject()
	newPrim := arena.NewString("SET_CAR")

	newValue.Set("prim", newPrim)

	carPrim := tree.Get("1", "prim").String()
	if carPrim == pCAR {
		annots := tree.Get("1", "annots")
		if annots != nil {
			newValue.Set("annots", annots)
		}
	}

	*tree = *newValue
	return nil
}

func (f setCarMacros) Skip() int {
	return f.skip
}
