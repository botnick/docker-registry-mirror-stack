package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func parseOptionalBool(value string) *bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	result := parseBool(value)
	return &result
}

func queryInt(value string, fallback int) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseInt64(value string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(value), 10, 64)
}

func clamp(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func trimTail(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}

func roundFloat(value float64) float64 {
	return float64(int(value*100)) / 100
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func nullString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func toPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	text := value.String
	return &text
}

func nullStringValue(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringsHasPrefix(value, prefix string) bool {
	return strings.HasPrefix(value, prefix)
}

func ternaryString(condition bool, truthy, falsy string) string {
	if condition {
		return truthy
	}
	return falsy
}

func tokenString(bytesLength int) (string, error) {
	buffer := make([]byte, bytesLength)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func parseRFC3339(value string) (time.Time, error) {
	return time.Parse(time.RFC3339, value)
}

func mustRFC3339OrZero(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func safeDurationMinutes(value time.Duration) int {
	return int(value.Minutes())
}

func validatePasswordStrength(username, password string) error {
	password = strings.TrimSpace(password)
	switch {
	case len(password) < 12:
		return fmt.Errorf("รหัสผ่านต้องยาวอย่างน้อย 12 ตัวอักษร")
	case strings.EqualFold(password, username):
		return fmt.Errorf("รหัสผ่านต้องไม่เหมือนชื่อผู้ใช้")
	}

	var hasLetter bool
	var hasNumber bool
	for _, char := range password {
		switch {
		case char >= '0' && char <= '9':
			hasNumber = true
		case (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z'):
			hasLetter = true
		}
	}
	if !hasLetter || !hasNumber {
		return fmt.Errorf("รหัสผ่านควรมีทั้งตัวอักษรและตัวเลข")
	}
	return nil
}
