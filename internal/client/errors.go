package client

import (
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
)

// extractUsername extracts the username from a Kubernetes error message.
// Error messages typically contain patterns like 'User "id:username"' or 'User "username"'.
func extractUsername(message string) string {
	// Try to match 'User "id:username"' pattern first
	re := regexp.MustCompile(`User "(?:id:)?([^"]+)"`)
	matches := re.FindStringSubmatch(message)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// FormatK8sError converts Kubernetes API errors into user-friendly messages.
// It extracts the meaningful part of the error and formats it appropriately.
func FormatK8sError(err error, operation, resource, namespace string) error {
	if err == nil {
		return nil
	}

	// Check for Kubernetes status errors
	if statusErr, ok := err.(*errors.StatusError); ok {
		status := statusErr.ErrStatus

		switch status.Reason {
		case "Forbidden":
			username := extractUsername(status.Message)
			if username != "" {
				return fmt.Errorf("user %q does not have permission to %s %s in workspace %q", username, operation, resource, namespace)
			}
			return fmt.Errorf("you do not have permission to %s %s in workspace %q", operation, resource, namespace)
		case "NotFound":
			return fmt.Errorf("%s not found in workspace %q", resource, namespace)
		case "AlreadyExists":
			return fmt.Errorf("%s already exists in workspace %q", resource, namespace)
		case "Unauthorized":
			return fmt.Errorf("authentication required - please run 'sgs fetch' to refresh credentials")
		case "Conflict":
			return fmt.Errorf("%s is being modified by another operation, please try again", resource)
		case "ServiceUnavailable":
			return fmt.Errorf("service temporarily unavailable, please try again later")
		case "Timeout":
			return fmt.Errorf("request timed out, please try again")
		}
	}

	// Check for common error patterns in the error string
	errStr := err.Error()

	if strings.Contains(errStr, "forbidden") || strings.Contains(errStr, "Forbidden") {
		username := extractUsername(errStr)
		if username != "" {
			return fmt.Errorf("user %q does not have permission to %s %s in workspace %q", username, operation, resource, namespace)
		}
		return fmt.Errorf("you do not have permission to %s %s in workspace %q", operation, resource, namespace)
	}

	if strings.Contains(errStr, "not found") || strings.Contains(errStr, "NotFound") {
		return fmt.Errorf("%s not found in workspace %q", resource, namespace)
	}

	if strings.Contains(errStr, "already exists") {
		return fmt.Errorf("%s already exists in workspace %q", resource, namespace)
	}

	if strings.Contains(errStr, "Unauthorized") || strings.Contains(errStr, "unauthorized") {
		return fmt.Errorf("authentication required - please run 'sgs fetch' to refresh credentials")
	}

	if strings.Contains(errStr, "connection refused") {
		return fmt.Errorf("cannot connect to cluster - please check your network connection")
	}

	if strings.Contains(errStr, "no such host") {
		return fmt.Errorf("cannot reach cluster - please check your network connection")
	}

	if strings.Contains(errStr, "certificate") {
		return fmt.Errorf("certificate error - please run 'sgs fetch' to update configuration")
	}

	// Return a generic but clean error
	return fmt.Errorf("failed to %s %s: %v", operation, resource, err)
}

// FormatSimpleK8sError is a simpler version for cases where we don't have full context
func FormatSimpleK8sError(err error, namespace string) error {
	if err == nil {
		return nil
	}

	errStr := err.Error()

	if strings.Contains(errStr, "forbidden") || strings.Contains(errStr, "Forbidden") {
		username := extractUsername(errStr)
		if username != "" {
			return fmt.Errorf("user %q does not have permission to perform this operation in workspace %q", username, namespace)
		}
		return fmt.Errorf("you do not have permission to perform this operation in workspace %q", namespace)
	}

	if strings.Contains(errStr, "Unauthorized") || strings.Contains(errStr, "unauthorized") {
		return fmt.Errorf("authentication required - please run 'sgs fetch' to refresh credentials")
	}

	if strings.Contains(errStr, "connection refused") || strings.Contains(errStr, "no such host") {
		return fmt.Errorf("cannot connect to cluster - please check your network connection")
	}

	return err
}
