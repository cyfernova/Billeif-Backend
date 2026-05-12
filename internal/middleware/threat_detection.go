package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

const (
	securityDetectionKey           = "security_detection"
	securityDetectionCategoriesKey = "security_detection_categories"
	securityDetectionSeverityKey   = "security_detection_severity"
)

type threatDetectionRule struct {
	category   string
	severity   string
	indicators []string
}

var requestThreatRules = []threatDetectionRule{
	{
		category: "path_traversal_probe",
		severity: "high",
		indicators: []string{
			"../", "..\\", "%2e%2e", "%252e%252e", "/etc/passwd", "c:\\windows", "win.ini",
		},
	},
	{
		category: "secret_discovery_probe",
		severity: "high",
		indicators: []string{
			"/.env", ".git/", ".aws/", ".aws/credentials", "aws_access_key_id", "aws_secret_access_key", "config.env", "wp-config.php", "id_rsa", "private_key",
		},
	},
	{
		category: "ssrf_metadata_probe",
		severity: "high",
		indicators: []string{
			"169.254.169.254", "169.254.170.2", "100.100.100.200", "metadata.google.internal", "/latest/meta-data", "/metadata/identity", "/metadata/iam", "localhost", "127.0.0.1", "[::1]",
		},
	},
	{
		category: "jndi_rce_probe",
		severity: "high",
		indicators: []string{
			"${jndi:", "jndi:ldap", "jndi:rmi", "${lower:", "${::-",
		},
	},
	{
		category: "sql_injection_probe",
		severity: "medium",
		indicators: []string{
			" union select ", "' or '1'='1", "\" or \"1\"=\"1", " or 1=1", "sleep(", "benchmark(", "pg_sleep(", "information_schema",
		},
	},
	{
		category: "xss_probe",
		severity: "medium",
		indicators: []string{
			"<script", "%3cscript", "onerror=", "javascript:", "alert(",
		},
	},
	{
		category: "command_injection_probe",
		severity: "high",
		indicators: []string{
			";cat /etc/passwd", "|/bin/sh", "bash -c", "powershell", "$(curl", "`curl", "wget http",
		},
	},
}

var scannerUserAgentIndicators = []string{
	"acunetix", "burp", "masscan", "nessus", "nikto", "nuclei", "sqlmap", "zgrab",
}

// ThreatDetection annotates and logs high-signal exploit probes without blocking
// requests. WAF/rate-limit layers remain responsible for enforcement.
func ThreatDetection() gin.HandlerFunc {
	return func(c *gin.Context) {
		categories, severity := detectRequestThreats(c.Request.Method, c.Request.URL.EscapedPath(), c.Request.URL.RawQuery, c.Request.UserAgent())
		if len(categories) > 0 {
			c.Set(securityDetectionKey, true)
			c.Set(securityDetectionCategoriesKey, categories)
			c.Set(securityDetectionSeverityKey, severity)

			logger.FromContext(c.Request.Context()).Warn("security detection matched",
				"security_detection", true,
				"security_detection_categories", categories,
				"security_detection_severity", severity,
				"request_id", GetRequestID(c),
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"path_length", len(c.Request.URL.Path),
				"raw_query_present", c.Request.URL.RawQuery != "",
				"query_key_count", queryKeyCount(c.Request.URL.RawQuery),
				"content_length", c.Request.ContentLength,
				"route", c.FullPath(),
				"client_ip", c.ClientIP(),
				"user_agent", c.Request.UserAgent(),
			)
		}

		c.Next()
	}
}

func detectRequestThreats(method, path, rawQuery, userAgent string) ([]string, string) {
	requestTarget := normalizedDetectionInput(path + "?" + rawQuery)
	userAgentTarget := normalizedDetectionInput(userAgent)

	categories := make([]string, 0, 2)
	maxSeverity := ""
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodConnect, http.MethodTrace:
		categories = append(categories, "unusual_http_method")
		maxSeverity = higherSeverity(maxSeverity, "high")
	}

	if len(path) > 2048 {
		categories = append(categories, "oversized_path_probe")
		maxSeverity = higherSeverity(maxSeverity, "medium")
	}

	if queryKeyCount(rawQuery) > 1000 {
		categories = append(categories, "oversized_query_probe")
		maxSeverity = higherSeverity(maxSeverity, "medium")
	}

	for _, rule := range requestThreatRules {
		if containsAny(requestTarget, rule.indicators) {
			categories = append(categories, rule.category)
			maxSeverity = higherSeverity(maxSeverity, rule.severity)
		}
	}

	if containsAny(userAgentTarget, scannerUserAgentIndicators) {
		categories = append(categories, "scanner_user_agent")
		maxSeverity = higherSeverity(maxSeverity, "medium")
	}

	return categories, maxSeverity
}

func normalizedDetectionInput(value string) string {
	value = strings.ToLower(value)
	for i := 0; i < 2; i++ {
		decoded, err := url.QueryUnescape(value)
		if err != nil || decoded == value {
			break
		}
		value = strings.ToLower(decoded)
	}
	if len(value) > 8192 {
		return value[:8192]
	}
	return value
}

func containsAny(value string, indicators []string) bool {
	for _, indicator := range indicators {
		if strings.Contains(value, indicator) {
			return true
		}
	}
	return false
}

func higherSeverity(current, candidate string) string {
	if severityRank(candidate) > severityRank(current) {
		return candidate
	}
	return current
}

func severityRank(severity string) int {
	switch severity {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}
