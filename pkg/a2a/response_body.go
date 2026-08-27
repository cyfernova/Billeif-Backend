package a2a

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

const MaxA2AResponseBodyBytes int64 = 1 << 20

var ErrA2AResponseBodyTooLarge = errors.New("A2A response body exceeds size limit")

// ReadResponseBody reads an A2A response body with a strict byte limit.
func ReadResponseBody(resp *http.Response) ([]byte, error) {
	if resp.ContentLength > MaxA2AResponseBodyBytes {
		return nil, responseBodyTooLargeError(resp.ContentLength)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxA2AResponseBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > MaxA2AResponseBodyBytes {
		return nil, responseBodyTooLargeError(int64(len(body)))
	}
	return body, nil
}

func responseBodyTooLargeError(size int64) error {
	return fmt.Errorf("%w: %d bytes exceeds %d-byte limit", ErrA2AResponseBodyTooLarge, size, MaxA2AResponseBodyBytes)
}
