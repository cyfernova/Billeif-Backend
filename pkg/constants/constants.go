package constants

const (
	InvoiceStatusDraft     = "draft"
	InvoiceStatusSent      = "sent"
	InvoiceStatusPaid      = "paid"
	InvoiceStatusOverdue   = "overdue"
	InvoiceStatusCancelled = "cancelled"
)

const (
	NotificationTypeInvoiceCreated = "invoice.created"
	NotificationTypeInvoiceSent    = "invoice.sent"
	NotificationTypeInvoicePaid    = "invoice.paid"
	NotificationTypeInvoiceOverdue = "invoice.overdue"
)

const (
	AlertTypePaymentFailed     = "invoice.payment_failed"
	AlertTypeDuplicateDetected = "invoice.duplicate_detected"
)

const (
	RequestIDHeader = "X-Request-ID"
	CorrelationID   = "correlation_id"
)

const (
	DefaultTimeout = 30
)
