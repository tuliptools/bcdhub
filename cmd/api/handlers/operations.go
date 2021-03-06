package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/baking-bad/bcdhub/internal/contractparser"
	"github.com/baking-bad/bcdhub/internal/contractparser/cerrors"
	"github.com/baking-bad/bcdhub/internal/contractparser/consts"
	"github.com/baking-bad/bcdhub/internal/contractparser/formatter"
	formattererror "github.com/baking-bad/bcdhub/internal/contractparser/formatter_error"
	"github.com/baking-bad/bcdhub/internal/contractparser/meta"
	"github.com/baking-bad/bcdhub/internal/contractparser/newmiguel"
	"github.com/baking-bad/bcdhub/internal/elastic"
	"github.com/baking-bad/bcdhub/internal/helpers"
	"github.com/baking-bad/bcdhub/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// GetContractOperations -
func (ctx *Context) GetContractOperations(c *gin.Context) {
	var req getContractRequest
	if err := c.BindUri(&req); handleError(c, err, http.StatusBadRequest) {
		return
	}

	var filtersReq operationsRequest
	if err := c.BindQuery(&filtersReq); handleError(c, err, http.StatusBadRequest) {
		return
	}

	filters := prepareFilters(filtersReq)
	ops, err := ctx.ES.GetContractOperations(req.Network, req.Address, filtersReq.Size, filters)
	if handleError(c, err, 0) {
		return
	}

	resp, err := prepareOperations(ctx.ES, ops.Operations)
	if handleError(c, err, 0) {
		return
	}
	c.JSON(http.StatusOK, OperationResponse{
		Operations: resp,
		LastID:     ops.LastID,
	})
}

