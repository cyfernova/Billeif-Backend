package middleware

// The middleware execution order (Gin processes middleware in order they are registered):
// 1. Recovery - Panic recovery
// 2. RequestID - Correlation ID generation
// 3. Logging - Request/response logging
// 4. CORS - Cross-origin handling
// 5. RateLimit - Rate limiting
// 6. Auth - JWT authentication (for protected routes)
// 7. RBAC - Role-based access control (for protected routes)
