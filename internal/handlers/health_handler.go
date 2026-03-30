package handlers

import (
	"net/http"

	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type HealthHandler struct {
	log *logger.Logger
}

func NewHealthHandler(log *logger.Logger) *HealthHandler {
	return &HealthHandler{log: log}
}

// Check returns the health status of the service
// @Summary Health check
// @Description Returns a simple health check message
// @Tags Health
// @Produce plain
// @Success 200 {string} string "OK"
// @Router /health [get]
func (h *HealthHandler) Check(c *gin.Context) {
	logger.FromContext(c.Request.Context()).Named("health_handler").Debug("health check request")
	message := `
 __          __                                  _  _                _
 \ \        / /                                 | |(_)              | |
  \ \  /\  / /   ___    __ _   _ __   ___       | | _  __   __  ___ | |
   \ \/  \/ /   / _ \  / _` + "`" + ` | | '__| / _ \      | || | \ \ / / / _ \| |
    \  /\  /   |  __/ | (_| | | |    |  __/      | || |  \ V / |  __/|_|
     \/  \/     \___|  \__,_| |_|     \___|      |_||_|   \_/   \___|(_)
`
	c.String(http.StatusOK, message)
}
