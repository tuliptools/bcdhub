package elastic

import (
	"fmt"

	"github.com/baking-bad/bcdhub/internal/models"
)

// GetLastProjectContracts -
func (e *Elastic) GetLastProjectContracts() ([]models.Contract, error) {
	query := newQuery().Add(
		aggs("projects", qItem{
			"terms": qItem{
				"field": "project_id.keyword",
				"size":  maxQuerySize,
			},
			"aggs": qItem{
				"last": topHits(1, "timestamp", "desc"),
			},
		}),
	).Zero()

	resp, err := e.query([]string{DocContracts}, query)
	if err != nil {
		return nil, err
	}

	arr := resp.Get("aggregations.projects.buckets.#.last.hits.hits.0")
	if !arr.Exists() {
		return nil, fmt.Errorf("Empty response: %v", resp)
	}

	contracts := make([]models.Contract, 0)
	for _, item := range arr.Array() {
		var c models.Contract
		c.ParseElasticJSON(item)
		contracts = append(contracts, c)
	}
	return contracts, nil
}

// GetSameContracts -
func (e *Elastic) GetSameContracts(c models.Contract, size, offset int64) (scp SameContractsResponse, err error) {
	if c.Fingerprint == nil {
		return scp, fmt.Errorf("Invalid contract data")
	}

	if size == 0 {
		size = 10
	}

	q := newQuery().Query(
		boolQ(
			filter(
				matchPhrase("hash", c.Hash),
			),
			notMust(
				matchPhrase("address", c.Address),
			),
		),
	).Sort("last_action", "desc").Size(size).From(offset)

	resp, err := e.query([]string{DocContracts}, q)
	if err != nil {
		return
	}

	arr := resp.Get("hits.hits")
	if !arr.Exists() {
		return scp, fmt.Errorf("Empty response: %v", resp)
	}

	contracts := make([]models.Contract, 0)
	for _, item := range arr.Array() {
		var c models.Contract
		c.ParseElasticJSON(item)
		contracts = append(contracts, c)
	}
	scp.Contracts = contracts
	scp.Count = resp.Get("hits.total.value").Uint()
	return
}

// GetSimilarContracts -
func (e *Elastic) GetSimilarContracts(c models.Contract) ([]SimilarContract, error) {
	if c.Fingerprint == nil {
		return nil, nil
	}

	query := newQuery().Query(
		boolQ(
			filter(
				matchPhrase("project_id", c.ProjectID),
			),
			notMust(
				matchQ("hash.keyword", c.Hash),
			),
		),
	).Add(
		aggs(
			"projects",
			qItem{
				"terms": qItem{
					"field": "hash.keyword",
					"size":  10000,
					"order": qItem{
						"bucketsSort": "desc",
					},
				},
				"aggs": qItem{
					"last":        topHits(1, "last_action", "desc"),
					"bucketsSort": max("last_action"),
				},
			},
		),
	).Zero()

	resp, err := e.query([]string{DocContracts}, query)
	if err != nil {
		return nil, err
	}

	buckets := resp.Get("aggregations.projects.buckets")
	if !buckets.Exists() {
		return nil, nil
	}

	res := make([]SimilarContract, 0)
	for _, item := range buckets.Array() {
		var buf models.Contract
		buf.ParseElasticJSON(item.Get("last.hits.hits.0"))
		res = append(res, SimilarContract{
			Contract: &buf,
			Count:    item.Get("doc_count").Int(),
		})
	}
	return res, nil
}

// GetProjectsStats -
func (e *Elastic) GetProjectsStats() (stats []ProjectStats, err error) {
	last := topHits(1, "timestamp", "desc")
	last.Get("top_hits").Append("_source", includes([]string{"address", "network", "timestamp"}))

	query := newQuery().Add(
		aggs("by_project", qItem{
			"terms": qItem{
				"field": "project_id.keyword",
				"size":  maxQuerySize,
			},
			"aggs": qItem{
				"by_same": qItem{
					"terms": qItem{
						"field": "hash.keyword",
						"size":  maxQuerySize,
					},
					"aggs": qItem{
						"last_action_date":  max("last_action"),
						"first_deploy_date": min("timestamp"),
					},
				},
				"count": qItem{
					"cardinality": qItem{
						"field": "hash.keyword",
					},
				},
				"last_action_date":  maxBucket("by_same>last_action_date"),
				"first_deploy_date": minBucket("by_same>first_deploy_date"),
				"language": qItem{
					"terms": qItem{
						"field": "language.keyword",
						"size":  1,
					},
				},
				"tx_count": sum("tx_count"),
				"last":     last,
			},
		}),
	).Zero()
	resp, err := e.query([]string{DocContracts}, query)
	if err != nil {
		return
	}
	count := resp.Get("aggregations.by_project.buckets.#").Int()
	stats = make([]ProjectStats, count)
	for i, item := range resp.Get("aggregations.by_project.buckets").Array() {
		var p ProjectStats
		p.parse(item)
		stats[i] = p
	}
	return
}

// GetDiffTasks -
func (e *Elastic) GetDiffTasks(offset int64) ([]DiffTask, error) {
	query := newQuery().Add(
		aggs("by_project", qItem{
			"terms": qItem{
				"field": "project_id.keyword",
				"size":  maxQuerySize,
			},
			"aggs": qItem{
				"by_hash": qItem{
					"terms": qItem{
						"field": "hash.keyword",
						"size":  maxQuerySize,
					},
					"aggs": qItem{
						"last": topHits(1, "last_action", "desc"),
					},
				},
			},
		}),
	).From(offset).Zero()

	resp, err := e.query([]string{DocContracts}, query)
	if err != nil {
		return nil, err
	}

	tasks := make([]DiffTask, 0)
	buckets := resp.Get("aggregations.by_project.buckets").Array()
	for _, bucket := range buckets {
		similar := bucket.Get("by_hash.buckets").Array()
		if len(similar) < 2 {
			continue
		}

		for i := 0; i < len(similar)-1; i++ {
			var current models.Contract
			current.ParseElasticJSON(similar[i].Get("last.hits.hits.0"))
			for j := i + 1; j < len(similar); j++ {
				var next models.Contract
				next.ParseElasticJSON(similar[j].Get("last.hits.hits.0"))

				tasks = append(tasks, DiffTask{
					Network1: current.Network,
					Address1: current.Address,
					Network2: next.Network,
					Address2: next.Address,
				})
			}
		}
	}

	return tasks, nil
}
