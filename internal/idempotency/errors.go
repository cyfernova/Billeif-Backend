package idempotency

type InvalidKeyError struct{}

func (*InvalidKeyError) Error() string {
	return "a UUID idempotency key is required"
}

type ConflictError struct{}

func (*ConflictError) Error() string {
	return "idempotency key conflicts with a different request"
}

type InProgressError struct{}

func (*InProgressError) Error() string {
	return "idempotent request is still in progress"
}
