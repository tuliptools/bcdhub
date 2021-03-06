package elastic

import (
	"fmt"
	"strings"

	"github.com/baking-bad/bcdhub/internal/helpers"
	"github.com/baking-bad/bcdhub/internal/models"
	"github.com/tidwall/gjson"
)

const (
	defaultSize = 10
)

func getMapFields(allFields []string) map[string]string {
	res := make(map[string]string)
	for _, f := range allFields {
		str := strings.Split(f, "^")
		res[str[0]] = f
	}
	return res
}

func getHighlights(allFields []string) qItem {
	res := make(qItem)
	for _, f := range allFields {
		str := strings.Split(f, "^")
		res[str[0]] = qItem{}
	}
	return res
}

func getFields(search string, fields []string) ([]string, qItem, error) {
	if len(fields) == 0 {
		allFields, err := GetSearchScores(search, searchableInidices)
		if err != nil {
			return nil, nil, err
		}
		highlights := getHighlights(allFields)
		return allFields, highlights, nil
	}

	allFields, err := GetSearchScores(search, fields)
	if err != nil {
		return nil, nil, err
	}
	mapFields := getMapFields(allFields)

	f := make([]string, 0)
	h := make(qItem)
	for _, field := range fields {
		if nf, ok := mapFields[field]; ok {
			f = append(f, nf)
			s := strings.Split(nf, "^")
			h[s[0]] = qItem{}
		} else {
			return nil, nil, fmt.Errorf("Unknown field: %s", field)
		}
	}
	return f, h, nil
}

func prepareSearchFilters(filters map[string]interface{}) ([]qItem, error) {
	mustItems := make([]qItem, 0)
	for k, v := range filters {
		switch k {
		case "from":
			val, ok := v.(uint)
			if !ok {
				return nil, fmt.Errorf("Invalid type for 'from' filter (wait int64): %T", v)
			}
			if val > 0 {
				mustItems = append(mustItems, rangeQ("timestamp", qItem{
					"gte": val * 1000,
				}))
			}
		case "to":
			val, ok := v.(uint)
			if !ok {
				return nil, fmt.Errorf("Invalid type for 'to' filter (wait int64): %T", v)
			}
			if val > 0 {
				mustItems = append(mustItems, rangeQ("timestamp", qItem{
					"lte": val * 1000,
				}))
			}
		case "networks":
			val, ok := v.([]string)
			if !ok {
				return nil, fmt.Errorf("Invalid type for 'network' filter (wait []string): %T", v)
			}
			if len(val) == 0 {
				continue
			}
			mustItems = append(mustItems, in("network", val))
		case "languages":
			val, ok := v.([]string)
			if !ok {
				return nil, fmt.Errorf("Invalid type for 'language' filter (wait []string): %T", v)
			}
			if len(val) == 0 {
				continue
			}
			mustItems = append(mustItems, in("language", val))
		default:
			return nil, fmt.Errorf("Unknown search filter: %s", k)
		}
	}
	return mustItems, nil
}

func getSearchIndices(filters map[string]interface{}) ([]string, error) {
	if val, ok := filters["indices"]; ok {
		indices, ok := val.([]string)
		if !ok {
			return nil, fmt.Errorf("Invalid type for 'indices' filter (wait []string): %T", val)
		}
		for i := range indices {
			if !helpers.StringInArray(indices[i], searchableInidices) {
				return nil, fmt.Errorf("Invalid index name: %s", indices[i])
			}
		}
		delete(filters, "indices")
		return indices, nil
	}
	return searchableInidices, nil
}

// SearchByText -
func (e *Elastic) SearchByText(text string, offset int64, fields []string, filters map[string]interface{}, grouping bool) (SearchResult, error) {
	if grouping {
		return e.searchWithGroup(text, offset, fields, filters)
	}
	return e.searchWithoutGroup(text, offset, fields, filters)
}

