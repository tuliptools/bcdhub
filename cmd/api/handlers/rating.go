package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetContractRating -
func (ctx *Context) GetContractRating(c *gin.Context) {
	var req getContractRequest
	if err := c.BindUri(&req); handleError(c, err, http.StatusBadRequest) {
		return
	}

	rating, err := ctx.DB.GetSubscriptionRating(req.Address, req.Network)
	if handleError(c, err, 0) {
		return
	}

	c.JSON(http.StatusOK, rating)
}
