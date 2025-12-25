package utils

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

var validate *validator.Validate

func init() {
	validate = validator.New()

	// Register custom tag name function to use json tag names in error messages
	validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})
}

// ValidateStruct validates a struct and returns formatted errors
func ValidateStruct(s interface{}) error {
	if err := validate.Struct(s); err != nil {
		if validationErrors, ok := err.(validator.ValidationErrors); ok {
			return formatValidationErrors(validationErrors)
		}
		return err
	}
	return nil
}

// formatValidationErrors converts validation errors to a readable format
func formatValidationErrors(errs validator.ValidationErrors) error {
	formatted := make(map[string]string)

	for _, err := range errs {
		field := err.Field()
		tag := err.Tag()
		param := err.Param()

		var message string
		switch tag {
		case "required":
			message = fmt.Sprintf("%s is required", field)
		case "email":
			message = fmt.Sprintf("%s must be a valid email address", field)
		case "min":
			message = fmt.Sprintf("%s must be at least %s characters", field, param)
		case "max":
			message = fmt.Sprintf("%s must be at most %s characters", field, param)
		case "len":
			message = fmt.Sprintf("%s must be %s characters", field, param)
		case "gte":
			message = fmt.Sprintf("%s must be greater than or equal to %s", field, param)
		case "lte":
			message = fmt.Sprintf("%s must be less than or equal to %s", field, param)
		case "oneof":
			message = fmt.Sprintf("%s must be one of: %s", field, param)
		case "url":
			message = fmt.Sprintf("%s must be a valid URL", field)
		case "uuid":
			message = fmt.Sprintf("%s must be a valid UUID", field)
		case "numeric":
			message = fmt.Sprintf("%s must be numeric", field)
		case "alphanum":
			message = fmt.Sprintf("%s must be alphanumeric", field)
		default:
			message = fmt.Sprintf("%s validation failed on %s", field, tag)
		}

		formatted[field] = message
	}

	return &ValidationError{Errors: formatted}
}

// ValidationError represents validation errors
type ValidationError struct {
	Errors map[string]string `json:"errors"`
}

// Error returns the validation error as JSON string
func (v *ValidationError) Error() string {
	data, _ := json.Marshal(v.Errors)
	return string(data)
}

// ToMap returns the errors as a map
func (v *ValidationError) ToMap() map[string]string {
	return v.Errors
}

// IsValidEmail checks if a string is a valid email
func IsValidEmail(email string) bool {
	return validate.Var(email, "required,email") == nil
}

// IsValidUUID checks if a string is a valid UUID
func IsValidUUID(id string) bool {
	return validate.Var(id, "required,uuid") == nil
}