func (e *Elastic) searchWithoutGroup(text string, offset int64, fields []string, filters map[string]interface{}) (SearchResult, error) {
	query := newQuery()

	indices, err := getSearchIndices(filters)
	if err != nil {
		return SearchResult{}, err
	}
	mustItems, err := prepareSearchFilters(filters)
	if err != nil {
		return SearchResult{}, err
	}
	if text != "" {
		internalFields, highlights, err := getFields(text, fields)
		if err != nil {
			return SearchResult{}, err
		}
		mustItems = append(mustItems, queryString(text, internalFields))

		query.Highlights(highlights)
	}

	b := boolQ()
	if len(mustItems) > 0 {
		b.Get("bool").Extend(must(mustItems...))
	}

	query.Query(b).From(offset).Size(defaultSize)

	resp, err := e.query(indices, query)
	if err != nil {
		return SearchResult{}, err
	}

	return SearchResult{
		Items: parseSearchResponse(resp),
		Time:  resp.Get("took").Int(),
		Count: resp.Get("hits.total.value").Int(),
	}, nil
}
func (e *Elastic) searchWithGroup(text string, offset int64, fields []string, filters map[string]interface{}) (SearchResult, error) {
	query := newQuery()

	indices, err := getSearchIndices(filters)
	if err != nil {
		return SearchResult{}, err
	}
	mustItems, err := prepareSearchFilters(filters)
	if err != nil {
		return SearchResult{}, err
	}

	if text == "" {
		return SearchResult{}, fmt.Errorf("Empty search string. Please query something")
	}

	internalFields, highlights, err := getFields(text, fields)
	if err != nil {
		return SearchResult{}, err
	}
	mustItems = append(mustItems, queryString(text, internalFields))

	b := boolQ()
	if len(mustItems) > 0 {
		b.Get("bool").Extend(must(mustItems...))
	}

	topHits := qItem{
		"top_hits": qItem{
			"size": 1,
			"sort": qList{
				sort("_score", "desc"),
				qItem{"last_action": qItem{"order": "desc", "unmapped_type": "long"}},
				sort("timestamp", "desc"),
			},
			"highlight": qItem{
				"fields": highlights,
			},
		},
	}

	query.Query(b).Add(
		aggs(
			"projects",
			qItem{
				"terms": qItem{
					"script": `
							if (doc.containsKey('fingerprint.parameter')) {
								return doc['fingerprint.parameter'].value + '|' + doc['fingerprint.storage'].value + '|' + doc['fingerprint.code'].value
							} else if (doc.containsKey('hash')) {
								return doc['hash.keyword'].value
							} else return doc['key_hash.keyword'].value`,
					"size": defaultSize + offset,
					"order": qList{
						qItem{"bucket_score": "desc"},
						qItem{"bucket_time": "desc"},
					},
				},
				"aggs": qItem{
					"last": topHits,
					"bucket_score": qItem{
						"max": qItem{
							"script": "_score",
						},
					},
					"bucket_time": qItem{
						"max": qItem{
							"script": "if (doc.containsKey('last_action')) {return doc['last_action'].value} else {return doc['timestamp']}",
						},
					},
				},
			},
		),
	).Zero()

	resp, err := e.query(indices, query)
	if err != nil {
		return SearchResult{}, err
	}
	return SearchResult{
		Items: parseSearchGroupingResponse(resp, defaultSize, offset),
		Time:  resp.Get("took").Int(),
		Count: resp.Get("hits.total.value").Int(),
	}, nil
}

func parseHighlights(hit gjson.Result) map[string][]string {
	highlight := hit.Get("highlight").Map()
	res := make(map[string][]string, len(highlight))
	for k, v := range highlight {
		items := v.Array()
		res[k] = make([]string, len(items))
		for i, item := range items {
			res[k][i] = item.String()
		}
	}
	return res
}

func parseSearchResponse(data gjson.Result) []SearchItem {
	items := make([]SearchItem, 0)
	arr := data.Get("hits.hits").Array()
	for i := range arr {
		index := arr[i].Get("_index").String()
		highlights := parseHighlights(arr[i])
		switch index {
		case DocContracts:
			var c models.Contract
			c.ParseElasticJSON(arr[i])
			item := SearchItem{
				Type:       DocContracts,
				Value:      c.Address,
				Body:       c,
				Highlights: highlights,
			}
			items = append(items, item)
		case DocOperations:
			var op models.Operation
			op.ParseElasticJSON(arr[i])
			item := SearchItem{
				Type:       DocOperations,
				Value:      op.Hash,
				Body:       op,
				Highlights: highlights,
			}
			items = append(items, item)
		case DocBigMapDiff:
			var b models.BigMapDiff
			b.ParseElasticJSON(arr[i])
			item := SearchItem{
				Type:       DocBigMapDiff,
				Value:      b.KeyHash,
				Body:       b,
				Highlights: highlights,
			}
			items = append(items, item)
		default:
		}

	}
	return items
}

func parseSearchGroupingResponse(data gjson.Result, size, offset int64) []SearchItem {
	buckets := data.Get("aggregations.projects.buckets")
	if !buckets.Exists() {
		return nil
	}

	arr := buckets.Array()
	lArr := int64(len(arr))
	items := make([]SearchItem, 0)
	if offset > lArr {
		return items
	}
	arr = arr[offset:]
	for i := range arr {
		searchItem := SearchItem{}
		count := arr[i].Get("doc_count").Int()
		if count > 1 {
			searchItem.Group = &Group{
				Count: arr[i].Get("doc_count").Int(),
				Top:   make([]Top, 0),
			}
		}

		for j, item := range arr[i].Get("last.hits.hits").Array() {
			index := item.Get("_index").String()
			highlights := parseHighlights(item)
			searchItem.Type = index

			switch index {
			case DocContracts:
				if j == 0 {
					var c models.Contract
					c.ParseElasticJSON(item)
					searchItem.Body = c
					searchItem.Value = c.Address
					searchItem.Highlights = highlights
				} else {
					searchItem.Group.Top = append(searchItem.Group.Top, Top{
						Key:     item.Get("_source.address").String(),
						Network: item.Get("_source.network").String(),
					})
				}
			case DocOperations:
				for j, item := range arr[i].Get("last.hits.hits").Array() {
					if j == 0 {
						var op models.Operation
						op.ParseElasticJSON(item)
						searchItem.Body = op
						searchItem.Value = op.Hash
						searchItem.Highlights = highlights
					} else {
						searchItem.Group.Top = append(searchItem.Group.Top, Top{
							Key:     item.Get("_source.hash").String(),
							Network: item.Get("_source.network").String(),
						})
					}
				}
			case DocBigMapDiff:
				var b models.BigMapDiff
				b.ParseElasticJSON(arr[i].Get("last.hits.hits.0"))
				searchItem.Body = b
				searchItem.Value = b.KeyHash
				searchItem.Highlights = highlights
			default:
			}
		}
		items = append(items, searchItem)
	}
	return items
}
