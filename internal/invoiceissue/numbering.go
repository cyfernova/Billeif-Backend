package invoiceissue

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

const (
	DocumentTypeTaxInvoice   = "tax_invoice"
	DocumentTypeBillOfSupply = "bill_of_supply"
	defaultTimezone          = "Asia/Kolkata"
)

var (
	seriesPattern        = regexp.MustCompile(`^[A-Z]{1,3}$`)
	financialYearPattern = regexp.MustCompile(`^(\d{4})-(\d{4})$`)
)

type InvalidTimezoneError struct {
	Timezone string
}

func (e *InvalidTimezoneError) Error() string {
	return fmt.Sprintf("invalid business timezone %q", e.Timezone)
}

type InvalidSeriesError struct {
	Series string
}

func (e *InvalidSeriesError) Error() string {
	return fmt.Sprintf("invalid invoice series %q", e.Series)
}

type InvalidDocumentTypeError struct {
	DocumentType string
}

func (e *InvalidDocumentTypeError) Error() string {
	return fmt.Sprintf("unsupported invoice document type %q", e.DocumentType)
}

type SequenceExhaustedError struct {
	Number int
}

func (e *SequenceExhaustedError) Error() string {
	return fmt.Sprintf("invoice sequence exhausted at %d", e.Number)
}

type NotFoundError struct{}

func (e *NotFoundError) Error() string { return "invoice not found" }

type StaleVersionError struct {
	Expected int
	Actual   int
}

func (e *StaleVersionError) Error() string {
	return fmt.Sprintf("invoice version conflict: expected %d, found %d", e.Expected, e.Actual)
}

type AlreadyIssuedError struct{}

func (e *AlreadyIssuedError) Error() string { return "invoice is already issued" }

type InvalidLifecycleError struct {
	Reason string
}

func (e *InvalidLifecycleError) Error() string {
	if e.Reason == "" {
		return "invalid invoice lifecycle"
	}
	return "invalid invoice lifecycle: " + e.Reason
}

func FinancialYear(at time.Time, timezone string) (string, error) {
	if timezone == "" {
		timezone = defaultTimezone
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return "", &InvalidTimezoneError{Timezone: timezone}
	}
	local := at.In(location)
	startYear := local.Year()
	if local.Month() < time.April {
		startYear--
	}
	return fmt.Sprintf("%04d-%04d", startYear, startYear+1), nil
}

func FormatNumber(documentType, series, financialYear string, number int) (string, error) {
	if documentType != DocumentTypeTaxInvoice && documentType != DocumentTypeBillOfSupply {
		return "", &InvalidDocumentTypeError{DocumentType: documentType}
	}
	if !seriesPattern.MatchString(series) {
		return "", &InvalidSeriesError{Series: series}
	}
	if number < 1 || number > 999999 {
		return "", &SequenceExhaustedError{Number: number}
	}
	matches := financialYearPattern.FindStringSubmatch(financialYear)
	if len(matches) != 3 {
		return "", fmt.Errorf("invalid financial year %q", financialYear)
	}
	startYear, _ := strconv.Atoi(matches[1])
	endYear, _ := strconv.Atoi(matches[2])
	if endYear != startYear+1 {
		return "", fmt.Errorf("invalid financial year %q", financialYear)
	}
	return fmt.Sprintf("%s/%02d-%02d/%06d", series, startYear%100, endYear%100, number), nil
}
