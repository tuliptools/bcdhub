package main

import (
	"encoding/json"
	"fmt"

	"github.com/baking-bad/bcdhub/internal/metrics"

	"github.com/baking-bad/bcdhub/internal/elastic"
	"github.com/baking-bad/bcdhub/internal/logger"
	"github.com/baking-bad/bcdhub/internal/models"
	"github.com/streadway/amqp"
)

func getContract(data amqp.Delivery) error {
	var contractID string
	if err := json.Unmarshal(data.Body, &contractID); err != nil {
		return fmt.Errorf("[getContract] Unmarshal message body error: %s", err)
	}

	c := models.Contract{ID: contractID}
	if err := ctx.ES.GetByID(&c); err != nil {
		return fmt.Errorf("[getContract] Find contract error: %s", err)
	}

	if err := parseContract(c); err != nil {
		return fmt.Errorf("[getContract] Compute error message: %s", err)
	}
	return nil
}

func parseContract(contract models.Contract) error {
	h := metrics.New(ctx.ES, ctx.DB)

	if contract.Alias == "" {
		h.SetContractAlias(ctx.Aliases, &contract)
	}

	if contract.ProjectID == "" {
		if err := h.SetContractProjectID(&contract); err != nil {
			return fmt.Errorf("[parseContract] Error during set contract projectID: %s", err)
		}
	}

	logger.Info("Contract %s to project %s", contract.Address, contract.ProjectID)

	return ctx.ES.UpdateFields(elastic.DocContracts, contract.ID, contract, "ProjectID", "Alias")
}
