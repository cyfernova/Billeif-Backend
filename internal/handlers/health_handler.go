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

func (h *HealthHandler) Check(c *gin.Context) {
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