// GetOperation -
func (ctx *Context) GetOperation(c *gin.Context) {
	var req OPGRequest
	if err := c.BindUri(&req); handleError(c, err, http.StatusBadRequest) {
		return
	}

	op, err := ctx.ES.GetOperationByHash(req.Hash)
	if len(op) == 0 {
		operation, err := ctx.getOperationFromMempool(req.Hash)
		if handleError(c, err, 0) {
			return
		}

		c.JSON(http.StatusOK, []Operation{operation})
		return
	}

	resp, err := prepareOperations(ctx.ES, op)
	if handleError(c, err, 0) {
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (ctx *Context) getOperationFromMempool(hash string) (Operation, error) {
	var wg sync.WaitGroup
	var opCh = make(chan Operation, len(ctx.TzKTServices))

	defer close(opCh)

	for network := range ctx.TzKTServices {
		wg.Add(1)
		go ctx.getOperation(network, hash, opCh, &wg)
	}

	wg.Wait()

	return <-opCh, nil
}

func (ctx *Context) getOperation(network, hash string, ops chan<- Operation, wg *sync.WaitGroup) {
	defer wg.Done()

	api, err := ctx.GetTzKTService(network)
	if err != nil {
		return
	}

	res, err := api.GetMempool(hash)
	if err != nil {
		return
	}

	if res.Get("#").Int() == 0 {
		return
	}

	operation, err := ctx.prepareMempoolOperation(res, network, hash)
	if err != nil {
		return
	}

	ops <- operation
}

func prepareFilters(req operationsRequest) map[string]interface{} {
	filters := map[string]interface{}{}

	if req.LastID != "" {
		filters["last_id"] = req.LastID
	}

	if req.From > 0 {
		filters["from"] = req.From
	}

	if req.To > 0 {
		filters["to"] = req.To
	}

	if req.Status != "" {
		status := "'" + strings.Join(strings.Split(req.Status, ","), "','") + "'"
		filters["status"] = status
	}

	if req.Entrypoints != "" {
		entrypoints := "'" + strings.Join(strings.Split(req.Entrypoints, ","), "','") + "'"
		filters["entrypoints"] = entrypoints
	}
	return filters
}

func formatErrors(errs []cerrors.IError, op *Operation) error {
	for i := range errs {
		if err := errs[i].Format(); err != nil {
			return err
		}
	}
	op.Errors = errs
	return nil
}

func prepareOperation(es *elastic.Elastic, operation models.Operation) (Operation, error) {
	op := Operation{
		ID:        operation.ID,
		Protocol:  operation.Protocol,
		Hash:      operation.Hash,
		Network:   operation.Network,
		Internal:  operation.Internal,
		Timesatmp: operation.Timestamp,

		Level:            operation.Level,
		Kind:             operation.Kind,
		Source:           operation.Source,
		SourceAlias:      operation.SourceAlias,
		Fee:              operation.Fee,
		Counter:          operation.Counter,
		GasLimit:         operation.GasLimit,
		StorageLimit:     operation.StorageLimit,
		Amount:           operation.Amount,
		Destination:      operation.Destination,
		DestinationAlias: operation.DestinationAlias,
		PublicKey:        operation.PublicKey,
		ManagerPubKey:    operation.ManagerPubKey,
		Delegate:         operation.Delegate,
		Status:           operation.Status,
		Burned:           operation.Burned,
		Entrypoint:       operation.Entrypoint,
		IndexedTime:      operation.IndexedTime,

		BalanceUpdates: operation.BalanceUpdates,
		Result:         operation.Result,
	}

	if err := formatErrors(operation.Errors, &op); err != nil {
		return op, err
	}
	if operation.DeffatedStorage != "" && strings.HasPrefix(op.Destination, "KT") && op.Status == "applied" {
		if err := setStorageDiff(es, op.Destination, op.Network, operation.DeffatedStorage, &op); err != nil {
			return op, err
		}
	}

	if op.Kind != consts.Transaction {
		return op, nil
	}

	if strings.HasPrefix(op.Destination, "KT") && !cerrors.HasParametersError(op.Errors) {
		if err := setParameters(es, operation.Parameters, &op); err != nil {
			return op, err
		}
	}

	return op, nil
}

func prepareOperations(es *elastic.Elastic, ops []models.Operation) ([]Operation, error) {
	resp := make([]Operation, len(ops))
	for i := 0; i < len(ops); i++ {
		op, err := prepareOperation(es, ops[i])
		if err != nil {
			return nil, err
		}
		resp[i] = op
	}
	return resp, nil
}

func setParameters(es *elastic.Elastic, parameters string, op *Operation) error {
	metadata, err := meta.GetMetadata(es, op.Destination, consts.PARAMETER, op.Protocol)
	if err != nil {
		return nil
	}

	params := gjson.Parse(parameters)
	op.Parameters, err = newmiguel.ParameterToMiguel(params, metadata)
	if err != nil {
		if !cerrors.HasGasExhaustedError(op.Errors) {
			helpers.CatchErrorSentry(err)
			return err
		}
	}
	return nil
}

func setStorageDiff(es *elastic.Elastic, address, network, storage string, op *Operation) error {
	metadata, err := meta.GetContractMetadata(es, address)
	if err != nil {
		return err
	}
	bmd, err := es.GetUniqueBigMapDiffsByOperationID(op.ID)
	if err != nil {
		return err
	}
	store, err := enrichStorage(storage, bmd, op.Protocol, false)
	if err != nil {
		return err
	}
	storageMetadata, err := metadata.Get(consts.STORAGE, op.Protocol)
	if err != nil {
		return err
	}
	currentStorage, err := newmiguel.MichelineToMiguel(store, storageMetadata)
	if err != nil {
		return err
	}

	var prevStorage *newmiguel.Node
	prev, err := es.GetPreviousOperation(address, op.Network, op.IndexedTime)
	if err == nil {
		var prevBmd []models.BigMapDiff
		if len(bmd) > 0 {
			prevBmd, err = getPrevBmd(es, bmd, op.IndexedTime, op.Destination)
			if err != nil {
				return err
			}
		}
		prevStore, err := enrichStorage(prev.DeffatedStorage, prevBmd, op.Protocol, false)
		if err != nil {
			return err
		}

		prevMetadata, err := metadata.Get(consts.STORAGE, prev.Protocol)
		if err != nil {
			return err
		}
		prevStorage, err = newmiguel.MichelineToMiguel(prevStore, prevMetadata)
		if err != nil {
			return err
		}
	} else {
		if !strings.Contains(err.Error(), elastic.RecordNotFound) {
			return err
		}

		if currentStorage == nil {
			return nil
		}
		prevStorage = nil
	}

	currentStorage.Diff(prevStorage)
	op.StorageDiff = currentStorage
	return nil
}

func enrichStorage(s string, bmd []models.BigMapDiff, protocol string, skipEmpty bool) (gjson.Result, error) {
	if len(bmd) == 0 {
		return gjson.Parse(s), nil
	}

	parser, err := contractparser.MakeStorageParser(nil, nil, protocol)
	if err != nil {
		return gjson.Result{}, err
	}

	return parser.Enrich(s, bmd, skipEmpty)
}

func getPrevBmd(es *elastic.Elastic, bmd []models.BigMapDiff, indexedTime int64, address string) ([]models.BigMapDiff, error) {
	return es.GetPrevBigMapDiffs(bmd, indexedTime, address)
}

func (ctx *Context) prepareMempoolOperation(res gjson.Result, network, hash string) (Operation, error) {
	item := res.Array()[0]

	status := item.Get("status").String()
	if status == "applied" {
		status = "pending"
	}

	op := Operation{
		Protocol:  item.Get("protocol").String(),
		Hash:      item.Get("hash").String(),
		Network:   network,
		Timesatmp: time.Unix(item.Get("timestamp").Int(), 0).UTC(),

		Kind:         item.Get("kind").String(),
		Source:       item.Get("source").String(),
		Fee:          item.Get("fee").Int(),
		Counter:      item.Get("counter").Int(),
		GasLimit:     item.Get("gas_limit").Int(),
		StorageLimit: item.Get("storage_limit").Int(),
		Amount:       item.Get("amount").Int(),
		Destination:  item.Get("destination").String(),
		Mempool:      true,
		Status:       status,
	}

	op.Errors = cerrors.ParseArray(item.Get("errors"))

	if op.Kind != consts.Transaction {
		return op, nil
	}

	if strings.HasPrefix(op.Destination, "KT") && op.Protocol != "" {
		if params := item.Get("parameters"); params.Exists() {
			ctx.buildOperationParameters(params, &op)
		} else {
			op.Entrypoint = "default"
		}
	}

	return op, nil
}

func (ctx *Context) buildOperationParameters(params gjson.Result, op *Operation) {
	metadata, err := meta.GetMetadata(ctx.ES, op.Destination, consts.PARAMETER, op.Protocol)
	if err != nil {
		return
	}

	op.Entrypoint, err = metadata.GetByPath(params)
	if err != nil && op.Errors == nil {
		return
	}

	op.Parameters, err = newmiguel.ParameterToMiguel(params, metadata)
	if err != nil {
		if !cerrors.HasParametersError(op.Errors) {
			return
		}
	}
}

// GetOperationErrorLocation -
func (ctx *Context) GetOperationErrorLocation(c *gin.Context) {
	var req getOperationByIDRequest
	if err := c.BindUri(&req); handleError(c, err, http.StatusBadRequest) {
		return
	}
	operation := models.Operation{ID: req.ID}
	if err := ctx.ES.GetByID(&operation); handleError(c, err, 0) {
		return
	}

	if !cerrors.HasScriptRejectedError(operation.Errors) {
		handleError(c, fmt.Errorf("No reject script error in operation"), http.StatusBadRequest)
		return
	}

	response, err := ctx.getErrorLocation(operation, 2)
	if handleError(c, err, 0) {
		return
	}
	c.JSON(http.StatusOK, response)
}

func (ctx *Context) getErrorLocation(operation models.Operation, window int) (GetErrorLocationResponse, error) {
	rpc, err := ctx.GetRPC(operation.Network)
	if err != nil {
		return GetErrorLocationResponse{}, err
	}
	code, err := contractparser.GetContract(rpc, operation.Destination, operation.Network, operation.Protocol, ctx.SharePath, 0)
	if err != nil {
		return GetErrorLocationResponse{}, err
	}
	opErr := cerrors.First(operation.Errors, consts.ScriptRejectedError)
	if opErr == nil {
		return GetErrorLocationResponse{}, fmt.Errorf("Can't find script rejevted error")
	}
	defaultError, ok := opErr.(*cerrors.DefaultError)
	if !ok {
		return GetErrorLocationResponse{}, fmt.Errorf("Invalid error type: %T", opErr)
	}

	location := int(defaultError.Location)
	sections := code.Get("code")
	row, sCol, eCol, err := formattererror.LocateContractError(sections, location)
	if err != nil {
		return GetErrorLocationResponse{}, err
	}

	michelson, err := formatter.MichelineToMichelson(sections, false, formatter.DefLineSize)
	if err != nil {
		return GetErrorLocationResponse{}, err
	}
	rows := strings.Split(michelson, "\n")
	start := helpers.MaxInt(0, row-window)
	end := helpers.MinInt(len(rows), row+window+1)

	rows = rows[start:end]
	return GetErrorLocationResponse{
		Text:        strings.Join(rows, "\n"),
		FailedRow:   row + 1,
		StartColumn: sCol,
		EndColumn:   eCol,
		FirstRow:    start + 1,
	}, nil
}
