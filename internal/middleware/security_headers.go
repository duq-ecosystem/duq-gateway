package middleware

import (
	"net/http"
)

// SecurityHeadersConfig holds configuration for security headers middleware.
type SecurityHeadersConfig struct {
	// HSTS settings
	HSTSEnabled           bool
	HSTSMaxAge            int  // Max-Age in seconds (default: 31536000 = 1 year)
	HSTSIncludeSubdomains bool // Include subdomains in HSTS

	// Frame options
	FrameOptions string // DENY, SAMEORIGIN, or empty to disable

	// Content Security Policy
	CSPEnabled   bool
	CSPDirective string // Full CSP directive

	// Other security headers
	ContentTypeNosniff bool // X-Content-Type-Options: nosniff
	XSSProtection      bool // X-XSS-Protection: 1; mode=block
	ReferrerPolicy     string // Referrer-Policy value
}

// DefaultSecurityHeadersConfig returns production-ready security headers config.
func DefaultSecurityHeadersConfig() SecurityHeadersConfig {
	return SecurityHeadersConfig{
		HSTSEnabled:           true,
		HSTSMaxAge:            31536000, // 1 year
		HSTSIncludeSubdomains: true,
		FrameOptions:          "DENY",
		CSPEnabled:            true,
		CSPDirective:          "default-src 'self'; script-src 'self' 'unsafe-inline' https://unpkg.com; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
		ContentTypeNosniff:    true,
		XSSProtection:         true,
		ReferrerPolicy:        "strict-origin-when-cross-origin",
	}
}

// SecurityHeaders returns a middleware that adds security headers to all responses.
// These headers protect against common web vulnerabilities:
// - HSTS: Forces HTTPS connections
// - X-Frame-Options: Prevents clickjacking
// - CSP: Mitigates XSS attacks
// - X-Content-Type-Options: Prevents MIME sniffing
// - X-XSS-Protection: Legacy XSS protection
// - Referrer-Policy: Controls referrer information
func SecurityHeaders(cfg SecurityHeadersConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// HSTS - Strict Transport Security
			if cfg.HSTSEnabled {
				hstsValue := "max-age=" + itoa(cfg.HSTSMaxAge)
				if cfg.HSTSIncludeSubdomains {
					hstsValue += "; includeSubDomains"
				}
				w.Header().Set("Strict-Transport-Security", hstsValue)
			}

			// X-Frame-Options - Clickjacking protection
			if cfg.FrameOptions != "" {
				w.Header().Set("X-Frame-Options", cfg.FrameOptions)
			}

			// Content-Security-Policy
			if cfg.CSPEnabled && cfg.CSPDirective != "" {
				w.Header().Set("Content-Security-Policy", cfg.CSPDirective)
			}

			// X-Content-Type-Options - Prevent MIME sniffing
			if cfg.ContentTypeNosniff {
				w.Header().Set("X-Content-Type-Options", "nosniff")
			}

			// X-XSS-Protection - Legacy XSS protection (for older browsers)
			if cfg.XSSProtection {
				w.Header().Set("X-XSS-Protection", "1; mode=block")
			}

			// Referrer-Policy
			if cfg.ReferrerPolicy != "" {
				w.Header().Set("Referrer-Policy", cfg.ReferrerPolicy)
			}

			// Permissions-Policy - Restrict browser features
			w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

			next.ServeHTTP(w, r)
		})
	}
}

// itoa converts int to string without importing strconv
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
