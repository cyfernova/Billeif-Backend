package utils

import (
	"math"
	"strconv"

	"github.com/gin-gonic/gin"
)

type PaginationParams struct {
	Page  int `form:"page" json:"page"`
	Limit int `form:"limit" json:"limit"`
}

type PaginatedResponse struct {
	Data       interface{} `json:"data"`
	Total      int64       `json:"total"`
	Page       int         `json:"page"`
	Limit      int         `json:"limit"`
	TotalPages int         `json:"total_pages"`
	HasNext    bool        `json:"has_next"`
	HasPrev    bool        `json:"has_prev"`
}

func NewPaginationParams(page, limit int) PaginationParams {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	return PaginationParams{Page: page, Limit: limit}
}

func (p PaginationParams) Offset() int {
	return (p.Page - 1) * p.Limit
}

func NewPaginatedResponse(data interface{}, total int64, page, limit int) PaginatedResponse {
	totalPages := int(math.Ceil(float64(total) / float64(limit)))
	return PaginatedResponse{
		Data:       data,
		Total:      total,
		Page:       page,
		Limit:      limit,
		TotalPages: totalPages,
		HasNext:    page < totalPages,
		HasPrev:    page > 1,
	}
}

const (
	DefaultPage  = 1
	DefaultLimit = 10
	MaxLimit     = 100
)

// ParsePagination extracts and validates pagination parameters from the Gin context.
// Returns validated page and limit values with safe defaults.
func ParsePagination(c *gin.Context) (page, limit int) {
	pageStr := c.DefaultQuery("page", strconv.Itoa(DefaultPage))
	limitStr := c.DefaultQuery("limit", strconv.Itoa(DefaultLimit))

	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = DefaultPage
	}

	limit, err = strconv.Atoi(limitStr)
	if err != nil || limit < 1 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}

	return page, limit
}
