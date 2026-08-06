package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeHTTPServerUsesBoundedProductionTimeouts(t *testing.T) {
	application, httpServer, err := newRuntimeApplication(validRuntimeEnvironment(), bootstrapRuntimeDependencies())
	require.NoError(t, err)
	t.Cleanup(func() { _ = application.Shutdown(t.Context()) })

	assert.Equal(t, "0.0.0.0:8080", httpServer.Addr)
	assert.Equal(t, 5*time.Second, httpServer.ReadHeaderTimeout)
	assert.Equal(t, 15*time.Second, httpServer.ReadTimeout)
	assert.Equal(t, 15*time.Second, httpServer.WriteTimeout)
	assert.Equal(t, 30*time.Second, httpServer.IdleTimeout)
	assert.Equal(t, 16<<10, httpServer.MaxHeaderBytes)
}
