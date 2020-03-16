package elastic

import (
	"fmt"
	"time"

	"github.com/aopoltorzhicky/bcdhub/internal/models"
	"github.com/tidwall/gjson"
)

// GetOperationByID -
func (e *Elastic) GetOperationByID(id string) (op models.Operation, err error) {
	resp, err := e.GetByID(DocOperations, id)
	if err != nil {
		return
	}
	if !resp.Get("found").Bool() {
		return op, fmt.Errorf("Unknown operation with ID %s", id)
	}
	op.ParseElasticJSON(resp)
	return
}

// GetOperationByHash -
func (e *Elastic) GetOperationByHash(hash string) (ops []models.Operation, err error) {
	query := newQuery().Query(
		boolQ(
			must(
				matchPhrase("hash", hash),
			),
		),
	).Add(qItem{
		"sort": qItem{
			"_script": qItem{
				"type": "number",
				"script": qItem{
					"lang":   "painless",
					"inline": "doc['level'].value * 100 + (doc['internal'].value ? 0 : 10) + (doc['indexed_time'].value / params.nano_ts)",
					"params": qItem{
						"nano_ts": time.Now().UnixNano(),
					},
				},
				"order": "desc",
			},
		},
	}).All()
	resp, err := e.query([]string{DocOperations}, query)
	if err != nil {
		return
	}
	if resp.Get("hits.total.value").Int() < 1 {
		return nil, fmt.Errorf("Unknown operation with hash %s", hash)
	}
	count := resp.Get("hits.hits.#").Int()
	ops = make([]models.Operation, count)
	for i, item := range resp.Get("hits.hits").Array() {
		var o models.Operation
		o.ParseElasticJSON(item)
		ops[i] = o
	}

	return ops, nil
}

func (e *Elastic) getContractOPG(address, network string, size uint64, filters map[string]interface{}) ([]string, error) {
	if size == 0 {
		size = 10
	}

	filtersString, err := prepareOperationFilters(filters)
	if err != nil {
		return nil, err
	}

	sqlString := fmt.Sprintf(`SELECT hash, level
		FROM operation 
		WHERE (source = '%s' OR destination = '%s') AND network = '%s' %s 
		GROUP BY hash, level 
		ORDER BY level DESC 
		LIMIT %d`, address, address, network, filtersString, size)

	res, err := e.executeSQL(sqlString)
	if err != nil {
		return nil, err
	}

	hash := make([]string, 0)
	for _, item := range res.Get("rows").Array() {
		hash = append(hash, item.Get("0").String())
	}

	return hash, nil
}

func prepareOperationFilters(filters map[string]interface{}) (s string, err error) {
	for k, v := range filters {
		if v != "" {
			s += " AND "
			switch k {
			case "from":
				s += fmt.Sprintf("timestamp >= %d", v)
			case "to":
				s += fmt.Sprintf("timestamp <= %d", v)
			case "entrypoints":
				s += fmt.Sprintf("entrypoint IN (%s)", v)
			case "last_id":
				s += fmt.Sprintf("indexed_time < %s", v)
			case "status":
				s += fmt.Sprintf("status IN (%s)", v)
			default:
				return "", fmt.Errorf("Unknown operation filter: %s %v", k, v)
			}
		}
	}
	return
}

// GetContractOperations -
func (e *Elastic) GetContractOperations(network, address string, size uint64, filters map[string]interface{}) (po PageableOperations, err error) {
	opg, err := e.getContractOPG(address, network, size, filters)
	if err != nil {
		return
	}
	s := make([]qItem, len(opg))
	for i := range opg {
		s[i] = matchPhrase("hash", opg[i])
	}

	b := boolQ(
		should(s...),
		must(
			matchPhrase("network", network),
		),
	)
	b.Get("bool").Append("minimum_should_match", 1)
	query := newQuery().
		Query(b).
		Add(
			aggs("last_id", min("indexed_time")),
		).
		Add(qItem{
			"sort": qItem{
				"_script": qItem{
					"type": "number",
					"script": qItem{
						"lang":   "painless",
						"inline": "doc['level'].value * 100 + (doc['internal'].value ? 0 : 10) + (doc['indexed_time'].value / params.nano_ts)",
						"params": qItem{
							"nano_ts": time.Now().UnixNano(),
						},
					},
					"order": "desc",
				},
			},
		}).All()

	res, err := e.query([]string{DocOperations}, query)
	if err != nil {
		return
	}

	count := res.Get("hits.hits.#").Int()
	ops := make([]models.Operation, count)
	for i, item := range res.Get("hits.hits").Array() {
		var o models.Operation
		o.ParseElasticJSON(item)
		ops[i] = o
	}

	po.Operations = ops
	po.LastID = res.Get("aggregations.last_id.value").String()

	return
}

// GetLastStorage -
func (e *Elastic) GetLastStorage(network, address string) (gjson.Result, error) {
	query := newQuery().
		Query(
			boolQ(
				must(
					matchPhrase("network", network),
					matchPhrase("destination", address),
					term("status", "applied"),
				),
				notMust(
					term("deffated_storage", ""),
				),
			),
		).
		Add(qItem{
			"sort": qItem{
				"_script": qItem{
					"type": "number",
					"script": qItem{
						"lang":   "painless",
						"inline": "doc['level'].value * 100 + (doc['internal'].value ? 0 : 10) + (doc['indexed_time'].value / params.nano_ts)",
						"params": qItem{
							"nano_ts": time.Now().UnixNano(),
						},
					},
					"order": "desc",
				},
			},
		}).
		One()

	res, err := e.query([]string{DocOperations}, query)
	if err != nil {
		return gjson.Result{}, err
	}

	if res.Get("hits.total.value").Int() < 1 {
		return gjson.Result{}, nil
	}
	return res.Get("hits.hits.0"), nil
}

// GetPreviousOperation -
func (e *Elastic) GetPreviousOperation(address, network string, level int64) (op models.Operation, err error) {
	query := newQuery().
		Query(
			boolQ(
				must(
					matchPhrase("destination", address),
					matchPhrase("network", network),
					rangeQ("level", qItem{"lt": level}),
					term("status", "applied"),
				),
				notMust(
					term("deffated_storage", ""),
				),
			),
		).
		Add(qItem{
			"sort": qItem{
				"_script": qItem{
					"type": "number",
					"script": qItem{
						"lang":   "painless",
						"inline": "doc['level'].value * 100 + (doc['internal'].value ? 0 : 10) + (doc['indexed_time'].value / params.nano_ts)",
						"params": qItem{
							"nano_ts": time.Now().UnixNano(),
						},
					},
					"order": "desc",
				},
			},
		}).One()

	res, err := e.query([]string{DocOperations}, query)
	if err != nil {
		return
	}

	if res.Get("hits.total.value").Int() < 1 {
		return op, fmt.Errorf("Unknown operation: %s in %s on %d", address, network, level)
	}
	op.ParseElasticJSON(res.Get("hits.hits.0"))
	return
}
